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

// FileWriter persists a rendered export. Writing to disk is an effect outside
// the database, so the domain does not do it: the implementation lives outside
// this package and is injected like IDGenerator, keeping knowledge on the
// standard library alone and the export use case testable without a disk.
type FileWriter interface {
	// WriteFiles writes each file into dir. Names are unique within files
	// (RenderMarkdown guarantees it), so order does not matter.
	WriteFiles(ctx context.Context, dir string, files []ExportFile) error
}

// Service holds the product's use cases. It is the only place where the
// logic lives; every input calls its methods.
type Service struct {
	llm    LLMClient
	repo   Repository
	newID  IDGenerator
	writer FileWriter
}

// NewService wires the domain to its boundaries: the LLM provider, storage,
// and the injected ID and file-writer sources. Nothing in the domain stamps
// time — storage does — so no time source is injected here.
func NewService(llm LLMClient, repo Repository, newID IDGenerator, writer FileWriter) *Service {
	return &Service{llm: llm, repo: repo, newID: newID, writer: writer}
}

// ApplyOps validates and applies a write batch directly. Every touched
// block gains a revision; AI revisions await review, owner revisions are
// born reviewed. The single write path for MCP, the GUI AI flows, and the
// owner's manual edits.
//
// A batch that renames a topic root additionally rewrites every [[Topic]]
// reference to it, so the text stays true. The expansion lands in the same
// batch and therefore the same revision group, so the rename is reviewed and
// reverted as a unit — and because it happens here rather than on one caller's
// path, an AI rename is expanded exactly like the owner's and inherits the
// batch's review state.
func (s *Service) ApplyOps(ctx context.Context, meta WriteMeta, ops []Op) (Applied, error) {
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
	if err := s.repo.ApplyOps(ctx, groupID, meta, resolved); err != nil {
		return Applied{}, err
	}
	return Applied{GroupID: groupID, IDs: ids}, nil
}

// CreateTopic adds an empty topic root named name. Owner-authored, so it is
// born reviewed, and it needs no LLM — the provider has no default, so an AI
// expansion is not a path a fresh install can rely on to make a topic exist.
// The name is written verbatim, which is what makes the new root's title_key
// exactly the key the [[Topic]] reference that asked for it resolves by.
//
// A name nothing could reference — empty, or carrying a bracket — is refused
// rather than creating a root the reference cannot reach after all, and a name
// already taken reports ErrDuplicateTopic.
func (s *Service) CreateTopic(ctx context.Context, name string) (ID, error) {
	if TopicKey(name) == "" {
		return "", fmt.Errorf("create topic %q: a topic needs a name: %w", name, ErrInvalidOps)
	}
	const temp = "topic"
	applied, err := s.ApplyOps(ctx, ownerMeta(), []Op{{Kind: OpAdd, TempID: temp, Content: name}})
	if err != nil {
		return "", err
	}
	return applied.IDs[temp], nil
}

// ExpandTopic expands a topic into a block tree and writes it directly,
// awaiting review.
func (s *Service) ExpandTopic(ctx context.Context, topic, hint string) (Applied, error) {
	ops, err := s.llm.GenerateTree(ctx, topic, hint)
	if err != nil {
		return Applied{}, err
	}
	return s.ApplyOps(ctx, WriteMeta{Author: AuthorAI, Source: SourceGUIExpand, Title: topic}, ops)
}

// EditWithAI turns a user's text command into structure edits and applies
// them directly, awaiting review.
func (s *Service) EditWithAI(ctx context.Context, scope ID, instruction string) (Applied, error) {
	blocks, err := s.repo.GetSubtree(ctx, scope)
	if err != nil {
		return Applied{}, err
	}
	ops, err := s.llm.GenerateEdit(ctx, blocks, instruction)
	if err != nil {
		return Applied{}, err
	}
	return s.ApplyOps(ctx, WriteMeta{Author: AuthorAI, Source: SourceGUIEdit, Title: instruction}, ops)
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
}

// expandRenames appends to ops the reference rewrites its topic renames imply,
// and names the batch after them when the caller had nothing more specific to
// say. An edit that drops a still-referenced root's name has no rewrite to make
// and is refused instead (see refuseOrphaning).
//
// The batch is authoritative over every block it already names: such a block
// keeps the content its own op writes and gets no rewrite. Otherwise the
// author's text would be silently replaced by a rewrite of the content it was
// about to supersede — which is also why the renamed root's own edit survives
// a self-reference — and two ops could target one block in a single batch.
// Rewrites this call appends are not the caller's, so a second rename in the
// same batch chains onto them rather than emitting a duplicate.
func (s *Service) expandRenames(ctx context.Context, meta *WriteMeta, ops []Op) ([]Op, error) {
	renames, err := s.renamesIn(ctx, ops)
	if err != nil || len(renames) == 0 {
		return ops, err
	}
	named := make(map[ID]bool, len(ops))
	for _, op := range ops {
		if op.BlockID != "" {
			named[op.BlockID] = true
		}
	}
	out := slices.Clone(ops)
	at := make(map[ID]int) // block id → index of the rewrite already appended for it
	for _, r := range renames {
		sources, err := s.repo.BlocksReferencingTopic(ctx, r.oldKey)
		if err != nil {
			return nil, fmt.Errorf("rename topic %s: %w", r.id, err)
		}
		if r.newKey == "" {
			if err := refuseOrphaning(r, sources, named); err != nil {
				return nil, err
			}
			continue
		}
		for _, src := range sources {
			if named[src.ID] {
				continue
			}
			i, pending := at[src.ID]
			content := src.Content
			if pending {
				content = out[i].Content
			}
			rewritten := RewriteTopicRef(content, r.oldKey, r.newName)
			switch {
			case rewritten == content:
			case pending:
				out[i].Content = rewritten
			default:
				at[src.ID] = len(out)
				out = append(out, Op{Kind: OpEdit, BlockID: src.ID, Content: rewritten})
			}
		}
	}
	if len(at) > 0 {
		// The rewrites are only true alongside the rename that caused them, so
		// the batch they share becomes one reviewable unit.
		meta.Atomic = true
	}
	retitleRename(meta, len(at))
	return out, nil
}

// refuseOrphaning rejects an edit that would leave a topic root unnamed while
// blocks still reference the name it is dropping. There is nothing to rewrite
// those references to — [[]] resolves to nothing — so the only honest answers
// are refusing the edit or orphaning every backlink silently.
//
// A source the batch already names is not counted: the batch is authoritative
// over the blocks it writes (see expandRenames), and that is also how the
// root's own self-reference is excluded. An unreferenced root may still become
// unnamed, which the design allows.
func refuseOrphaning(r rename, sources []Block, named map[ID]bool) error {
	for _, src := range sources {
		if named[src.ID] {
			continue
		}
		return fmt.Errorf("unname topic %s still referenced by %s: %w",
			r.id, src.ID, ErrUnreferenceableTopic)
	}
	return nil
}

// renamesIn reports the name changes a batch makes, in batch order: an edit of
// a topic root whose key (TopicKey) changes to a different one, empty included.
// Only a root has a name to rename, and a root that had none has nothing for a
// reference to have resolved by, so it is not a rename.
//
// A name a reference cannot carry — one holding a bracket — keys as empty, so
// taking a topic to such a name is the same transition as unnaming it: there is
// nothing to rewrite the backlinks to, and refuseOrphaning decides.
func (s *Service) renamesIn(ctx context.Context, ops []Op) ([]rename, error) {
	var edited []ID
	for _, op := range ops {
		if op.Kind == OpEdit && op.BlockID != "" {
			edited = append(edited, op.BlockID)
		}
	}
	if len(edited) == 0 {
		return nil, nil
	}
	blocks, err := s.repo.GetBlocks(ctx, edited)
	if err != nil {
		return nil, err
	}
	var out []rename
	for _, op := range ops {
		if op.Kind != OpEdit {
			continue
		}
		block, ok := blocks[op.BlockID]
		if !ok || !block.IsTopic() {
			continue
		}
		oldKey, newKey := TopicKey(block.Content), TopicKey(op.Content)
		if oldKey == "" || oldKey == newKey {
			continue
		}
		out = append(out, rename{
			id: op.BlockID, oldKey: oldKey, newKey: newKey, newName: TopicName(op.Content),
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

// Search runs a full-text search over block content.
func (s *Service) Search(ctx context.Context, query string) ([]Block, error) {
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

// ExportMarkdown exports the base: one markdown file per topic, block nesting
// rendered as nested lists (RenderMarkdown). It reads every topic's full
// subtree, renders the set, then hands the files to the injected writer. All
// reads complete before any write, so a read failure aborts with nothing
// written; how a mid-write failure is handled is the writer's concern.
func (s *Service) ExportMarkdown(ctx context.Context, dir string) error {
	topics, err := s.repo.GetTopics(ctx)
	if err != nil {
		return err
	}

	var subtrees [][]Block
	for _, t := range topics {
		if !t.IsTopic() { // GetTopics also returns first-level children; skip them
			continue
		}
		sub, err := s.repo.GetSubtree(ctx, t.ID)
		if err != nil {
			return err
		}
		subtrees = append(subtrees, sub)
	}

	return s.writer.WriteFiles(ctx, dir, RenderMarkdown(subtrees))
}
