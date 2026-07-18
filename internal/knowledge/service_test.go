package knowledge

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

// clockAt returns a now-source pinned to a fixed instant.
func clockAt(ts time.Time) func() time.Time { return func() time.Time { return ts } }

// fakeFileWriter is a FileWriter double: it records the files it was handed
// instead of touching disk, so export tests assert what the domain produced
// without a filesystem.
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

// fakeRepo is a Repository double: it serves a fixed pending queue and records
// what ApplyProposal was handed, so tests can assert the assembled op list.
type fakeRepo struct {
	pending    []Proposal
	listErr    error
	applyErr   error
	appliedID  ID
	appliedOps []Op
	applyCalls int
	saveErr    error
	saved      *Proposal // the last proposal handed to SaveProposal

	topics     []Block // what GetTopics returns
	topicsErr  error
	subtrees   map[ID][]Block // per-root subtree GetSubtree returns
	subtreeErr error
	gotSubtree ID // the root GetSubtree was asked for

	blocks    map[ID]Block // what GetBlocks serves
	blocksErr error

	edges     []Edge // what GetEdges returns
	edgesErr  error
	gotEdges  ID     // the id GetEdges was asked for
	added     []Edge // edges handed to AddEdge
	addErr    error
	removed   []Edge // edges handed to RemoveEdge
	removeErr error
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

func (f *fakeRepo) GetEdges(_ context.Context, id ID) ([]Edge, error) {
	f.gotEdges = id
	return f.edges, f.edgesErr
}

func (f *fakeRepo) AddEdge(_ context.Context, from, to ID, rel Relation) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, Edge{From: from, To: to, Relation: rel})
	return nil
}

func (f *fakeRepo) RemoveEdge(_ context.Context, from, to ID, rel Relation) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, Edge{From: from, To: to, Relation: rel})
	return nil
}

func (f *fakeRepo) GetTopics(context.Context) ([]Block, error) { return f.topics, f.topicsErr }

func (f *fakeRepo) GetSubtree(_ context.Context, root ID) ([]Block, error) {
	f.gotSubtree = root
	if f.subtreeErr != nil {
		return nil, f.subtreeErr
	}
	return f.subtrees[root], nil
}

func (f *fakeRepo) EditBlockContent(context.Context, ID, string) error { return nil }
func (f *fakeRepo) RejectProposal(context.Context, ID) error           { return nil }
func (f *fakeRepo) Search(context.Context, string) ([]Block, error)    { return nil, nil }

func (f *fakeRepo) SaveProposal(_ context.Context, p Proposal) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = &p
	return nil
}

// fakeLLM is an LLMClient double: it returns a canned tree (or error) and
// records the topic/hint it was asked to expand.
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

func (f *fakeRepo) ListProposals(context.Context, Status) ([]Proposal, error) {
	return f.pending, f.listErr
}

func (f *fakeRepo) ApplyProposal(_ context.Context, id ID, ops []Op) error {
	f.applyCalls++
	f.appliedID, f.appliedOps = id, ops
	return f.applyErr
}

func TestServiceSubmitProposalQueuesPending(t *testing.T) {
	repo := &fakeRepo{}
	at := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	svc := NewService(nil, repo, seqIDs(), nil, clockAt(at))

	ops := []Op{addOp("t1", "", "A")}
	got, err := svc.SubmitProposal(context.Background(), SourceMCP, "My topic", ops)
	if err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	// A fresh pending proposal, stamped with a generated id and the clock.
	if got.Status != StatusPending {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if got.ID != "U1" {
		t.Errorf("id = %q, want a generated id U1", got.ID)
	}
	if got.Source != SourceMCP || got.Title != "My topic" || len(got.Ops) != 1 {
		t.Errorf("proposal fields wrong: %+v", got)
	}
	if !got.CreatedAt.Equal(at) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, at)
	}
	if got.DecidedAt != (time.Time{}) {
		t.Errorf("DecidedAt = %v, want zero for a pending proposal", got.DecidedAt)
	}
	// It was persisted, exactly as returned (not just the id).
	if repo.saved == nil || !reflect.DeepEqual(*repo.saved, got) {
		t.Errorf("persisted %+v, want the returned proposal %+v", repo.saved, got)
	}
}

func TestServiceSubmitProposalPropagatesSaveError(t *testing.T) {
	sentinel := errors.New("disk full")
	repo := &fakeRepo{saveErr: sentinel}
	svc := NewService(nil, repo, seqIDs(), nil, clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	got, err := svc.SubmitProposal(context.Background(), SourceMCP, "t", []Op{addOp("t1", "", "A")})
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want the storage error", err)
	}
	if !reflect.DeepEqual(got, Proposal{}) {
		t.Errorf("got %+v, want a zero proposal when the save fails", got)
	}
}

func TestServiceSubmitProposalRejectsEmptyOps(t *testing.T) {
	// A proposal that changes nothing is not a proposal; it must be refused and
	// never reach the queue.
	repo := &fakeRepo{}
	svc := NewService(nil, repo, seqIDs(), nil, clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	got, err := svc.SubmitProposal(context.Background(), SourceMCP, "empty", nil)
	if !errors.Is(err, ErrEmptyProposal) {
		t.Errorf("error = %v, want ErrEmptyProposal", err)
	}
	if !reflect.DeepEqual(got, Proposal{}) {
		t.Errorf("got %+v, want a zero proposal on error", got)
	}
	if repo.saved != nil {
		t.Errorf("an empty proposal was queued: %+v", repo.saved)
	}
}

func TestServiceExpandTopicQueuesProposal(t *testing.T) {
	llm := &fakeLLM{tree: []Op{addOp("t1", "", "# Go contexts")}}
	repo := &fakeRepo{}
	at := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	svc := NewService(llm, repo, seqIDs(), nil, clockAt(at))

	got, err := svc.ExpandTopic(context.Background(), "Go contexts", "cancellation")
	if err != nil {
		t.Fatalf("ExpandTopic: %v", err)
	}
	// The topic and hint reach the model.
	if llm.gotTopic != "Go contexts" || llm.gotHint != "cancellation" {
		t.Errorf("LLM asked for (%q, %q), want (Go contexts, cancellation)", llm.gotTopic, llm.gotHint)
	}
	// The expansion is queued as a pending gui_expand proposal titled by the topic.
	if got.Status != StatusPending || got.Source != SourceGUIExpand || got.Title != "Go contexts" {
		t.Errorf("proposal = %+v, want pending gui_expand titled 'Go contexts'", got)
	}
	if len(got.Ops) != 1 {
		t.Errorf("ops = %+v, want the tree from the model", got.Ops)
	}
	if repo.saved == nil || !reflect.DeepEqual(*repo.saved, got) {
		t.Errorf("expansion persisted %+v, want the returned proposal %+v", repo.saved, got)
	}
}

func TestServiceExpandTopicSurfacesLLMError(t *testing.T) {
	// When the model is unavailable, the error surfaces as its domain sentinel and
	// nothing is queued.
	llm := &fakeLLM{treeErr: ErrLLMUnavailable}
	repo := &fakeRepo{}
	svc := NewService(llm, repo, seqIDs(), nil, clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	_, err := svc.ExpandTopic(context.Background(), "Go contexts", "")
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Errorf("error = %v, want ErrLLMUnavailable", err)
	}
	if repo.saved != nil {
		t.Errorf("a proposal was queued despite the LLM failure: %+v", repo.saved)
	}
}

func TestServiceExpandTopicRejectsEmptyTree(t *testing.T) {
	// A provider that returns no ops (nil error) must not queue an empty proposal;
	// the empty-proposal invariant surfaces instead.
	llm := &fakeLLM{tree: nil}
	repo := &fakeRepo{}
	svc := NewService(llm, repo, seqIDs(), nil, clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	if _, err := svc.ExpandTopic(context.Background(), "Go contexts", ""); !errors.Is(err, ErrEmptyProposal) {
		t.Errorf("error = %v, want ErrEmptyProposal", err)
	}
	if repo.saved != nil {
		t.Errorf("an empty expansion was queued: %+v", repo.saved)
	}
}

func TestServiceApplyProposalAssemblesSelection(t *testing.T) {
	repo := &fakeRepo{pending: []Proposal{{
		ID:     "P1",
		Status: StatusPending,
		Ops: []Op{
			addOp("t1", "", "# Root"),
			addOp("t2", "t1", "Child"),
			addOp("t3", "t2", "Grandchild"),
		},
	}}}
	svc := NewService(nil, repo, seqIDs(), nil, nil)

	// Accept the root and child but not the grandchild.
	if err := svc.ApplyProposal(context.Background(), "P1", []int{0, 1}); err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if repo.appliedID != "P1" {
		t.Errorf("applied id = %q, want P1", repo.appliedID)
	}
	if len(repo.appliedOps) != 2 {
		t.Fatalf("applied %d ops, want 2 (grandchild deselected): %+v", len(repo.appliedOps), repo.appliedOps)
	}
	// Storage receives resolved ids, never temp ids.
	if got := repo.appliedOps[0]; got.TempID != "" || string(got.BlockID) != "U1" || got.Parent != "" {
		t.Errorf("root op = %+v, want resolved BlockID U1 at the root", got)
	}
	if got := repo.appliedOps[1]; got.Parent != "U1" {
		t.Errorf("child parent = %q, want the resolved root id U1", got.Parent)
	}
}

func TestServiceApplyProposalNotPending(t *testing.T) {
	repo := &fakeRepo{pending: nil} // the id is not in the review queue
	svc := NewService(nil, repo, seqIDs(), nil, nil)

	err := svc.ApplyProposal(context.Background(), "GONE", []int{0})
	if !errors.Is(err, ErrNotPending) {
		t.Errorf("error = %v, want ErrNotPending", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("storage ApplyProposal called %d times, want 0 for a non-pending id", repo.applyCalls)
	}
}

func TestServiceProposeEditQueuesProposal(t *testing.T) {
	scope := []Block{{ID: "01A", Content: "old text"}}
	llm := &fakeLLM{editOps: []Op{{Kind: OpEdit, BlockID: "01A", Content: "revised"}}}
	repo := &fakeRepo{subtrees: map[ID][]Block{"01A": scope}}
	at := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	svc := NewService(llm, repo, seqIDs(), nil, clockAt(at))

	got, err := svc.ProposeEdit(context.Background(), "01A", "revise this block")
	if err != nil {
		t.Fatalf("ProposeEdit: %v", err)
	}
	// The scope is read from storage and handed to the model with the command.
	if repo.gotSubtree != "01A" {
		t.Errorf("read subtree of %q, want 01A", repo.gotSubtree)
	}
	if llm.gotInstruction != "revise this block" || len(llm.gotScope) != 1 || llm.gotScope[0].ID != "01A" {
		t.Errorf("model got scope %+v / instruction %q, want the scope and command", llm.gotScope, llm.gotInstruction)
	}
	// The edit is queued as a pending gui_edit proposal carrying the model's ops.
	if got.Status != StatusPending || got.Source != SourceGUIEdit {
		t.Errorf("proposal = %+v, want pending gui_edit", got)
	}
	if len(got.Ops) != 1 || got.Ops[0].Kind != OpEdit {
		t.Errorf("ops = %+v, want the edit from the model", got.Ops)
	}
	if repo.saved == nil || !reflect.DeepEqual(*repo.saved, got) {
		t.Errorf("persisted %+v, want the returned proposal %+v", repo.saved, got)
	}
}

func TestServiceProposeEditSurfacesLLMError(t *testing.T) {
	llm := &fakeLLM{editErr: ErrLLMUnavailable}
	repo := &fakeRepo{subtrees: map[ID][]Block{"01A": {{ID: "01A", Content: "x"}}}}
	svc := NewService(llm, repo, seqIDs(), nil, clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	if _, err := svc.ProposeEdit(context.Background(), "01A", "do a thing"); !errors.Is(err, ErrLLMUnavailable) {
		t.Errorf("error = %v, want ErrLLMUnavailable", err)
	}
	if repo.saved != nil {
		t.Errorf("a proposal was queued despite the LLM failure: %+v", repo.saved)
	}
}

func TestServiceProposeEditPropagatesReadError(t *testing.T) {
	boom := errors.New("db down")
	llm := &fakeLLM{editOps: []Op{{Kind: OpDelete, BlockID: "01A"}}}
	repo := &fakeRepo{subtreeErr: boom}
	svc := NewService(llm, repo, seqIDs(), nil, clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	_, err := svc.ProposeEdit(context.Background(), "01A", "do a thing")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the storage read error", err)
	}
	if llm.gotInstruction != "" {
		t.Errorf("the model was called despite a read failure")
	}
}

func TestServiceProposeEditRejectsEmptyEdit(t *testing.T) {
	// A command the model turns into no operations must not queue an empty
	// proposal; the empty-proposal invariant surfaces instead.
	llm := &fakeLLM{editOps: nil}
	repo := &fakeRepo{subtrees: map[ID][]Block{"01A": {{ID: "01A", Content: "x"}}}}
	svc := NewService(llm, repo, seqIDs(), nil, clockAt(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)))

	if _, err := svc.ProposeEdit(context.Background(), "01A", "do nothing"); !errors.Is(err, ErrEmptyProposal) {
		t.Errorf("error = %v, want ErrEmptyProposal", err)
	}
	if repo.saved != nil {
		t.Errorf("an empty edit was queued: %+v", repo.saved)
	}
}

func TestServiceExportMarkdownWritesThroughWriter(t *testing.T) {
	// The domain must render and hand the files to the injected writer, never
	// touch the disk itself. GetTopics returns roots plus their first level;
	// only roots become files, each carrying its full subtree — a first-level
	// child must not spawn its own file. The writer records what it received.
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
	svc := NewService(nil, repo, seqIDs(), writer, nil)

	if err := svc.ExportMarkdown(context.Background(), "/export/here"); err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}

	if writer.dir != "/export/here" {
		t.Errorf("writer got dir %q, want the dir passed to ExportMarkdown", writer.dir)
	}
	// Two files, one per root; the child does not spawn its own.
	want := []ExportFile{
		{Name: "alpha.md", Content: "- # Alpha\n  - child of alpha\n"},
		{Name: "beta.md", Content: "- # Beta\n"},
	}
	if !slices.Equal(writer.files, want) {
		t.Errorf("writer got files %+v, want %+v", writer.files, want)
	}
}

func TestServiceExportMarkdownIncludesOutgoingLinks(t *testing.T) {
	// A block's outgoing typed links must reach the export, hydrated with the
	// target's first line, so nothing in the base is dropped.
	repo := &fakeRepo{
		topics:   []Block{blk("t1", "", 0, "# Root")},
		subtrees: map[ID][]Block{"t1": {blk("t1", "", 0, "# Root")}},
		edges:    []Edge{{From: "t1", To: "x9", Relation: RelationExample}},
		blocks:   map[ID]Block{"x9": {ID: "x9", Content: "Target block\nsecond line"}},
	}
	writer := &fakeFileWriter{}
	svc := NewService(nil, repo, seqIDs(), writer, nil)

	if err := svc.ExportMarkdown(context.Background(), "/export/here"); err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	want := "- # Root\n" +
		"  - links:\n" +
		"    - example → Target block\n"
	if len(writer.files) != 1 || writer.files[0].Content != want {
		t.Errorf("content = %+v, want %q", writer.files, want)
	}
}

func TestServiceExportMarkdownSurfacesWriteError(t *testing.T) {
	// A write failure must reach the caller, mirroring the read-error path.
	boom := errors.New("disk full")
	repo := &fakeRepo{
		topics:   []Block{blk("t1", "", 0, "# Alpha")},
		subtrees: map[ID][]Block{"t1": {blk("t1", "", 0, "# Alpha")}},
	}
	svc := NewService(nil, repo, seqIDs(), &fakeFileWriter{writeErr: boom}, nil)

	err := svc.ExportMarkdown(context.Background(), "/export/here")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the writer error surfaced", err)
	}
}

func TestServiceExportMarkdownEmptyBase(t *testing.T) {
	repo := &fakeRepo{topics: nil}
	writer := &fakeFileWriter{}
	svc := NewService(nil, repo, seqIDs(), writer, nil)

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
	svc := NewService(nil, repo, seqIDs(), writer, nil)

	err := svc.ExportMarkdown(context.Background(), "/export/here")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the storage read error surfaced", err)
	}
	// A read failure aborts before any write; the writer is never reached.
	if writer.files != nil {
		t.Errorf("writer was handed %+v; a read failure must write nothing", writer.files)
	}
}
