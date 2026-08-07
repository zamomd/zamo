package knowledge

import (
	"errors"
	"testing"
)

func TestSetTopicModeToDocCollapsesTheSubtree(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {
			{ID: "t", Content: "Go Contexts"},
			{ID: "a", ParentID: "t", Position: 1, Content: "First."},
		},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "t", ModeDoc); err != nil {
		t.Fatalf("SetTopicMode: %v", err)
	}
	if !repo.applyMeta.Atomic {
		t.Error("conversion batch is not atomic")
	}
	if len(repo.applyOps) != 2 {
		t.Fatalf("ops = %#v, want an edit and a delete", repo.applyOps)
	}
	edit := repo.applyOps[0]
	if edit.Kind != OpEdit || edit.BlockID != "t" || edit.Mode != ModeDoc {
		t.Errorf("first op = %#v, want an edit of t into doc mode", edit)
	}
	if want := "Go Contexts\n\n- First.\n"; edit.Content != want {
		t.Errorf("content = %q, want %q", edit.Content, want)
	}
	if del := repo.applyOps[1]; del.Kind != OpDelete || del.BlockID != "a" {
		t.Errorf("second op = %#v, want a delete of a", del)
	}
}

func TestSetTopicModeToOutlineSplitsTheDocument(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: "Go Contexts\n\n- First.\n  - Nested.\n", Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "t", ModeOutline); err != nil {
		t.Fatalf("SetTopicMode: %v", err)
	}
	edit := repo.applyOps[0]
	if edit.Kind != OpEdit || edit.Content != "Go Contexts" || edit.Mode != ModeOutline {
		t.Fatalf("first op = %#v, want an edit of t back to its name", edit)
	}
	if len(repo.applyOps) != 3 {
		t.Fatalf("ops = %#v, want an edit and two adds", repo.applyOps)
	}
	first, nested := repo.applyOps[1], repo.applyOps[2]
	if first.Kind != OpAdd || first.Parent != "t" || first.Content != "First." {
		t.Errorf("second op = %#v, want an add of \"First.\" under t", first)
	}
	if nested.Kind != OpAdd || nested.Parent != string(first.BlockID) || nested.Content != "Nested." {
		t.Errorf("third op = %#v, want an add of \"Nested.\" under the first", nested)
	}
}

func TestSetTopicModeRefusesANonRoot(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"c": {{ID: "c", ParentID: "t", Content: "child"}},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "c", ModeDoc); !errors.Is(err, ErrInvalidOps) {
		t.Fatalf("SetTopicMode = %v, want ErrInvalidOps", err)
	}
}

// TestSetTopicModeToOutlineReparentsAfterAHeadingJumpsBackOut covers
// expandToOutline's parents map for a section nested deeper than its
// predecessor (an h3 under an h2) followed by one back at the shallower
// level. The later h2 must attach to the root, not linger under the h3's
// stale depth-1 entry.
func TestSetTopicModeToOutlineReparentsAfterAHeadingJumpsBackOut(t *testing.T) {
	doc := "Go Contexts\n\n## A\n\nA text.\n\n### AA\n\nAA text.\n\n## B\n\nB text.\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "t", ModeOutline); err != nil {
		t.Fatalf("SetTopicMode: %v", err)
	}
	// ops[0] is the root edit; ops[1:] are, in order: "## A", "A text.",
	// "### AA", "AA text.", "## B", "B text.".
	if len(repo.applyOps) != 7 {
		t.Fatalf("ops = %#v, want an edit and six adds", repo.applyOps)
	}
	a, aText, aa, aaText, b, bText := repo.applyOps[1], repo.applyOps[2], repo.applyOps[3],
		repo.applyOps[4], repo.applyOps[5], repo.applyOps[6]

	if a.Content != "## A" || a.Parent != "t" {
		t.Errorf("A = %#v, want a child of the root", a)
	}
	if aText.Content != "A text." || aText.Parent != string(a.BlockID) {
		t.Errorf("A text. = %#v, want a child of A", aText)
	}
	if aa.Content != "### AA" || aa.Parent != string(a.BlockID) {
		t.Errorf("AA = %#v, want a child of A, alongside \"A text.\"", aa)
	}
	if aaText.Content != "AA text." || aaText.Parent != string(aa.BlockID) {
		t.Errorf("AA text. = %#v, want a child of AA", aaText)
	}
	if b.Content != "## B" || b.Parent != "t" {
		t.Errorf("B = %#v, want a child of the root, not left under AA", b)
	}
	if bText.Content != "B text." || bText.Parent != string(b.BlockID) {
		t.Errorf("B text. = %#v, want a child of B", bText)
	}
}

// TestSetTopicModeToOutlineNestsUnderTheNearestAncestorWhenAHeadingSkipsALevel
// covers a document that jumps from an h2 straight to an h4, leaving depth 1
// empty. The h4 belongs to the h2 above it, not to the topic root.
func TestSetTopicModeToOutlineNestsUnderTheNearestAncestorWhenAHeadingSkipsALevel(t *testing.T) {
	doc := "Go Contexts\n\n## A\n\n#### B\n\nB text.\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "t", ModeOutline); err != nil {
		t.Fatalf("SetTopicMode: %v", err)
	}
	if len(repo.applyOps) != 4 {
		t.Fatalf("ops = %#v, want an edit and three adds", repo.applyOps)
	}
	a, b, bText := repo.applyOps[1], repo.applyOps[2], repo.applyOps[3]

	if a.Content != "## A" || a.Parent != "t" {
		t.Errorf("A = %#v, want a child of the root", a)
	}
	if b.Content != "#### B" || b.Parent != string(a.BlockID) {
		t.Errorf("B = %#v, want a child of A, the nearest heading above it", b)
	}
	if bText.Content != "B text." || bText.Parent != string(b.BlockID) {
		t.Errorf("B text. = %#v, want a child of B", bText)
	}
}

// TestSetTopicModeToOutlineNestsUnderTheNearestAncestorWhenAListSkipsALevel
// covers a nested list item indented four spaces, as several editors write it.
// The two levels of indent it reads as must still land it under the item above.
func TestSetTopicModeToOutlineNestsUnderTheNearestAncestorWhenAListSkipsALevel(t *testing.T) {
	doc := "Go Contexts\n\n- top\n\n    - deep\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "t", ModeOutline); err != nil {
		t.Fatalf("SetTopicMode: %v", err)
	}
	if len(repo.applyOps) != 3 {
		t.Fatalf("ops = %#v, want an edit and two adds", repo.applyOps)
	}
	top, deep := repo.applyOps[1], repo.applyOps[2]

	if top.Content != "top" || top.Parent != "t" {
		t.Errorf("top = %#v, want a child of the root", top)
	}
	if deep.Content != "deep" || deep.Parent != string(top.BlockID) {
		t.Errorf("deep = %#v, want a child of top", deep)
	}
}

// TestSetTopicModeToOutlineDoesNotNestUnderAnAbandonedBranch covers the two
// shapes meeting: a heading pulls back out, then the next one skips a level
// going deeper. The depth it lands on was last filled inside the branch the
// document already left, which must not make that block its parent.
func TestSetTopicModeToOutlineDoesNotNestUnderAnAbandonedBranch(t *testing.T) {
	doc := "Go Contexts\n\n## A\n\nA text.\n\n## B\n\n#### C\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "t", ModeOutline); err != nil {
		t.Fatalf("SetTopicMode: %v", err)
	}
	if len(repo.applyOps) != 5 {
		t.Fatalf("ops = %#v, want an edit and four adds", repo.applyOps)
	}
	a, aText, b, c := repo.applyOps[1], repo.applyOps[2], repo.applyOps[3], repo.applyOps[4]

	if a.Content != "## A" || a.Parent != "t" {
		t.Errorf("A = %#v, want a child of the root", a)
	}
	if aText.Content != "A text." || aText.Parent != string(a.BlockID) {
		t.Errorf("A text. = %#v, want a child of A", aText)
	}
	if b.Content != "## B" || b.Parent != "t" {
		t.Errorf("B = %#v, want a child of the root", b)
	}
	if c.Content != "#### C" || c.Parent != string(b.BlockID) {
		t.Errorf("C = %#v, want a child of B, not of anything under A", c)
	}
}

func TestSetTopicModeToTheModeAlreadyHeldWritesNothing(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: "# Doc", Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if _, err := svc.SetTopicMode(t.Context(), "t", ModeDoc); err != nil {
		t.Fatalf("SetTopicMode: %v", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("applyCalls = %d, want 0", repo.applyCalls)
	}
}

func TestAIRewriteChangingTheFirstLineRewritesBacklinks(t *testing.T) {
	repo := &fakeRepo{
		subtrees: map[ID][]Block{
			"t": {{ID: "t", Content: "# Go Contexts\n\nBody.", Mode: ModeDoc}},
		},
		blocks: map[ID]Block{
			"t": {ID: "t", Content: "# Go Contexts\n\nBody.", Mode: ModeDoc},
		},
		referencing: []Block{{ID: "s", ParentID: "other", Content: "See [[Go Contexts]]."}},
	}
	llm := &fakeLLM{document: "# Contexts in Go\n\nBody."}
	svc := NewService(llm, repo, seqIDs(), nil)

	if _, err := svc.EditWithAI(t.Context(), "t", "rename it"); err != nil {
		t.Fatalf("EditWithAI: %v", err)
	}
	if !repo.applyMeta.Atomic {
		t.Error("a rename expansion did not make the batch atomic")
	}
	var rewrote bool
	for _, op := range repo.applyOps {
		if op.BlockID == "s" && op.Content == "See [[Contexts in Go]]." {
			rewrote = true
		}
	}
	if !rewrote {
		t.Errorf("ops = %#v, want the backlink rewritten", repo.applyOps)
	}
}
