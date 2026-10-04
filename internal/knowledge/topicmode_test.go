package knowledge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetTopicModeToDocCollapsesTheSubtree(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {
			{ID: "t", Content: "Go Contexts"},
			{ID: "a", ParentID: "t", Position: 1, Content: "First."},
		},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeDoc)
	require.NoError(t, err)
	assert.True(t, repo.applyMeta.Atomic, "conversion batch is not atomic")
	require.Len(t, repo.applyOps, 2, "want an edit and a delete")
	edit := repo.applyOps[0]
	assert.Equal(t, OpEdit, edit.Kind)
	assert.Equal(t, ID("t"), edit.BlockID)
	assert.Equal(t, ModeDoc, edit.Mode)
	assert.Equal(t, "Go Contexts\n\n- First.\n", edit.Content)
	assert.Equal(t, OpDelete, repo.applyOps[1].Kind)
	assert.Equal(t, ID("a"), repo.applyOps[1].BlockID)
}

func TestSetTopicModeToOutlineSplitsTheDocument(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: "Go Contexts\n\n- First.\n  - Nested.\n", Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeOutline)
	require.NoError(t, err)
	edit := repo.applyOps[0]
	require.Equal(t, OpEdit, edit.Kind)
	require.Equal(t, "Go Contexts", edit.Content, "want an edit of t back to its name")
	require.Equal(t, ModeOutline, edit.Mode)
	require.Len(t, repo.applyOps, 3, "want an edit and two adds")
	first, nested := repo.applyOps[1], repo.applyOps[2]
	assert.Equal(t, OpAdd, first.Kind)
	assert.Equal(t, "t", first.Parent)
	assert.Equal(t, "First.", first.Content)
	assert.Equal(t, OpAdd, nested.Kind)
	assert.Equal(t, string(first.BlockID), nested.Parent, "want Nested. under the first")
	assert.Equal(t, "Nested.", nested.Content)
}

func TestSetTopicModeAwaitsReviewWithoutLyingAboutTheAuthor(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {
			{ID: "t", Content: "Go Contexts"},
			{ID: "a", ParentID: "t", Position: 1, Content: "First."},
		},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeDoc)
	require.NoError(t, err)
	meta := repo.applyMeta
	assert.Equal(t, AuthorOwner, meta.Author, "the owner made this change")
	// Atomic alone is inert on a batch born reviewed: every consumer of it
	// filters on reviewed = 0. The conversion has to reach the review queue for
	// the flag — and the whole-batch revert it buys — to mean anything.
	assert.False(t, meta.BornReviewed(), "the conversion is born reviewed, so nothing can revert it as one unit")
}

func TestOwnerWritesAreStillBornReviewed(t *testing.T) {
	assert.True(t, ownerMeta().BornReviewed(), "an ordinary owner write now waits in the review queue")
	assert.False(t, (WriteMeta{Author: AuthorAI}).BornReviewed(), "an AI write is born reviewed")
}

func TestSetTopicModeRefusesANonRoot(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"c": {{ID: "c", ParentID: "t", Content: "child"}},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "c", ModeDoc)
	require.ErrorIs(t, err, ErrInvalidOps)
}

func TestSetTopicModeToOutlineReparentsAfterAHeadingJumpsBackOut(t *testing.T) {
	doc := "Go Contexts\n\n## A\n\nA text.\n\n### AA\n\nAA text.\n\n## B\n\nB text.\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeOutline)
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 7, "want an edit and six adds")
	a, aText, aa, aaText, b, bText := repo.applyOps[1], repo.applyOps[2], repo.applyOps[3],
		repo.applyOps[4], repo.applyOps[5], repo.applyOps[6]

	assert.Equal(t, "## A", a.Content)
	assert.Equal(t, "t", a.Parent, "a child of the root")
	assert.Equal(t, "A text.", aText.Content)
	assert.Equal(t, string(a.BlockID), aText.Parent, "a child of A")
	assert.Equal(t, "### AA", aa.Content)
	assert.Equal(t, string(a.BlockID), aa.Parent, "a child of A, alongside \"A text.\"")
	assert.Equal(t, "AA text.", aaText.Content)
	assert.Equal(t, string(aa.BlockID), aaText.Parent, "a child of AA")
	assert.Equal(t, "## B", b.Content)
	assert.Equal(t, "t", b.Parent, "a child of the root, not left under AA")
	assert.Equal(t, "B text.", bText.Content)
	assert.Equal(t, string(b.BlockID), bText.Parent, "a child of B")
}

func TestSetTopicModeToOutlineNestsUnderTheNearestAncestorWhenAHeadingSkipsALevel(t *testing.T) {
	doc := "Go Contexts\n\n## A\n\n#### B\n\nB text.\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeOutline)
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 4, "want an edit and three adds")
	a, b, bText := repo.applyOps[1], repo.applyOps[2], repo.applyOps[3]

	assert.Equal(t, "## A", a.Content)
	assert.Equal(t, "t", a.Parent, "a child of the root")
	assert.Equal(t, "#### B", b.Content)
	assert.Equal(t, string(a.BlockID), b.Parent, "a child of A, the nearest heading above it")
	assert.Equal(t, "B text.", bText.Content)
	assert.Equal(t, string(b.BlockID), bText.Parent, "a child of B")
}

func TestSetTopicModeToOutlineNestsUnderTheNearestAncestorWhenAListSkipsALevel(t *testing.T) {
	doc := "Go Contexts\n\n- top\n\n    - deep\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeOutline)
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 3, "want an edit and two adds")
	top, deep := repo.applyOps[1], repo.applyOps[2]

	assert.Equal(t, "top", top.Content)
	assert.Equal(t, "t", top.Parent, "a child of the root")
	assert.Equal(t, "deep", deep.Content)
	assert.Equal(t, string(top.BlockID), deep.Parent, "a child of top")
}

func TestSetTopicModeToOutlineDoesNotNestUnderAnAbandonedBranch(t *testing.T) {
	doc := "Go Contexts\n\n## A\n\nA text.\n\n## B\n\n#### C\n"
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: doc, Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeOutline)
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 5, "want an edit and four adds")
	a, aText, b, c := repo.applyOps[1], repo.applyOps[2], repo.applyOps[3], repo.applyOps[4]

	assert.Equal(t, "## A", a.Content)
	assert.Equal(t, "t", a.Parent, "a child of the root")
	assert.Equal(t, "A text.", aText.Content)
	assert.Equal(t, string(a.BlockID), aText.Parent, "a child of A")
	assert.Equal(t, "## B", b.Content)
	assert.Equal(t, "t", b.Parent, "a child of the root")
	assert.Equal(t, "#### C", c.Content)
	assert.Equal(t, string(b.BlockID), c.Parent, "a child of B, not of anything under A")
}

func TestSetTopicModeToTheModeAlreadyHeldWritesNothing(t *testing.T) {
	repo := &fakeRepo{subtrees: map[ID][]Block{
		"t": {{ID: "t", Content: "# Doc", Mode: ModeDoc}},
	}}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.SetTopicMode(t.Context(), "t", ModeDoc)
	require.NoError(t, err)
	assert.Zero(t, repo.applyCalls)
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
	svc := NewService(llm, repo, seqIDs())

	_, err := svc.EditWithAI(t.Context(), guiEdit("t", "rename it"))
	require.NoError(t, err)
	assert.True(t, repo.applyMeta.Atomic, "a rename expansion did not make the batch atomic")
	var rewrote bool
	for _, op := range repo.applyOps {
		if op.BlockID == "s" && op.Content == "See [[Contexts in Go]]." {
			rewrote = true
		}
	}
	assert.True(t, rewrote, "ops = %#v, want the backlink rewritten", repo.applyOps)
}
