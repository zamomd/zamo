package knowledge

import (
	"errors"
	"slices"
	"testing"
)

// outlineService wires a Service over a fakeRepo seeded with one topic root
// and two children, which is the smallest shape every outline operation needs.
//
//	root
//	├── first
//	└── second
func outlineService(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	blocks := map[ID]Block{
		"root":   {ID: "root", Content: "Root"},
		"first":  {ID: "first", ParentID: "root", Position: 0, Content: "First"},
		"second": {ID: "second", ParentID: "root", Position: 65536, Content: "Second"},
	}
	repo := &fakeRepo{
		blocks: blocks,
		subtrees: map[ID][]Block{
			"root": {blocks["root"], blocks["first"], blocks["second"]},
		},
	}
	return NewService(nil, repo, func() ID { return "new-1" }, nil), repo
}

func TestSplitBlockEditsHeadAndAnchorsTailAfterIt(t *testing.T) {
	svc, repo := outlineService(t)

	newID, err := svc.SplitBlock(t.Context(), "first", "Fi", "rst")
	if err != nil {
		t.Fatalf("SplitBlock: %v", err)
	}
	if newID != "new-1" {
		t.Errorf("new id = %q, want new-1", newID)
	}
	if repo.applyMeta.Author != AuthorOwner || repo.applyMeta.Source != SourceGUIManual {
		t.Errorf("meta = %+v, want owner/gui_manual (born reviewed)", repo.applyMeta)
	}
	if len(repo.applyOps) != 2 {
		t.Fatalf("ops = %+v, want an edit and an add", repo.applyOps)
	}
	edit, add := repo.applyOps[0], repo.applyOps[1]
	if edit.Kind != OpEdit || edit.BlockID != "first" || edit.Content != "Fi" {
		t.Errorf("edit op = %+v, want edit of first to %q", edit, "Fi")
	}
	// The tail is a sibling anchored on the head, so it lands directly below
	// it rather than at the end of the parent's children.
	if add.Kind != OpAdd || add.Parent != "root" || add.After != "first" || add.Content != "rst" {
		t.Errorf("add op = %+v, want add under root after first with %q", add, "rst")
	}
	if add.Before != "" {
		t.Errorf("add op carries Before %q, want only After", add.Before)
	}
}

func TestInsertBeforeAnchorsAheadOfTheBlock(t *testing.T) {
	svc, repo := outlineService(t)

	if _, err := svc.InsertBefore(t.Context(), "first", ""); err != nil {
		t.Fatalf("InsertBefore: %v", err)
	}
	if len(repo.applyOps) != 1 {
		t.Fatalf("ops = %+v, want a single add", repo.applyOps)
	}
	add := repo.applyOps[0]
	if add.Kind != OpAdd || add.Parent != "root" || add.Before != "first" || add.Content != "" {
		t.Errorf("add op = %+v, want empty add under root before first", add)
	}
}

func TestSplitBlockRefusesATopicRoot(t *testing.T) {
	svc, repo := outlineService(t)

	// A topic root has no parent to hang a sibling from, and splitting one
	// would silently create a second topic.
	_, err := svc.SplitBlock(t.Context(), "root", "Ro", "ot")
	if !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("applyCalls = %d, want 0 (refused before any write)", repo.applyCalls)
	}
}

func TestSplitBlockReportsAMissingBlock(t *testing.T) {
	svc, _ := outlineService(t)

	if _, err := svc.SplitBlock(t.Context(), "ghost", "a", "b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestIndentBlockMovesItUnderThePreviousSibling(t *testing.T) {
	svc, repo := outlineService(t)

	if err := svc.IndentBlock(t.Context(), "second"); err != nil {
		t.Fatalf("IndentBlock: %v", err)
	}
	if len(repo.applyOps) != 1 {
		t.Fatalf("ops = %+v, want a single move", repo.applyOps)
	}
	move := repo.applyOps[0]
	// No anchor: the block becomes the previous sibling's last child, which
	// is where it already appears on screen once it is indented.
	if move.Kind != OpMove || move.BlockID != "second" || move.Parent != "first" {
		t.Errorf("move op = %+v, want move of second under first", move)
	}
	if move.After != "" || move.Before != "" {
		t.Errorf("move op = %+v, want no anchor (appended)", move)
	}
}

func TestIndentFirstChildIsRefused(t *testing.T) {
	svc, repo := outlineService(t)

	// Nothing above it at its own level to become its parent.
	err := svc.IndentBlock(t.Context(), "first")
	if !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("applyCalls = %d, want 0", repo.applyCalls)
	}
}

func TestOutdentBlockMovesItAfterItsParent(t *testing.T) {
	blocks := map[ID]Block{
		"root":  {ID: "root", Content: "Root"},
		"mid":   {ID: "mid", ParentID: "root", Position: 0, Content: "Mid"},
		"leaf":  {ID: "leaf", ParentID: "mid", Position: 0, Content: "Leaf"},
		"after": {ID: "after", ParentID: "root", Position: 65536, Content: "After"},
	}
	repo := &fakeRepo{
		blocks: blocks,
		subtrees: map[ID][]Block{
			"root": {blocks["root"], blocks["mid"], blocks["leaf"], blocks["after"]},
			"mid":  {blocks["mid"], blocks["leaf"]},
		},
	}
	svc := NewService(nil, repo, func() ID { return "new-1" }, nil)

	if err := svc.OutdentBlock(t.Context(), "leaf"); err != nil {
		t.Fatalf("OutdentBlock: %v", err)
	}
	move := repo.applyOps[0]
	// Anchored on the old parent, so it lands directly after it and the
	// parent's following siblings keep their places.
	if move.Kind != OpMove || move.BlockID != "leaf" || move.Parent != "root" || move.After != "mid" {
		t.Errorf("move op = %+v, want move of leaf under root after mid", move)
	}
}

func TestOutdentChildOfTopicRootIsRefused(t *testing.T) {
	svc, repo := outlineService(t)

	// Outdenting here would make the block a second topic root.
	err := svc.OutdentBlock(t.Context(), "first")
	if !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("applyCalls = %d, want 0", repo.applyCalls)
	}
}

func TestMergeBlocksMovesChildrenBeforeDeletingTheSource(t *testing.T) {
	blocks := map[ID]Block{
		"root":   {ID: "root", Content: "Root"},
		"target": {ID: "target", ParentID: "root", Position: 0, Content: "Head "},
		"source": {ID: "source", ParentID: "root", Position: 65536, Content: "tail"},
		"kid-a":  {ID: "kid-a", ParentID: "source", Position: 0, Content: "A"},
		"kid-b":  {ID: "kid-b", ParentID: "source", Position: 65536, Content: "B"},
	}
	repo := &fakeRepo{
		blocks: blocks,
		subtrees: map[ID][]Block{
			"source": {blocks["source"], blocks["kid-a"], blocks["kid-b"]},
		},
	}
	svc := NewService(nil, repo, func() ID { return "new-1" }, nil)

	if err := svc.MergeBlocks(t.Context(), "target", "source"); err != nil {
		t.Fatalf("MergeBlocks: %v", err)
	}

	// Order is load-bearing: applyDelete tombstones the whole subtree, so the
	// children must be reparented before the delete runs. prepareOps keeps
	// this order because every id here is real — there are no temp
	// references, so orderParentsFirst leaves a batch of equals alone.
	kinds := make([]OpKind, 0, len(repo.applyOps))
	for _, op := range repo.applyOps {
		kinds = append(kinds, op.Kind)
	}
	want := []OpKind{OpEdit, OpMove, OpMove, OpDelete}
	if !slices.Equal(kinds, want) {
		t.Fatalf("op kinds = %v, want %v", kinds, want)
	}
	if got := repo.applyOps[0].Content; got != "Head tail" {
		t.Errorf("merged content = %q, want %q", got, "Head tail")
	}
	for _, op := range repo.applyOps[1:3] {
		if op.Parent != "target" {
			t.Errorf("child move %+v, want parent target", op)
		}
	}
	if repo.applyOps[3].BlockID != "source" {
		t.Errorf("delete op = %+v, want the source", repo.applyOps[3])
	}
}

func TestMergeBlocksRefusesMergingIntoItself(t *testing.T) {
	svc, repo := outlineService(t)

	if err := svc.MergeBlocks(t.Context(), "first", "first"); !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("applyCalls = %d, want 0", repo.applyCalls)
	}
}

func TestDeleteBlockEmitsOneDelete(t *testing.T) {
	svc, repo := outlineService(t)

	if err := svc.DeleteBlock(t.Context(), "second"); err != nil {
		t.Fatalf("DeleteBlock: %v", err)
	}
	if len(repo.applyOps) != 1 || repo.applyOps[0].Kind != OpDelete || repo.applyOps[0].BlockID != "second" {
		t.Errorf("ops = %+v, want a single delete of second", repo.applyOps)
	}
}

func TestMoveBlockPassesTheAnchorThrough(t *testing.T) {
	svc, repo := outlineService(t)

	if err := svc.MoveBlock(t.Context(), "second", "root", "first"); err != nil {
		t.Fatalf("MoveBlock: %v", err)
	}
	move := repo.applyOps[0]
	if move.Kind != OpMove || move.BlockID != "second" || move.Parent != "root" || move.After != "first" {
		t.Errorf("move op = %+v, want move of second under root after first", move)
	}
}
