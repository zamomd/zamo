package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

func TestEditBlockContentUpdatesAndReindexes(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	insertBlock(ctx, t, db, "01A", "", 0, "old content")
	repo := NewRepo(db, time.Now)

	if err := repo.EditBlockContent(ctx, "01A", "new content"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	// The stored content is replaced.
	blocks, err := repo.GetSubtree(ctx, "01A")
	if err != nil {
		t.Fatalf("GetSubtree: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Content != "new content" {
		t.Fatalf("content = %+v, want 'new content'", blocks)
	}
	// Full-text search follows the edit — the FTS trigger fires on update.
	hits, err := repo.Search(ctx, "new")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "01A" {
		t.Errorf("search 'new' = %+v, want the edited block", hits)
	}
	if stale, _ := repo.Search(ctx, "old"); len(stale) != 0 {
		t.Errorf("search 'old' still finds the block: %+v", stale)
	}
}

func TestEditBlockContentMissingBlockIsNotFound(t *testing.T) {
	// A no-op update (the block vanished) must surface as not-found, not a
	// silent success, so the GUI can react instead of reporting a false save.
	repo := NewRepo(openTestDB(t), time.Now)
	err := repo.EditBlockContent(t.Context(), "NOPE", "x")
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}
