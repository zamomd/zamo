package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	topics     []Block // what GetTopics returns
	topicsErr  error
	graph      TopicGraph // what GetTopicGraph returns
	graphErr   error
	subtrees   map[ID][]Block // per-root subtree GetSubtree returns
	subtreeErr error

	unreviewedUnder int  // what CountUnreviewedUnder returns
	applyNothing    bool // ApplyOps reports that nothing changed
	gotSubtree      ID   // the root GetSubtree was asked for

	backlinks    []Backlink // what GetBacklinks returns
	backlinksErr error

	referencing       []Block            // what BlocksReferencingTopic returns for any key not in referencingByKey
	referencingByKey  map[string][]Block // per-key answers, for a batch renaming more than one topic
	referencingErr    error
	gotReferencingKey string // the title key BlocksReferencingTopic was last asked for

	blocks    map[ID]Block // what GetBlocks serves
	blocksErr error

	applyGroupID ID
	applyMeta    WriteMeta
	applyOps     []Op
	applyErr     error
	applyCalls   int

	undoneGroup  ID
	undoGroupErr error

	revisions    map[ID][]Revision // what ListRevisions serves, by block id
	revisionsErr error

	deletedBlockID, deletedRevID ID
	deleteRevisionErr            error

	rejectedBlock  ID
	rejectBlockErr error
	rejectedPage   ID
	rejectPageErr  error

	markedBlockReviewed  ID
	markBlockReviewedErr error
	markedGroupReviewed  ID
	markGroupReviewedErr error
	markedPageReviewed   ID
	markPageReviewedErr  error

	unreviewed    UnreviewedChanges
	unreviewedErr error

	searchErr error

	aliases     []TopicAlias      // what ListTopicAliases returns
	props       map[ID]TopicProps // what SetTopicProps stored, by topic
	setPropsErr error
}

func (f *fakeRepo) GetTopicProps(_ context.Context, topic ID) (TopicProps, error) {
	return f.props[topic], nil
}

func (f *fakeRepo) SetTopicProps(_ context.Context, topic ID, props TopicProps) error {
	if f.setPropsErr != nil {
		return f.setPropsErr
	}
	if f.props == nil {
		f.props = map[ID]TopicProps{}
	}
	f.props[topic] = props
	return nil
}

func (f *fakeRepo) ListTopicAliases(context.Context) ([]TopicAlias, error) {
	return f.aliases, nil
}

func (f *fakeRepo) ListTopicTags(context.Context) ([]TopicTag, error) {
	return nil, nil
}

func (f *fakeRepo) GetTopics(context.Context) ([]Block, error) { return f.topics, f.topicsErr }

func (f *fakeRepo) GetTopicGraph(context.Context) (TopicGraph, error) {
	return f.graph, f.graphErr
}

func (f *fakeRepo) GetSubtree(_ context.Context, root ID) ([]Block, error) {
	f.gotSubtree = root
	if f.subtreeErr != nil {
		return nil, f.subtreeErr
	}
	return f.subtrees[root], nil
}

func (f *fakeRepo) GetBacklinks(_ context.Context, target ID) ([]Backlink, error) {
	return f.backlinks, f.backlinksErr
}

func (f *fakeRepo) BlocksReferencingTopic(_ context.Context, titleKey string) ([]Block, error) {
	f.gotReferencingKey = titleKey
	if blocks, ok := f.referencingByKey[titleKey]; ok {
		return blocks, f.referencingErr
	}
	return f.referencing, f.referencingErr
}

func (f *fakeRepo) GetBlocks(_ context.Context, ids []ID) (map[ID]Block, error) {
	if f.blocksErr != nil {
		return nil, f.blocksErr
	}
	out := make(map[ID]Block, len(ids))
	for _, id := range ids {
		if b, ok := f.blocks[id]; ok {
			out[id] = b
		}
	}
	return out, nil
}

func (f *fakeRepo) CountUnreviewedUnder(context.Context, ID) (int, error) {
	return f.unreviewedUnder, nil
}

func (f *fakeRepo) ApplyOps(_ context.Context, groupID ID, meta WriteMeta, ops []Op) (bool, error) {
	f.applyCalls++
	f.applyGroupID, f.applyMeta, f.applyOps = groupID, meta, ops
	return !f.applyNothing, f.applyErr
}

func (f *fakeRepo) ListRevisions(_ context.Context, blockID ID) ([]Revision, error) {
	return f.revisions[blockID], f.revisionsErr
}

func (f *fakeRepo) DeleteRevision(_ context.Context, blockID, revID ID) error {
	f.deletedBlockID, f.deletedRevID = blockID, revID
	return f.deleteRevisionErr
}

func (f *fakeRepo) UndoGroup(_ context.Context, groupID ID) error {
	f.undoneGroup = groupID
	return f.undoGroupErr
}

func (f *fakeRepo) RejectBlock(_ context.Context, blockID ID) error {
	f.rejectedBlock = blockID
	return f.rejectBlockErr
}

func (f *fakeRepo) RejectPage(_ context.Context, topicID ID) error {
	f.rejectedPage = topicID
	return f.rejectPageErr
}

func (f *fakeRepo) MarkBlockReviewed(_ context.Context, blockID ID) error {
	f.markedBlockReviewed = blockID
	return f.markBlockReviewedErr
}

func (f *fakeRepo) MarkGroupReviewed(_ context.Context, groupID ID) error {
	f.markedGroupReviewed = groupID
	return f.markGroupReviewedErr
}

func (f *fakeRepo) MarkPageReviewed(_ context.Context, topicID ID) error {
	f.markedPageReviewed = topicID
	return f.markPageReviewedErr
}

func (f *fakeRepo) ListUnreviewed(context.Context) (UnreviewedChanges, error) {
	return f.unreviewed, f.unreviewedErr
}

func (f *fakeRepo) Search(context.Context, string) ([]SearchHit, error) { return nil, f.searchErr }

func TestApplyOpsForcesGroupAndMapping(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	ops := []Op{
		addOp("child", "root", "Child"),
		addOp("root", "", "Root"),
	}
	got, err := svc.ApplyOps(t.Context(), WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "batch"}, ops)
	require.NoError(t, err)
	assert.NotEmpty(t, got.GroupID, "want a generated group id")
	assert.Len(t, got.IDs, 2)
	assert.NotEmpty(t, got.IDs["root"], "want every temp id mapped to a real id")
	assert.NotEmpty(t, got.IDs["child"], "want every temp id mapped to a real id")
	assert.Equal(t, got.GroupID, repo.applyGroupID)
	require.Len(t, repo.applyOps, 2)
	require.Equal(t, "Root", repo.applyOps[0].Content, "want the root add first (parent-first)")
	assert.Equal(t, got.IDs["root"], repo.applyOps[0].BlockID)
	assert.Equal(t, string(got.IDs["root"]), repo.applyOps[1].Parent, "want the resolved root id")
}

func TestApplyOpsRejectsEmptyTitle(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	_, err := svc.ApplyOps(t.Context(),
		WriteMeta{Author: AuthorOwner, Source: SourceGUIManual, Title: "   "},
		[]Op{addOp("t1", "", "A")})
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls, "storage ApplyOps called for an empty title")
}

func TestApplyOpsPropagatesRepoError(t *testing.T) {
	boom := errors.New("disk full")
	repo := &fakeRepo{applyErr: boom}
	svc := NewService(repo, seqIDs())

	meta := WriteMeta{Author: AuthorOwner, Source: SourceGUIManual, Title: "t"}
	_, err := svc.ApplyOps(t.Context(), meta, []Op{addOp("t1", "", "A")})
	require.ErrorIs(t, err, boom)
}

func TestApplyOpsRejectsMalformedOps(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "t"}
	_, err := svc.ApplyOps(t.Context(), meta, []Op{{Kind: "explode"}})
	require.ErrorIs(t, err, ErrInvalidOps)
	assert.Zero(t, repo.applyCalls, "a malformed batch reached storage")
}

func TestCreateTopicIsAnOwnerWriteOfOneRoot(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	id, err := svc.CreateTopic(t.Context(), "Local-first", "")
	require.NoError(t, err)
	assert.NotEmpty(t, id, "CreateTopic returned no id, want the new root's")
	require.Len(t, repo.applyOps, 1)
	op := repo.applyOps[0]
	assert.Equal(t, OpAdd, op.Kind)
	assert.Empty(t, op.Parent)
	assert.Equal(t, id, op.BlockID)
	// Verbatim: the title_key storage derives from it is what the [[Topic]]
	// reference that asked for this topic resolves by.
	assert.Equal(t, "Local-first", op.Content)
	assert.Equal(t, AuthorOwner, repo.applyMeta.Author, "want an owner write born reviewed")
}

func TestCreateTopicTakesItsMode(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	_, err := svc.CreateTopic(t.Context(), "Go Contexts", ModeDoc)
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 1)
	assert.Equal(t, ModeDoc, repo.applyOps[0].Mode)
}

func TestCreateTopicDefaultsToOutline(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	_, err := svc.CreateTopic(t.Context(), "Go Contexts", "")
	require.NoError(t, err)
	assert.Equal(t, ModeOutline, repo.applyOps[0].Mode)
}

func TestCreateTopicRefusesANameThatResolvesToNothing(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	// A bracketed name belongs here too: the root would be created and the
	// [[...]] that asked for it would still resolve to nothing.
	for _, name := range []string{"", "   ", "###", "Notes [WIP]"} {
		_, err := svc.CreateTopic(t.Context(), name, "")
		require.ErrorIs(t, err, ErrInvalidOps, "CreateTopic(%q)", name)
	}
	assert.Zero(t, repo.applyCalls, "a nameless topic reached storage")
}

func TestCreateTopicReportsATakenName(t *testing.T) {
	repo := &fakeRepo{applyErr: ErrDuplicateTopic}
	svc := NewService(repo, seqIDs())

	_, err := svc.CreateTopic(t.Context(), "Local-first", "")
	require.ErrorIs(t, err, ErrDuplicateTopic)
}

func TestEditBlockContentIsOwnerReviewedWrite(t *testing.T) {
	repo := &fakeRepo{blocks: map[ID]Block{"01A": {ID: "01A", ParentID: "01ROOT", Content: "old text"}}}
	svc := NewService(repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01A", "new text"))
	assert.Equal(t, AuthorOwner, repo.applyMeta.Author)
	assert.Equal(t, SourceGUIManual, repo.applyMeta.Source)
	require.Len(t, repo.applyOps, 1)
	op := repo.applyOps[0]
	assert.Equal(t, OpEdit, op.Kind)
	assert.Equal(t, ID("01A"), op.BlockID)
	assert.Equal(t, "new text", op.Content)
}

func TestEditBlockContentRefusesAMissingBlock(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	require.ErrorIs(t, svc.EditBlockContent(t.Context(), "01A", "new text"), ErrNotFound)
	assert.Zero(t, repo.applyCalls, "a batch was applied for a missing block")
}

func TestServiceReviewPassThroughs(t *testing.T) {
	repo := &fakeRepo{unreviewed: UnreviewedChanges{Blocks: []UnreviewedBlock{{Head: Revision{ID: "R1"}}}}}
	svc := NewService(repo, seqIDs())

	got, err := svc.Unreviewed(t.Context())
	require.NoError(t, err)
	assert.Len(t, got.Blocks, 1)
	require.NoError(t, svc.AcceptBlock(t.Context(), "B1"))
	assert.Equal(t, ID("B1"), repo.markedBlockReviewed)
	require.NoError(t, svc.AcceptGroup(t.Context(), "G1"))
	assert.Equal(t, ID("G1"), repo.markedGroupReviewed)
	require.NoError(t, svc.AcceptPage(t.Context(), "T1"))
	assert.Equal(t, ID("T1"), repo.markedPageReviewed)
	require.NoError(t, svc.RejectBlock(t.Context(), "B1"))
	assert.Equal(t, ID("B1"), repo.rejectedBlock)
	require.NoError(t, svc.RejectPage(t.Context(), "T1"))
	assert.Equal(t, ID("T1"), repo.rejectedPage)
	repo.revisions = map[ID][]Revision{"B1": {{ID: "R1", BlockID: "B1"}}}
	hist, err := svc.BlockHistory(t.Context(), "B1")
	require.NoError(t, err)
	assert.Len(t, hist, 1)
	require.NoError(t, svc.DeleteRevision(t.Context(), "B1", "R1"))
	assert.Equal(t, ID("B1"), repo.deletedBlockID)
	assert.Equal(t, ID("R1"), repo.deletedRevID)
}

func TestTopicGraphPassesThrough(t *testing.T) {
	want := TopicGraph{
		Topics: []TopicNode{{ID: "01A", Title: "Go", Blocks: 3}},
		Edges:  []TopicEdge{{From: "01A", To: "01B"}},
	}
	repo := &fakeRepo{graph: want}
	svc := NewService(repo, nil)

	got, err := svc.TopicGraph(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want.Topics, got.Topics)
	assert.Equal(t, want.Edges, got.Edges)

	boom := errors.New("boom")
	_, err = NewService(&fakeRepo{graphErr: boom}, nil).TopicGraph(t.Context())
	require.ErrorIs(t, err, boom)
}

func TestApplyOpsRefusesAIWritesWhileAIIsOff(t *testing.T) {
	off := func() bool { return false }

	tests := []struct {
		name       string
		meta       WriteMeta
		wantRefuse bool
	}{
		{
			name:       "an AI batch is refused",
			meta:       WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "from an agent"},
			wantRefuse: true,
		},
		{
			name:       "the owner's own batch still applies",
			meta:       WriteMeta{Author: AuthorOwner, Source: SourceGUIManual, Title: "by hand"},
			wantRefuse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := NewService(repo, seqIDs(), WithAIPolicy(off))

			_, err := svc.ApplyOps(t.Context(), tt.meta, []Op{{Kind: OpAdd, TempID: "t1", Content: "x"}})
			if tt.wantRefuse {
				require.ErrorIs(t, err, ErrAIDisabled)
				assert.Zero(t, repo.applyCalls, "a batch reached storage while AI is off")
			} else {
				require.NoError(t, err, "want the write to go through")
			}
		})
	}
}

func TestApplyOpsAsksThePolicyEveryTime(t *testing.T) {
	enabled := true
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs(), WithAIPolicy(func() bool { return enabled }))

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "batch"}
	ops := []Op{{Kind: OpAdd, TempID: "t1", Content: "x"}}

	_, err := svc.ApplyOps(t.Context(), meta, ops)
	require.NoError(t, err, "first write")
	enabled = false
	_, err = svc.ApplyOps(t.Context(), meta, ops)
	require.ErrorIs(t, err, ErrAIDisabled, "second write")
	assert.Equal(t, 1, repo.applyCalls, "want only the batch written while AI was on")
}

func TestApplyOpsAllowsAIWritesWithNoPolicy(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, seqIDs())

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "batch"}
	_, err := svc.ApplyOps(t.Context(), meta, []Op{{Kind: OpAdd, TempID: "t1", Content: "x"}})
	require.NoError(t, err)
}
