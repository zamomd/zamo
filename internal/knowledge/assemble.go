package knowledge

// assemble turns a proposal and the user's selection into the final operation
// list handed to Repository.ApplyProposal. It is pure: no storage, no clock. The
// steps are selection, cascade removal of ops that depend on unselected ones,
// cycle detection, parent-before-child ordering, and temp_id → ID resolution via
// newID. The result carries resolved ids only (adds gain a BlockID, references
// point at real ids), so storage can write it without further interpretation.
func assemble(p Proposal, selected []int, newID IDGenerator) ([]Op, error) {
	chosen := selectOps(p.Ops, selected)

	// A temp id is a block being created by an add somewhere in the proposal;
	// any reference to one that is not (kept) creates that block is dangling.
	idOf := make(map[string]ID, len(chosen))
	for _, op := range chosen {
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

	out := make([]Op, 0, len(chosen))
	for _, op := range chosen {
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
