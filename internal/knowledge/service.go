package knowledge

import (
	"context"
	"fmt"
	"maps"
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
// path; an owner revision is born reviewed.
func (s *Service) EditBlockContent(ctx context.Context, id ID, content string) error {
	_, err := s.ApplyOps(ctx,
		WriteMeta{Author: AuthorOwner, Source: SourceGUIManual, Title: "manual edit"},
		[]Op{{Kind: OpEdit, BlockID: id, Content: content}})
	return err
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

// AcceptEdge marks a pending edge as reviewed.
func (s *Service) AcceptEdge(ctx context.Context, from, to ID, rel Relation) error {
	return s.repo.MarkEdgeReviewed(ctx, from, to, rel)
}

// RejectEdge removes a pending edge.
func (s *Service) RejectEdge(ctx context.Context, from, to ID, rel Relation) error {
	return s.repo.RejectEdge(ctx, from, to, rel)
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

// GetSubtree returns a block together with all of its descendants.
func (s *Service) GetSubtree(ctx context.Context, root ID) ([]Block, error) {
	return s.repo.GetSubtree(ctx, root)
}

// LinkedBlocks returns the block's typed links hydrated with the neighbour
// blocks. An edge whose neighbour vanished mid-read is dropped rather than
// reported: the next read no longer sees the edge either.
func (s *Service) LinkedBlocks(ctx context.Context, id ID) ([]LinkedBlock, error) {
	edges, err := s.repo.GetEdges(ctx, id)
	if err != nil {
		return nil, err
	}
	ids := make([]ID, len(edges))
	for i, e := range edges {
		ids[i] = e.To
		if e.From != id {
			ids[i] = e.From
		}
	}
	blocks, err := s.repo.GetBlocks(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]LinkedBlock, 0, len(edges))
	for i, e := range edges {
		b, ok := blocks[ids[i]]
		if !ok {
			continue
		}
		out = append(out, LinkedBlock{Relation: e.Relation, Outgoing: e.From == id, Block: b})
	}
	return out, nil
}

// CreateLink records a typed link directly, without going through review:
// review guards the base against AI, not against its owner (like
// EditBlockContent). An empty relation defaults to related, mirroring the
// link op.
func (s *Service) CreateLink(ctx context.Context, from, to ID, rel Relation) error {
	if from == to {
		return fmt.Errorf("link %s to itself: %w", from, ErrInvalidLink)
	}
	if rel == "" {
		rel = RelationRelated
	}
	if rel != RelationExample && rel != RelationRelated {
		return fmt.Errorf("link %s->%s relation %q: %w", from, to, rel, ErrInvalidLink)
	}
	return s.repo.AddEdge(ctx, from, to, rel, WriteMeta{Author: AuthorOwner, Source: SourceGUIManual})
}

// RemoveLink deletes a typed link directly — the owner's counterpart of
// CreateLink. Removing an absent link is a no-op.
func (s *Service) RemoveLink(ctx context.Context, from, to ID, rel Relation) error {
	return s.repo.RemoveEdge(ctx, from, to, rel)
}

// ExportMarkdown exports the base: one markdown file per topic, block nesting
// rendered as nested lists (RenderMarkdown). It reads every topic's full
// subtree, renders the set, then hands the files to the injected writer. All
// reads complete before any write, so a read failure aborts with nothing
// written; how a mid-write failure is handled is the writer's concern.
//
// ponytail: one GetSubtree query per topic (N+1). Fine for a personal base;
// upgrade to a single bulk "all subtrees" read if export ever gets slow.
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

	links, err := s.exportLinks(ctx, subtrees)
	if err != nil {
		return err
	}
	return s.writer.WriteFiles(ctx, dir, RenderMarkdown(subtrees, links))
}

// exportLinks collects each block's outgoing typed links for the export,
// hydrated with the first line of the target block so the connection reads in
// plain markdown. Edges whose target vanished mid-read are dropped, like
// LinkedBlocks. Only outgoing edges are rendered, so a link appears once, under
// its source.
//
// ponytail: one GetEdges per block (N+1). Fine for a personal base; batch into
// a single edges read if export ever gets slow (same corner as the subtree N+1
// above).
func (s *Service) exportLinks(ctx context.Context, subtrees [][]Block) (map[ID][]ExportLink, error) {
	outgoing := make(map[ID][]Edge)
	targets := make(map[ID]bool)
	for _, sub := range subtrees {
		for _, block := range sub {
			edges, err := s.repo.GetEdges(ctx, block.ID)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				if e.From != block.ID {
					continue // render a link once, under its source
				}
				outgoing[block.ID] = append(outgoing[block.ID], e)
				targets[e.To] = true
			}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	blocks, err := s.repo.GetBlocks(ctx, slices.Collect(maps.Keys(targets)))
	if err != nil {
		return nil, err
	}
	links := make(map[ID][]ExportLink, len(outgoing))
	for blockID, edges := range outgoing {
		for _, e := range edges {
			target, ok := blocks[e.To]
			if !ok {
				continue // the neighbour vanished mid-read
			}
			first, _, _ := strings.Cut(target.Content, "\n")
			links[blockID] = append(links[blockID], ExportLink{Relation: e.Relation, Target: first})
		}
	}
	return links, nil
}
