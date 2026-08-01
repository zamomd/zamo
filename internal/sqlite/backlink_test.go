package sqlite

import (
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func backlinkBase(t *testing.T, repo *Repo) {
	t.Helper()
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01CCCCCCCCCCCCCCCCCCCCCCCC",
			Parent: "01RUST", Content: "Borrow checking is static.",
		},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "Unlike [[Rust]], allocation is explicit.",
		},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01CITE", Parent: "01ZIG",
			Content: "This contradicts ((01CCCCCCCCCCCCCCCCCCCCCCCC|Borrow checking is static)).",
		},
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func TestBacklinksToTopic(t *testing.T) {
	repo := testRepo(t)
	backlinkBase(t, repo)
	got, err := repo.GetBacklinks(t.Context(), "01RUST")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d backlinks, want 1", len(got))
	}
	if got[0].Block.ID != "01NOTE" {
		t.Errorf("source = %s, want 01NOTE", got[0].Block.ID)
	}
	if got[0].TopicID != "01ZIG" {
		t.Errorf("topic = %s, want 01ZIG", got[0].TopicID)
	}
	if got[0].TopicTitle != "Zig" {
		t.Errorf("topic title = %q, want %q", got[0].TopicTitle, "Zig")
	}
}

func TestBacklinksToBlock(t *testing.T) {
	repo := testRepo(t)
	backlinkBase(t, repo)
	got, err := repo.GetBacklinks(t.Context(), "01CCCCCCCCCCCCCCCCCCCCCCCC")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(got) != 1 || got[0].Block.ID != "01CITE" {
		t.Fatalf("got %v, want one backlink from 01CITE", got)
	}
}

func TestBacklinksDedupesSourceReferencingBothForms(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "Unlike [[Rust]] and ((01RUST|Rust)).",
		},
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := repo.GetBacklinks(t.Context(), "01RUST")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(got) != 1 || got[0].Block.ID != "01NOTE" {
		t.Fatalf("got %v, want exactly one backlink from 01NOTE", got)
	}
}

func TestBacklinksCarryTheSourceBlocksUnreviewedFlag(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := repo.ApplyOps(t.Context(), "01G2", aiMeta("test"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG", Content: "Unlike [[Rust]]."},
	}); err != nil {
		t.Fatalf("setup AI write: %v", err)
	}

	got, err := repo.GetBacklinks(t.Context(), "01RUST")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(got) != 1 || got[0].Block.ID != "01NOTE" {
		t.Fatalf("got %v, want one backlink from 01NOTE", got)
	}
	if !got[0].Block.Unreviewed {
		t.Error("Block.Unreviewed = false, want true: the source block is an unreviewed AI write")
	}
}

func TestBacklinksUnresolvedTopicYieldsNothing(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG", Content: "See [[Nothing here]]."},
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := repo.GetBacklinks(t.Context(), "01ZIG")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d backlinks, want 0", len(got))
	}
}

func TestBacklinksUnknownTargetIsEmpty(t *testing.T) {
	repo := testRepo(t)
	backlinkBase(t, repo)
	got, err := repo.GetBacklinks(t.Context(), "01MISSING")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d backlinks, want 0", len(got))
	}
}
