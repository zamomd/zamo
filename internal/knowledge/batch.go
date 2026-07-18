package knowledge

import (
	"fmt"
	"slices"
)

// validateAdds rejects a batch whose adds cannot resolve to distinct ids: an
// add with no temp id would emit a blank BlockID, and two adds sharing a temp
// id would collapse onto one generated id. Both mean malformed input (the
// format requires every new block to carry a unique temp id), so prepareOps
// refuses the whole batch rather than write something ambiguous to storage.
func validateAdds(ops []Op) error {
	seen := make(map[string]bool)
	for _, op := range ops {
		if op.Kind != OpAdd {
			continue
		}
		if op.TempID == "" {
			return fmt.Errorf("add op with empty temp id: %w", ErrInvalidOps)
		}
		if seen[op.TempID] {
			return fmt.Errorf("duplicate temp id %q: %w", op.TempID, ErrInvalidOps)
		}
		seen[op.TempID] = true
	}
	return nil
}

// hasCycle reports whether the ops' parent relationships form a ring. Edges run
// child → parent over block ids (temp or existing): an add links its TempID to
// its Parent, a move links the moved BlockID to its new Parent. A ring means the
// tree cannot be built — including a move of a block under its own descendant,
// whose ring closes through an existing id.
func hasCycle(ops []Op) bool {
	parents := make(map[string][]string)
	for _, op := range ops {
		switch op.Kind {
		case OpAdd:
			if op.TempID != "" && op.Parent != "" {
				parents[op.TempID] = append(parents[op.TempID], op.Parent)
			}
		case OpMove:
			if op.BlockID != "" && op.Parent != "" {
				parents[string(op.BlockID)] = append(parents[string(op.BlockID)], op.Parent)
			}
		}
	}
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int)
	var walk func(node string) bool
	walk = func(node string) bool {
		switch state[node] {
		case visiting:
			return true // back-edge to a node still on the stack
		case done:
			return false
		}
		state[node] = visiting
		if slices.ContainsFunc(parents[node], walk) {
			return true
		}
		state[node] = done
		return false
	}
	for node := range parents {
		if walk(node) {
			return true
		}
	}
	return false
}

// orderParentsFirst returns the ops in an order where every add appears before
// any op that references the temp id it creates. It assumes an acyclic input
// (hasCycle has passed), so a stable Kahn traversal always drains. Ties keep the
// original order, which keeps the output deterministic.
func orderParentsFirst(ops []Op) []Op {
	providedBy := make(map[string]int, len(ops)) // temp id → index of its add
	for i, op := range ops {
		if op.Kind == OpAdd && op.TempID != "" {
			providedBy[op.TempID] = i
		}
	}

	indegree := make([]int, len(ops))
	dependents := make([][]int, len(ops)) // provider index → ops that depend on it
	for i, op := range ops {
		for _, ref := range tempRefs(op) {
			if j, ok := providedBy[ref]; ok && j != i {
				dependents[j] = append(dependents[j], i)
				indegree[i]++
			}
		}
	}

	ordered := make([]Op, 0, len(ops))
	placed := make([]bool, len(ops))
	for len(ordered) < len(ops) {
		// Pick the lowest-index op with no unmet dependency, for a stable order.
		next := -1
		for i := range ops {
			if !placed[i] && indegree[i] == 0 {
				next = i
				break
			}
		}
		placed[next] = true
		ordered = append(ordered, ops[next])
		for _, k := range dependents[next] {
			indegree[k]--
		}
	}
	return ordered
}

// tempRefs returns the operation's references to blocks created within the same
// batch (temp ids): the fields a later op can depend on.
func tempRefs(op Op) []string {
	switch op.Kind {
	case OpAdd, OpMove:
		if op.Parent != "" {
			return []string{op.Parent}
		}
	case OpLink:
		return []string{op.From, op.To}
	}
	return nil
}

// prepareOps validates a write batch and makes it storage-ready: shape
// checks, cycle detection, parent-before-child ordering, and temp_id → ID
// resolution via newID. Pure: no storage, no clock. The returned map is the
// temp id → real id assignment reported back to the writer.
//
// A reference to an id that is not a temp id of this batch is taken to be an
// existing block and passed through; whether it exists is storage's concern.
func prepareOps(ops []Op, newID IDGenerator) ([]Op, map[string]ID, error) {
	if len(ops) == 0 {
		return nil, nil, ErrEmptyOps
	}
	for i, op := range ops {
		if err := op.validate(); err != nil {
			return nil, nil, fmt.Errorf("op %d: %w", i, err)
		}
	}
	if err := validateAdds(ops); err != nil {
		return nil, nil, err
	}
	if hasCycle(ops) {
		return nil, nil, ErrCycle
	}
	ordered := orderParentsFirst(ops)

	idOf := make(map[string]ID, len(ordered))
	for _, op := range ordered {
		if op.Kind == OpAdd && op.TempID != "" {
			idOf[op.TempID] = newID()
		}
	}
	resolve := func(ref string) string {
		if id, ok := idOf[ref]; ok {
			return string(id)
		}
		return ref
	}
	out := make([]Op, 0, len(ordered))
	for _, op := range ordered {
		switch op.Kind {
		case OpAdd:
			op.BlockID = idOf[op.TempID]
			op.Parent = resolve(op.Parent)
			op.TempID = ""
		case OpMove:
			op.Parent = resolve(op.Parent)
		case OpLink:
			op.From = resolve(op.From)
			op.To = resolve(op.To)
		}
		out = append(out, op)
	}
	return out, idOf, nil
}
