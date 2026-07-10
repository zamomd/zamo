package knowledge

// assemble turns a proposal and the user's selection into the final operation
// list handed to Repository.ApplyProposal. It is pure: no storage, no clock. The
// steps are selection, cascade removal of ops that depend on unselected ones,
// cycle detection, parent-before-child ordering, and temp_id → ID resolution via
// newID. The result carries resolved ids only (adds gain a BlockID, references
// point at real ids), so storage can write it without further interpretation.
func assemble(p Proposal, selected []int, newID IDGenerator) ([]Op, error) {
	chosen := selectOps(p.Ops, selected)
	chosen = cascade(chosen, proposalTempIDs(p.Ops))

	if hasCycle(chosen) {
		return nil, ErrCycle
	}
	ordered := orderParentsFirst(chosen)

	// A temp id is a block being created by an add; ids are assigned in emit
	// order so a parent always resolves before the child that references it.
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
	return out, nil
}

// selectOps keeps the ops at the given indexes, in their original order,
// ignoring out-of-range and duplicate indexes.
func selectOps(ops []Op, selected []int) []Op {
	seen := make(map[int]bool, len(selected))
	chosen := make([]Op, 0, len(selected))
	for _, i := range selected {
		if i < 0 || i >= len(ops) || seen[i] {
			continue
		}
		seen[i] = true
		chosen = append(chosen, ops[i])
	}
	return chosen
}

// proposalTempIDs is the set of every temp id the proposal's adds introduce,
// used to tell a reference to a (possibly unselected) new block apart from a
// reference to an existing block.
func proposalTempIDs(ops []Op) map[string]bool {
	temps := make(map[string]bool)
	for _, op := range ops {
		if op.Kind == OpAdd && op.TempID != "" {
			temps[op.TempID] = true
		}
	}
	return temps
}

// cascade drops ops that reference a temp block no surviving add creates: a
// reference to an unselected add's temp id would dangle. Removal repeats to a
// fixpoint, so dropping a block also drops everything hanging off it. A
// reference to an existing block (an id not in allTemp) is always kept.
func cascade(chosen []Op, allTemp map[string]bool) []Op {
	for {
		created := make(map[string]bool, len(chosen))
		for _, op := range chosen {
			if op.Kind == OpAdd && op.TempID != "" {
				created[op.TempID] = true
			}
		}
		kept := make([]Op, 0, len(chosen))
		for _, op := range chosen {
			dangling := false
			for _, ref := range tempRefs(op) {
				if allTemp[ref] && !created[ref] {
					dangling = true
					break
				}
			}
			if !dangling {
				kept = append(kept, op)
			}
		}
		if len(kept) == len(chosen) {
			return kept
		}
		chosen = kept
	}
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
		for _, parent := range parents[node] {
			if walk(parent) {
				return true
			}
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
// proposal (temp ids): the fields a later op can depend on.
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
