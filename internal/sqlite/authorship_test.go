package sqlite

import (
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// authorOf reads the authorship stamp the tree renders as the "you"/"AI" badge.
func authorOf(t *testing.T, repo *Repo, id knowledge.ID) any {
	t.Helper()
	blocks, err := repo.GetSubtree(t.Context(), id)
	if err != nil {
		t.Fatalf("GetSubtree %s: %v", id, err)
	}
	if len(blocks) == 0 {
		t.Fatalf("block %s is gone", id)
	}
	return blocks[0].Meta[knowledge.MetaKeyAuthor]
}

// The badge answers one question: is this text mine or the AI's? Whoever wrote
// the content last owns it, so every writer of content must restamp the author.
func TestAuthorshipStampTracksTheLastWriter(t *testing.T) {
	repo := NewRepo(openTestDB(t), time.Now)
	ctx := t.Context()

	// The AI adds the block.
	savePending(t, repo, "01ADD", knowledge.SourceMCP)
	if err := repo.ApplyProposal(ctx, "01ADD", []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "t1", Content: "ai text"},
	}); err != nil {
		t.Fatalf("ApplyProposal add: %v", err)
	}
	blocks, err := repo.GetTopics(ctx)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("GetTopics = %+v, %v", blocks, err)
	}
	id := blocks[0].ID
	if got := authorOf(t, repo, id); got != string(knowledge.AuthorAI) {
		t.Errorf("after AI add: author = %v, want %q", got, knowledge.AuthorAI)
	}

	// The owner rewrites it by hand and takes authorship.
	if err := repo.EditBlockContent(ctx, id, "human text"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if got := authorOf(t, repo, id); got != string(knowledge.AuthorHuman) {
		t.Errorf("after human edit: author = %v, want %q", got, knowledge.AuthorHuman)
	}

	// An accepted AI proposal rewrites that same block: the content is the AI's
	// again, so the badge must stop claiming the owner wrote it.
	savePending(t, repo, "01EDIT", knowledge.SourceMCP)
	if err := repo.ApplyProposal(ctx, "01EDIT", []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: id, Content: "ai rewrote this"},
	}); err != nil {
		t.Fatalf("ApplyProposal edit: %v", err)
	}
	if got := authorOf(t, repo, id); got != string(knowledge.AuthorAI) {
		t.Errorf("after AI edit: author = %v, want %q", got, knowledge.AuthorAI)
	}
}

// The stamp must not cost the block its provenance.
func TestAuthorshipStampKeepsSiblingMetadata(t *testing.T) {
	repo := NewRepo(openTestDB(t), time.Now)
	ctx := t.Context()

	savePending(t, repo, "01ADD", knowledge.SourceMCP)
	if err := repo.ApplyProposal(ctx, "01ADD", []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "t1", Content: "ai text", Origin: "o1"},
	}); err != nil {
		t.Fatalf("ApplyProposal add: %v", err)
	}
	topics, err := repo.GetTopics(ctx)
	if err != nil || len(topics) != 1 {
		t.Fatalf("GetTopics = %+v, %v", topics, err)
	}
	id := topics[0].ID

	if err := repo.EditBlockContent(ctx, id, "human text"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	blocks, err := repo.GetSubtree(ctx, id)
	if err != nil {
		t.Fatalf("GetSubtree: %v", err)
	}
	meta := blocks[0].Meta
	if meta["source"] != string(knowledge.SourceMCP) {
		t.Errorf("source = %v, want %q — the edit dropped provenance", meta["source"], knowledge.SourceMCP)
	}
	if meta["origin"] != "o1" {
		t.Errorf("origin = %v, want %q — the edit dropped provenance", meta["origin"], "o1")
	}
}
