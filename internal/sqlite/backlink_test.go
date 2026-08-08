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
	// The topic id must itself be a full 26-char ULID, or ((id|label)) fails
	// validULID and never becomes a block ref in the first place — leaving
	// only the [[Name]] form and silently not exercising the dedup at all.
	const rust = "01RRRRRRRRRRRRRRRRRRRRRRRR"
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: rust, Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "Unlike [[Rust]] and ((" + rust + "|Rust)).",
		},
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := repo.GetBacklinks(t.Context(), knowledge.ID(rust))
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

func TestBacklinkCarriesTheParagraphMakingTheReference(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01CCCCCCCCCCCCCCCCCCCCCCCC",
			Parent: "01RUST", Content: "Borrow checking is static.",
		},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01BARE", Parent: "01ZIG",
			Content: "An opening line.\n\nCompare ((01CCCCCCCCCCCCCCCCCCCCCCCC)).",
		},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01LABEL", Parent: "01ZIG",
			Content: "An opening line.\n\nAlso ((01CCCCCCCCCCCCCCCCCCCCCCCC|the other claim)).",
		},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01LOOSE", Parent: "01ZIG",
			Content: "An opening line.\n\nUnlike [[rust   ]], allocation is explicit.",
		},
	); err != nil {
		t.Fatalf("setup: %v", err)
	}

	toBlock, err := repo.GetBacklinks(t.Context(), "01CCCCCCCCCCCCCCCCCCCCCCCC")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	wantBlock := map[knowledge.ID]string{
		"01BARE":  "Compare ((01CCCCCCCCCCCCCCCCCCCCCCCC)).",
		"01LABEL": "Also ((01CCCCCCCCCCCCCCCCCCCCCCCC|the other claim)).",
	}
	if len(toBlock) != len(wantBlock) {
		t.Fatalf("got %d backlinks to the block, want %d", len(toBlock), len(wantBlock))
	}
	for _, link := range toBlock {
		if got, want := link.Excerpt, wantBlock[link.Block.ID]; got != want {
			t.Errorf("excerpt of %s = %q, want %q", link.Block.ID, got, want)
		}
	}

	toTopic, err := repo.GetBacklinks(t.Context(), "01RUST")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(toTopic) != 1 {
		t.Fatalf("got %d backlinks to the topic, want 1", len(toTopic))
	}
	if got, want := toTopic[0].Excerpt, "Unlike [[rust   ]], allocation is explicit."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}
