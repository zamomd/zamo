package knowledge

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeFileWriter struct {
	dir      string
	files    []ExportFile
	writeErr error
}

func (w *fakeFileWriter) WriteFiles(ctx context.Context, dir string, files []ExportFile) error {
	if w.writeErr != nil {
		return w.writeErr
	}
	w.dir = dir
	w.files = files
	return nil
}

type fakeRepo struct {
	topics     []Block // what GetTopics returns
	topicsErr  error
	graph      TopicGraph // what GetTopicGraph returns
	graphErr   error
	subtrees   map[ID][]Block // per-root subtree GetSubtree returns
	subtreeErr error
	gotSubtree ID // the root GetSubtree was asked for

	backlinks    []Backlink // what GetBacklinks returns
	backlinksErr error

	referencing       []Block // what BlocksReferencingTopic returns
	referencingErr    error
	gotReferencingKey string // the title key BlocksReferencingTopic was asked for

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

func (f *fakeRepo) ApplyOps(_ context.Context, groupID ID, meta WriteMeta, ops []Op) error {
	f.applyCalls++
	f.applyGroupID, f.applyMeta, f.applyOps = groupID, meta, ops
	return f.applyErr
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

func (f *fakeRepo) Search(context.Context, string) ([]Block, error) { return nil, f.searchErr }

type fakeLLM struct {
	tree              []Op
	treeErr           error
	gotTopic, gotHint string

	editOps        []Op
	editErr        error
	gotScope       []Block
	gotInstruction string

	pingErr error
}

func (l *fakeLLM) GenerateTree(_ context.Context, topic, hint string) ([]Op, error) {
	l.gotTopic, l.gotHint = topic, hint
	return l.tree, l.treeErr
}

func (l *fakeLLM) GenerateEdit(_ context.Context, scope []Block, instruction string) ([]Op, error) {
	l.gotScope, l.gotInstruction = scope, instruction
	return l.editOps, l.editErr
}

func (l *fakeLLM) Ping(_ context.Context) error { return l.pingErr }

func TestApplyOpsForcesGroupAndMapping(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(nil, repo, seqIDs(), nil)

	ops := []Op{
		addOp("child", "root", "Child"),
		addOp("root", "", "Root"),
	}
	got, err := svc.ApplyOps(t.Context(), WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "batch"}, ops)
	if err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}
	if got.GroupID == "" {
		t.Errorf("GroupID is empty, want a generated group id")
	}
	if len(got.IDs) != 2 || got.IDs["root"] == "" || got.IDs["child"] == "" {
		t.Errorf("IDs = %+v, want every temp id mapped to a real id", got.IDs)
	}
	if repo.applyGroupID != got.GroupID {
		t.Errorf("repo saw group %q, want the returned %q", repo.applyGroupID, got.GroupID)
	}
	if len(repo.applyOps) != 2 || repo.applyOps[0].Content != "Root" {
		t.Fatalf("repo ops = %+v, want the root add first (parent-first)", repo.applyOps)
	}
	if string(repo.applyOps[0].BlockID) != string(got.IDs["root"]) {
		t.Errorf("root BlockID = %q, want %q", repo.applyOps[0].BlockID, got.IDs["root"])
	}
	if repo.applyOps[1].Parent != string(got.IDs["root"]) {
		t.Errorf("child parent = %q, want the resolved root id", repo.applyOps[1].Parent)
	}
}

func TestApplyOpsRejectsEmptyTitle(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(nil, repo, seqIDs(), nil)

	_, err := svc.ApplyOps(t.Context(),
		WriteMeta{Author: AuthorOwner, Source: SourceGUIManual, Title: "   "},
		[]Op{addOp("t1", "", "A")})
	if !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("storage ApplyOps called %d times, want 0 for an empty title", repo.applyCalls)
	}
}

func TestApplyOpsPropagatesRepoError(t *testing.T) {
	boom := errors.New("disk full")
	repo := &fakeRepo{applyErr: boom}
	svc := NewService(nil, repo, seqIDs(), nil)

	meta := WriteMeta{Author: AuthorOwner, Source: SourceGUIManual, Title: "t"}
	_, err := svc.ApplyOps(t.Context(), meta, []Op{addOp("t1", "", "A")})
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the storage error", err)
	}
}

func TestApplyOpsRejectsMalformedOps(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(nil, repo, seqIDs(), nil)

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "t"}
	_, err := svc.ApplyOps(t.Context(), meta, []Op{{Kind: "explode"}})
	if !errors.Is(err, ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("a malformed batch reached storage: %d calls", repo.applyCalls)
	}
}

func TestEditBlockContentIsOwnerReviewedWrite(t *testing.T) {
	repo := &fakeRepo{blocks: map[ID]Block{"01A": {ID: "01A", ParentID: "01ROOT", Content: "old text"}}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01A", "new text"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if repo.applyMeta.Author != AuthorOwner || repo.applyMeta.Source != SourceGUIManual {
		t.Errorf("meta = %+v, want owner/gui_manual", repo.applyMeta)
	}
	if len(repo.applyOps) != 1 {
		t.Fatalf("ops = %+v, want exactly one edit", repo.applyOps)
	}
	op := repo.applyOps[0]
	if op.Kind != OpEdit || op.BlockID != "01A" || op.Content != "new text" {
		t.Errorf("op = %+v, want an edit of 01A with the new content", op)
	}
}

func TestEditBlockContentRefusesAMissingBlock(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01A", "new text"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("a batch was applied for a missing block: %d calls", repo.applyCalls)
	}
}

func TestEditWithAIUsesSubtreeAndAppliesDirectly(t *testing.T) {
	scope := []Block{{ID: "01A", Content: "old text"}}
	llm := &fakeLLM{editOps: []Op{{Kind: OpEdit, BlockID: "01A", Content: "revised"}}}
	repo := &fakeRepo{subtrees: map[ID][]Block{"01A": scope}}
	svc := NewService(llm, repo, seqIDs(), nil)

	got, err := svc.EditWithAI(t.Context(), "01A", "revise this block")
	if err != nil {
		t.Fatalf("EditWithAI: %v", err)
	}
	if repo.gotSubtree != "01A" {
		t.Errorf("read subtree of %q, want 01A", repo.gotSubtree)
	}
	if llm.gotInstruction != "revise this block" || len(llm.gotScope) != 1 || llm.gotScope[0].ID != "01A" {
		t.Errorf("model got scope %+v / instruction %q, want the scope and command", llm.gotScope, llm.gotInstruction)
	}
	if repo.applyMeta.Author != AuthorAI || repo.applyMeta.Source != SourceGUIEdit {
		t.Errorf("meta = %+v, want ai/gui_edit", repo.applyMeta)
	}
	if len(repo.applyOps) != 1 || repo.applyOps[0].Kind != OpEdit {
		t.Errorf("ops = %+v, want the edit from the model", repo.applyOps)
	}
	if got.GroupID == "" {
		t.Errorf("GroupID is empty, want a generated group id")
	}
}

func TestEditWithAISurfacesLLMError(t *testing.T) {
	llm := &fakeLLM{editErr: ErrLLMUnavailable}
	repo := &fakeRepo{subtrees: map[ID][]Block{"01A": {{ID: "01A", Content: "x"}}}}
	svc := NewService(llm, repo, seqIDs(), nil)

	if _, err := svc.EditWithAI(t.Context(), "01A", "do a thing"); !errors.Is(err, ErrLLMUnavailable) {
		t.Errorf("error = %v, want ErrLLMUnavailable", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("a batch was applied despite the LLM failure")
	}
}

func TestEditWithAIPropagatesReadError(t *testing.T) {
	boom := errors.New("db down")
	llm := &fakeLLM{editOps: []Op{{Kind: OpDelete, BlockID: "01A"}}}
	repo := &fakeRepo{subtreeErr: boom}
	svc := NewService(llm, repo, seqIDs(), nil)

	_, err := svc.EditWithAI(t.Context(), "01A", "do a thing")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the storage read error", err)
	}
	if llm.gotInstruction != "" {
		t.Errorf("the model was called despite a read failure")
	}
}

func TestEditWithAIRejectsEmptyEdit(t *testing.T) {
	llm := &fakeLLM{editOps: nil}
	repo := &fakeRepo{subtrees: map[ID][]Block{"01A": {{ID: "01A", Content: "x"}}}}
	svc := NewService(llm, repo, seqIDs(), nil)

	if _, err := svc.EditWithAI(t.Context(), "01A", "do nothing"); !errors.Is(err, ErrEmptyOps) {
		t.Errorf("error = %v, want ErrEmptyOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("an empty edit was applied: %d calls", repo.applyCalls)
	}
}

func TestExpandTopicAppliesDirectly(t *testing.T) {
	llm := &fakeLLM{tree: []Op{addOp("t1", "", "# Go contexts")}}
	repo := &fakeRepo{}
	svc := NewService(llm, repo, seqIDs(), nil)

	got, err := svc.ExpandTopic(t.Context(), "Go contexts", "cancellation")
	if err != nil {
		t.Fatalf("ExpandTopic: %v", err)
	}
	if llm.gotTopic != "Go contexts" || llm.gotHint != "cancellation" {
		t.Errorf("LLM asked for (%q, %q), want (Go contexts, cancellation)", llm.gotTopic, llm.gotHint)
	}
	badMeta := repo.applyMeta.Author != AuthorAI || repo.applyMeta.Source != SourceGUIExpand ||
		repo.applyMeta.Title != "Go contexts"
	if badMeta {
		t.Errorf("meta = %+v, want ai/gui_expand titled by the topic", repo.applyMeta)
	}
	if got.GroupID == "" {
		t.Errorf("GroupID is empty, want a generated group id")
	}
}

func TestExpandTopicSurfacesLLMError(t *testing.T) {
	llm := &fakeLLM{treeErr: ErrLLMUnavailable}
	repo := &fakeRepo{}
	svc := NewService(llm, repo, seqIDs(), nil)

	_, err := svc.ExpandTopic(t.Context(), "Go contexts", "")
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Errorf("error = %v, want ErrLLMUnavailable", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("a batch was applied despite the LLM failure")
	}
}

func TestExpandTopicRejectsEmptyTree(t *testing.T) {
	llm := &fakeLLM{tree: nil}
	repo := &fakeRepo{}
	svc := NewService(llm, repo, seqIDs(), nil)

	if _, err := svc.ExpandTopic(t.Context(), "Go contexts", ""); !errors.Is(err, ErrEmptyOps) {
		t.Errorf("error = %v, want ErrEmptyOps", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("an empty expansion was applied")
	}
}

func TestServiceReviewPassThroughs(t *testing.T) {
	repo := &fakeRepo{unreviewed: UnreviewedChanges{Blocks: []UnreviewedBlock{{Head: Revision{ID: "R1"}}}}}
	svc := NewService(nil, repo, seqIDs(), nil)

	if got, err := svc.Unreviewed(t.Context()); err != nil || len(got.Blocks) != 1 {
		t.Errorf("Unreviewed = %+v, %v, want the repo's changes", got, err)
	}
	if err := svc.AcceptBlock(t.Context(), "B1"); err != nil || repo.markedBlockReviewed != "B1" {
		t.Errorf("AcceptBlock did not reach MarkBlockReviewed(B1): err=%v got=%q", err, repo.markedBlockReviewed)
	}
	if err := svc.AcceptGroup(t.Context(), "G1"); err != nil || repo.markedGroupReviewed != "G1" {
		t.Errorf("AcceptGroup did not reach MarkGroupReviewed(G1): err=%v got=%q", err, repo.markedGroupReviewed)
	}
	if err := svc.AcceptPage(t.Context(), "T1"); err != nil || repo.markedPageReviewed != "T1" {
		t.Errorf("AcceptPage did not reach MarkPageReviewed(T1): err=%v got=%q", err, repo.markedPageReviewed)
	}
	if err := svc.RejectBlock(t.Context(), "B1"); err != nil || repo.rejectedBlock != "B1" {
		t.Errorf("RejectBlock did not reach RejectBlock(B1): err=%v got=%q", err, repo.rejectedBlock)
	}
	if err := svc.RejectPage(t.Context(), "T1"); err != nil || repo.rejectedPage != "T1" {
		t.Errorf("RejectPage did not reach RejectPage(T1): err=%v got=%q", err, repo.rejectedPage)
	}
	repo.revisions = map[ID][]Revision{"B1": {{ID: "R1", BlockID: "B1"}}}
	if hist, err := svc.BlockHistory(t.Context(), "B1"); err != nil || len(hist) != 1 {
		t.Errorf("BlockHistory = %+v, %v, want the repo's revisions", hist, err)
	}
	err := svc.DeleteRevision(t.Context(), "B1", "R1")
	if err != nil || repo.deletedBlockID != "B1" || repo.deletedRevID != "R1" {
		t.Errorf("DeleteRevision did not reach DeleteRevision(B1,R1): err=%v got=(%q,%q)",
			err, repo.deletedBlockID, repo.deletedRevID)
	}
}

func TestTopicGraphPassesThrough(t *testing.T) {
	want := TopicGraph{
		Topics: []TopicNode{{ID: "01A", Title: "Go", Blocks: 3}},
		Edges:  []TopicEdge{{From: "01A", To: "01B"}},
	}
	repo := &fakeRepo{graph: want}
	svc := NewService(nil, repo, nil, nil)

	got, err := svc.TopicGraph(t.Context())
	if err != nil {
		t.Fatalf("TopicGraph: %v", err)
	}
	if len(got.Topics) != 1 || got.Topics[0] != want.Topics[0] {
		t.Errorf("topics = %+v, want %+v", got.Topics, want.Topics)
	}
	if len(got.Edges) != 1 || got.Edges[0] != want.Edges[0] {
		t.Errorf("edges = %+v, want %+v", got.Edges, want.Edges)
	}

	boom := errors.New("boom")
	if _, err := NewService(nil, &fakeRepo{graphErr: boom}, nil, nil).TopicGraph(t.Context()); !errors.Is(err, boom) {
		t.Errorf("error = %v, want %v", err, boom)
	}
}

func TestServiceExportMarkdownWritesThroughWriter(t *testing.T) {
	repo := &fakeRepo{
		topics: []Block{
			blk("t1", "", 0, "# Alpha"),
			blk("c1", "t1", 0, "child of alpha"),
			blk("t2", "", 1, "# Beta"),
		},
		subtrees: map[ID][]Block{
			"t1": {blk("t1", "", 0, "# Alpha"), blk("c1", "t1", 0, "child of alpha")},
			"t2": {blk("t2", "", 1, "# Beta")},
		},
	}
	writer := &fakeFileWriter{}
	svc := NewService(nil, repo, seqIDs(), writer)

	if err := svc.ExportMarkdown(context.Background(), "/export/here"); err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}

	if writer.dir != "/export/here" {
		t.Errorf("writer got dir %q, want the dir passed to ExportMarkdown", writer.dir)
	}
	want := []ExportFile{
		{Name: "alpha.md", Content: "- # Alpha\n  - child of alpha\n"},
		{Name: "beta.md", Content: "- # Beta\n"},
	}
	if !slices.Equal(writer.files, want) {
		t.Errorf("writer got files %+v, want %+v", writer.files, want)
	}
}

func TestServiceExportMarkdownSurfacesWriteError(t *testing.T) {
	boom := errors.New("disk full")
	repo := &fakeRepo{
		topics:   []Block{blk("t1", "", 0, "# Alpha")},
		subtrees: map[ID][]Block{"t1": {blk("t1", "", 0, "# Alpha")}},
	}
	svc := NewService(nil, repo, seqIDs(), &fakeFileWriter{writeErr: boom})

	err := svc.ExportMarkdown(context.Background(), "/export/here")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the writer error surfaced", err)
	}
}

func TestServiceExportMarkdownEmptyBase(t *testing.T) {
	repo := &fakeRepo{topics: nil}
	writer := &fakeFileWriter{}
	svc := NewService(nil, repo, seqIDs(), writer)

	if err := svc.ExportMarkdown(context.Background(), "/export/here"); err != nil {
		t.Fatalf("empty base must export cleanly, got %v", err)
	}
	if len(writer.files) != 0 {
		t.Errorf("handed %d files for an empty base, want 0", len(writer.files))
	}
}

func TestServiceExportMarkdownPropagatesReadError(t *testing.T) {
	boom := errors.New("db down")
	repo := &fakeRepo{
		topics:     []Block{blk("t1", "", 0, "# Alpha")},
		subtreeErr: boom,
	}
	writer := &fakeFileWriter{}
	svc := NewService(nil, repo, seqIDs(), writer)

	err := svc.ExportMarkdown(context.Background(), "/export/here")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the storage read error surfaced", err)
	}
	if writer.files != nil {
		t.Errorf("writer was handed %+v; a read failure must write nothing", writer.files)
	}
}
