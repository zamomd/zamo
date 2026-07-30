package knowledge

import (
	"errors"
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
