package knowledge

import (
	"context"
	"fmt"
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
func (s *Service) ApplyOps(ctx context.Context, meta WriteMeta, ops []Op) (Applied, error) {
	if strings.TrimSpace(meta.Title) == "" {
		return Applied{}, fmt.Errorf("empty title: %w", ErrInvalidOps)
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
// path; an owner revision is born reviewed. Renaming a topic root — an edit
// that changes its normalized name — additionally rewrites every [[Topic]]
// reference to it, so the text stays true. All of it lands in one batch, and
// therefore one revision group, so the rename is reviewed and reverted as a
// unit.
func (s *Service) EditBlockContent(ctx context.Context, id ID, content string) error {
	block, err := s.blockByID(ctx, id)
	if err != nil {
		return err
	}
	ops, err := s.editOps(ctx, block, content)
	if err != nil {
		return err
	}
	meta := ownerMeta()
	// The title is what the human reads in the Changes screen when deciding
	// on the group, so it names what the batch actually touched.
	if blocks := len(ops) - 1; blocks > 0 {
		noun := "blocks"
		if blocks == 1 {
			noun = "block"
		}
		meta.Title = fmt.Sprintf("Rename topic and rewrite references in %d %s", blocks, noun)
	}
	_, err = s.ApplyOps(ctx, meta, ops)
	return err
}

// editOps turns one manual edit into the batch it implies: the edit itself,
// followed by a rewrite of every block referencing the topic by its old name
// when the edit renames a topic root.
func (s *Service) editOps(ctx context.Context, block Block, content string) ([]Op, error) {
	ops := []Op{{Kind: OpEdit, BlockID: block.ID, Content: content}}
	oldKey, newKey := NormalizeTitle(block.Content), NormalizeTitle(content)
	// Only a root has a name to rename. An edit that leaves the root unnamed
	// is not a rename either: rewriting references to it would turn them into
	// [[]], so they stay as the author wrote them.
	if !block.IsTopic() || oldKey == "" || newKey == "" || oldKey == newKey {
		return ops, nil
	}
	sources, err := s.repo.BlocksReferencingTopic(ctx, oldKey)
	if err != nil {
		return nil, fmt.Errorf("rename topic %s: %w", block.ID, err)
	}
	newName := TopicName(content)
	for _, src := range sources {
		// A root that references itself is already in the batch as the edit
		// that triggered the rename; a second edit of it would write the old
		// content back over the new name.
		if src.ID == block.ID {
			continue
		}
		if rewritten := RewriteTopicRef(src.Content, oldKey, newName); rewritten != src.Content {
			ops = append(ops, Op{Kind: OpEdit, BlockID: src.ID, Content: rewritten})
		}
	}
	return ops, nil
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
