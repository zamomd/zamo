package knowledge

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// IDGenerator produces a new unique ID. The implementation lives outside
// the domain and is injected here so this package stays
// on the standard library alone.
type IDGenerator func() ID

// AIPolicy reports whether AI operations are allowed.
type AIPolicy func() bool

// Option adjusts a Service at construction.
type Option func(*Service)

// WithAIPolicy sets the policy checked by AI-authored writes.
func WithAIPolicy(p AIPolicy) Option {
	return func(s *Service) { s.aiAllowed = p }
}

// Service holds the product's use cases. It is the only place where the
// logic lives; every input calls its methods.
type Service struct {
	repo      Repository
	newID     IDGenerator
	aiAllowed AIPolicy
}

func (s *Service) requireAI() error {
	if s.aiAllowed != nil && !s.aiAllowed() {
		return ErrAIDisabled
	}
	return nil
}

// NewService wires the domain to its boundaries: the LLM provider, storage,
// and the injected ID source. Nothing in the domain stamps
// time — storage does — so no time source is injected here.
// AI operations are allowed when no policy is provided.
func NewService(repo Repository, newID IDGenerator, opts ...Option) *Service {
	s := &Service{repo: repo, newID: newID}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ApplyOps validates and applies a write batch directly. Every touched block
// gains a revision, awaiting review or not as WriteMeta.BornReviewed decides:
// AI revisions always wait, owner revisions do not unless the batch asked to
// (WriteMeta.AwaitsReview). The single write path for MCP, the GUI AI flows,
// and the owner's manual edits.
func (s *Service) ApplyOps(ctx context.Context, meta WriteMeta, ops []Op) (Applied, error) {
	if meta.Author == AuthorAI && s.requireAI() != nil {
		return Applied{}, fmt.Errorf("%w: this batch is AI-authored", ErrAIDisabled)
	}
	if strings.TrimSpace(meta.Title) == "" {
		return Applied{}, fmt.Errorf("empty title: %w", ErrInvalidOps)
	}
	ops, err := s.expandRenames(ctx, &meta, ops)
	if err != nil {
		return Applied{}, err
	}
	resolved, ids, err := prepareOps(ops, s.newID)
	if err != nil {
		return Applied{}, err
	}
	groupID := s.newID()
	changed, err := s.repo.ApplyOps(ctx, groupID, meta, resolved)
	if err != nil {
		return Applied{}, err
	}
	if !changed {
		return Applied{}, nil
	}
	return Applied{GroupID: groupID, IDs: ids}, nil
}

// CreateTopic adds an empty topic root named name, in the given mode; an
// empty mode defaults to ModeOutline. Owner-authored, so it is born reviewed,
// and it needs no LLM — the provider has no default, so an AI expansion is
// not a path a fresh install can rely on to make a topic exist. The name is
// written verbatim, which is what makes the new root's title_key exactly the
// key the [[Topic]] reference that asked for it resolves by.
func (s *Service) CreateTopic(ctx context.Context, name string, mode Mode) (ID, error) {
	if TopicKey(name) == "" {
		return "", fmt.Errorf("create topic %q: a topic needs a name: %w", name, ErrInvalidOps)
	}
	if mode == "" {
		mode = ModeOutline
	}
	if !mode.valid() {
		return "", fmt.Errorf("create topic %q: unknown mode %q: %w", name, mode, ErrInvalidOps)
	}
	const temp = "topic"
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{
		{Kind: OpAdd, TempID: temp, Content: name, Mode: mode},
	})
	if err != nil {
		return "", err
	}
	return applied.IDs[temp], nil
}

// EditBlockContent applies the owner's manual edit through the same write
// path; an owner revision is born reviewed. The block is read first so a
// missing one is refused before any batch is built.
func (s *Service) EditBlockContent(ctx context.Context, id ID, content string) error {
	if _, err := s.blockByID(ctx, id); err != nil {
		return err
	}
	_, err := s.ApplyOps(ctx, ownerMeta(), []Op{{Kind: OpEdit, BlockID: id, Content: content}})
	return err
}

// rename is one topic rename found in a batch: the root being renamed, the
// normalized name references still resolve by, and the name to write instead.
// An empty newKey means the edit drops the root's name altogether.
type rename struct {
	id      ID
	oldKey  string
	newKey  string
	newName string
	content string
}

// expandRenames appends to ops the reference rewrites its topic renames imply,
// and names the batch after them when the caller had nothing more specific to
// say. An edit that takes a still-referenced root to a name nothing can
// reference has no rewrite to make and is refused instead (see refuseOrphaning).
func (s *Service) expandRenames(ctx context.Context, meta *WriteMeta, ops []Op) ([]Op, error) {
	renames, err := s.renamesIn(ctx, ops)
	if err != nil || len(renames) == 0 {
		return ops, err
	}
	e := newExpansion(ops)
	for _, r := range renames {
		e.expect(r.id, r.content)
		sources, err := s.repo.BlocksReferencingTopic(ctx, r.oldKey)
		if err != nil {
			return nil, fmt.Errorf("rename topic %s: %w", r.id, err)
		}
		e.expectRefs(r.id, r.oldKey, sources)
		if r.newKey == "" {
			if err := e.refuseOrphaning(r, sources); err != nil {
				return nil, err
			}
			continue
		}
		for _, src := range sources {
			e.rewrite(src, r)
		}
	}
	// A rewrite is only true alongside the rename that caused it, so a batch
	// carrying one becomes a single reviewable unit — unless the rename's own
	// op is what carries it, in which case there is nothing to decide on apart
	// and the block keeps its ordinary controls.
	meta.Atomic = e.split
	retitleRename(meta, len(e.rewrote))
	return e.ops, nil
}

// expansion is the batch a rename is being expanded into. It answers what text
// the batch will store for a block and where that text lives, which is what
// keeps one block to one written content: a rewrite lands on the op that already
// carries it — the caller's edit, or an earlier rewrite — and a block whose ops
// carry no content at all gets one appended edit and no more.
type expansion struct {
	ops     []Op
	carrier map[ID]int  // block id → index in ops of the op writing its content
	first   map[ID]int  // block id → index in ops of the first op naming it
	gone    map[ID]bool // block id → the batch deletes it
	rewrote map[ID]bool // block id → a rename changed its text
	split   bool        // a rewrite landed on a block other than the renamed root
}

// newExpansion reads the caller's batch without taking it over: ops is cloned,
// so nothing here is written through to the slice the caller still holds.
func newExpansion(ops []Op) *expansion {
	e := &expansion{
		ops:     slices.Clone(ops),
		carrier: make(map[ID]int),
		first:   make(map[ID]int),
		gone:    make(map[ID]bool),
		rewrote: make(map[ID]bool),
	}
	for i, op := range ops {
		if op.BlockID == "" {
			continue
		}
		if _, seen := e.first[op.BlockID]; !seen {
			e.first[op.BlockID] = i
		}
		switch op.Kind {
		case OpDelete:
			e.gone[op.BlockID] = true
		case OpEdit:
			// A later edit of the same block wins, as it does on apply.
			e.carrier[op.BlockID] = i
		}
	}
	return e
}

// content reports the text the batch leaves in src and whether the block
// survives it at all.
func (e *expansion) content(src Block) (string, bool) {
	if e.gone[src.ID] {
		return "", false
	}
	if i, ok := e.carrier[src.ID]; ok {
		return e.ops[i].Content, true
	}
	return src.Content, true
}

// rewrite makes r's new name true in src. It writes through the op that carries
// src's content, appending an edit only when no op does — a moved or untouched
// block — so no block is ever given two ops that write content.
func (e *expansion) rewrite(src Block, r rename) {
	content, live := e.content(src)
	if !live {
		return
	}
	rewritten := RewriteTopicRef(content, r.oldKey, r.newName)
	if rewritten == content {
		return
	}
	e.rewrote[src.ID] = true
	e.split = e.split || src.ID != r.id
	if i, ok := e.carrier[src.ID]; ok {
		e.ops[i].Content = rewritten
	} else {
		e.carrier[src.ID] = len(e.ops)
		e.ops = append(e.ops, Op{Kind: OpEdit, BlockID: src.ID, Content: rewritten})
	}
	e.expect(src.ID, src.Content)
}

// expect records the stored text a rename was worked out from, on the op that
// will write the block — or, for a block the batch only moves, on the move,
// which writes nothing but was still decided against that text. Storage refuses
// the batch if the text is no longer there. The op keeps the first text it is
// given: a batch renaming two topics reads the base once per rename, and it is
// the read the rewrite was computed from that has to still hold.
func (e *expansion) expect(id ID, content string) {
	if i, ok := e.carrier[id]; ok {
		e.ops[i].expectContent(content)
		return
	}
	if i, ok := e.first[id]; ok {
		e.ops[i].expectContent(content)
	}
}

// expectRefs records which blocks referenced the topic when the rename read the
// index, so a source that arrives meanwhile — one no rewrite in this batch keeps
// true — is caught on apply rather than silently orphaned.
func (e *expansion) expectRefs(id ID, key string, sources []Block) {
	i, ok := e.first[id]
	if !ok {
		return
	}
	ids := make([]ID, len(sources))
	for j, source := range sources {
		ids[j] = source.ID
	}
	e.ops[i].expectTopicRefs(key, ids)
}

// refuseOrphaning rejects an edit that would leave a topic root with a name no
// reference can reach while blocks still reference the name it is dropping.
// There is nothing to rewrite those references to — [[]] resolves to nothing —
// so the only honest answers are refusing the edit or orphaning every backlink
// silently.
//
// What counts is the text the batch will store: a source it deletes, or edits
// into something that no longer names the topic, orphans nothing. That is also
// how the root's own self-reference is excluded when the same edit drops it. An
// unreferenced root may still become unnamed, which the design allows.
func (e *expansion) refuseOrphaning(r rename, sources []Block) error {
	for _, src := range sources {
		content, live := e.content(src)
		if !live || !referencesTopic(content, r.oldKey) {
			continue
		}
		return fmt.Errorf("unname topic %s still referenced by %s: %w",
			r.id, src.ID, ErrUnreferenceableTopic)
	}
	return nil
}

// referencesTopic reports whether content carries a reference resolving by key,
// read through the grammar that built the index rather than a second one.
func referencesTopic(content, key string) bool {
	return slices.Contains(ParseRefs(content), Ref{Kind: RefTopic, TargetKey: key})
}

// renamesIn reports the name changes a batch makes: one per edited topic root,
// from the name references resolve by today to the one the batch leaves it with.
// Only a root has a name to rename, and a root that had none has nothing for a
// reference to have resolved by, so it is not a rename.
//
// It is the batch's last edit of a block that decides its content, as it does on
// apply, so a root edited several times yields a single rename over the whole
// batch — never one per op, which would rewrite the backlinks to a name only an
// intermediate op held. A root taken back to the name it started with changes
// nothing and is no rename at all.
func (s *Service) renamesIn(ctx context.Context, ops []Op) ([]rename, error) {
	var changed []ID
	seen := make(map[ID]bool)
	final := make(map[ID]int)     // block id → index in ops of the edit that decides its content
	parent := make(map[ID]string) // block id → parent left by its last move
	for i, op := range ops {
		if op.BlockID == "" {
			continue
		}
		switch op.Kind {
		case OpEdit:
			final[op.BlockID] = i
		case OpMove:
			parent[op.BlockID] = op.Parent
		default:
			continue
		}
		if !seen[op.BlockID] {
			seen[op.BlockID] = true
			changed = append(changed, op.BlockID)
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	blocks, err := s.repo.GetBlocks(ctx, changed)
	if err != nil {
		return nil, err
	}
	var out []rename
	for _, id := range changed {
		block, ok := blocks[id]
		if !ok || !block.IsTopic() {
			continue
		}
		content := block.Content
		if i, edited := final[id]; edited {
			content = ops[i].Content
		}
		oldKey := TopicKey(block.Content)
		newKey := TopicKey(content)
		if p, moved := parent[id]; moved && p != "" {
			newKey = ""
		}
		if oldKey == "" || oldKey == newKey {
			continue
		}
		out = append(out, rename{
			id: id, oldKey: oldKey, newKey: newKey, newName: TopicName(content), content: block.Content,
		})
	}
	return out, nil
}

// retitleRename names a rename batch in the Changes screen by what it actually
// touched. Only the generic owner label is replaced: any other title was chosen
// for this batch — an AI batch carries the instruction the human gave — and
// says more than a count of rewritten blocks.
func retitleRename(meta *WriteMeta, blocks int) {
	if blocks == 0 || meta.Title != ownerTitle {
		return
	}
	noun := "blocks"
	if blocks == 1 {
		noun = "block"
	}
	meta.Title = fmt.Sprintf("Rename topic and rewrite references in %d %s", blocks, noun)
}

// Unreviewed returns everything awaiting review: the Changes screen.
func (s *Service) Unreviewed(ctx context.Context) (UnreviewedChanges, error) {
	return s.repo.ListUnreviewed(ctx)
}

// AcceptBlock marks a block's pending revisions as reviewed.
func (s *Service) AcceptBlock(ctx context.Context, id ID) error {
	return s.repo.MarkBlockReviewed(ctx, id)
}

// AcceptGroup marks every revision of one ApplyOps batch as reviewed.
func (s *Service) AcceptGroup(ctx context.Context, id ID) error {
	return s.repo.MarkGroupReviewed(ctx, id)
}

// AcceptPage marks every pending revision under a topic as reviewed.
func (s *Service) AcceptPage(ctx context.Context, id ID) error {
	return s.repo.MarkPageReviewed(ctx, id)
}

// RejectBlock rolls a block back to its last reviewed state, deleting its
// contiguous unreviewed head revisions.
func (s *Service) RejectBlock(ctx context.Context, id ID) error {
	return s.repo.RejectBlock(ctx, id)
}

// RejectPage rolls every pending block under a topic back to its last
// reviewed state.
func (s *Service) RejectPage(ctx context.Context, id ID) error {
	return s.repo.RejectPage(ctx, id)
}

// BlockHistory returns a block's full revision history.
func (s *Service) BlockHistory(ctx context.Context, id ID) ([]Revision, error) {
	return s.repo.ListRevisions(ctx, id)
}

// DeleteRevision removes one revision from a block's history — the
// rollback primitive review and history editing build on.
func (s *Service) DeleteRevision(ctx context.Context, blockID, revID ID) error {
	return s.repo.DeleteRevision(ctx, blockID, revID)
}

// UndoGroup rolls back an entire revision group.
func (s *Service) UndoGroup(ctx context.Context, groupID ID) error {
	return s.repo.UndoGroup(ctx, groupID)
}

// CountUnreviewedUnder returns how many blocks at or under root await review,
// pending deletions included.
func (s *Service) CountUnreviewedUnder(ctx context.Context, root ID) (int, error) {
	return s.repo.CountUnreviewedUnder(ctx, root)
}

// Search runs a full-text search over block content.
func (s *Service) Search(ctx context.Context, query string) ([]SearchHit, error) {
	return s.repo.Search(ctx, query)
}

// TopicMap returns the map of the base — a compact topic tree for
// orientation.
func (s *Service) TopicMap(ctx context.Context) ([]Block, error) {
	return s.repo.GetTopics(ctx)
}

// TopicGraph returns the topic-level map: every topic with its block count and
// the collapsed edges between topics. A pass-through — the aggregation is a
// query, and doing it in Go would mean reading every block and every edge to
// throw most of both away.
func (s *Service) TopicGraph(ctx context.Context) (TopicGraph, error) {
	return s.repo.GetTopicGraph(ctx)
}

// GetSubtree returns a block together with all of its descendants.
func (s *Service) GetSubtree(ctx context.Context, root ID) ([]Block, error) {
	return s.repo.GetSubtree(ctx, root)
}

// Backlinks returns every reference pointing at the block or topic, with the
// context a reader needs to make sense of each one.
func (s *Service) Backlinks(ctx context.Context, id ID) ([]Backlink, error) {
	links, err := s.repo.GetBacklinks(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("backlinks of %s: %w", id, err)
	}
	return links, nil
}
