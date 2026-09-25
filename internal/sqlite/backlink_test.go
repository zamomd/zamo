package sqlite

import (
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func backlinkBase(t *testing.T, repo *Repo) {
	t.Helper()
	require.NoError(t, ownerOps(t, repo, "01G1",
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
	), "setup")
}

func TestBacklinksToTopic(t *testing.T) {
	repo := testRepo(t)
	backlinkBase(t, repo)
	got, err := repo.GetBacklinks(t.Context(), "01RUST")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, knowledge.ID("01NOTE"), got[0].Block.ID)
	assert.Equal(t, knowledge.ID("01ZIG"), got[0].TopicID)
	assert.Equal(t, "Zig", got[0].TopicTitle)
}

func TestBacklinksToBlock(t *testing.T) {
	repo := testRepo(t)
	backlinkBase(t, repo)
	got, err := repo.GetBacklinks(t.Context(), "01CCCCCCCCCCCCCCCCCCCCCCCC")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, knowledge.ID("01CITE"), got[0].Block.ID)
}

func TestBacklinksDedupesSourceReferencingBothForms(t *testing.T) {
	repo := testRepo(t)
	// The topic id must itself be a full 26-char ULID, or ((id|label)) fails
	// validULID and never becomes a block ref in the first place — leaving
	// only the [[Name]] form and silently not exercising the dedup at all.
	const rust = "01RRRRRRRRRRRRRRRRRRRRRRRR"
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: rust, Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "Unlike [[Rust]] and ((" + rust + "|Rust)).",
		},
	), "setup")
	got, err := repo.GetBacklinks(t.Context(), knowledge.ID(rust))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, knowledge.ID("01NOTE"), got[0].Block.ID)
}

func TestBacklinksCarryTheSourceBlocksUnreviewedFlag(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01RUST", Content: "Rust"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
	), "setup")
	require.NoError(t, repo.ApplyOps(t.Context(), "01G2", aiMeta("test"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG", Content: "Unlike [[Rust]]."},
	}), "setup AI write")

	got, err := repo.GetBacklinks(t.Context(), "01RUST")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, knowledge.ID("01NOTE"), got[0].Block.ID)
	assert.True(t, got[0].Block.Unreviewed, "the source block is an unreviewed AI write")
}

func TestBacklinksUnresolvedTopicYieldsNothing(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG", Content: "See [[Nothing here]]."},
	), "setup")
	got, err := repo.GetBacklinks(t.Context(), "01ZIG")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestBacklinksUnknownTargetIsEmpty(t *testing.T) {
	repo := testRepo(t)
	backlinkBase(t, repo)
	got, err := repo.GetBacklinks(t.Context(), "01MISSING")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestBacklinkCarriesTheParagraphMakingTheReference(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
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
	), "setup")

	toBlock, err := repo.GetBacklinks(t.Context(), "01CCCCCCCCCCCCCCCCCCCCCCCC")
	require.NoError(t, err)
	wantBlock := map[knowledge.ID]string{
		"01BARE":  "Compare ((01CCCCCCCCCCCCCCCCCCCCCCCC)).",
		"01LABEL": "Also ((01CCCCCCCCCCCCCCCCCCCCCCCC|the other claim)).",
	}
	require.Len(t, toBlock, len(wantBlock))
	for _, link := range toBlock {
		assert.Equal(t, wantBlock[link.Block.ID], link.Excerpt, "excerpt of %s", link.Block.ID)
	}

	toTopic, err := repo.GetBacklinks(t.Context(), "01RUST")
	require.NoError(t, err)
	require.Len(t, toTopic, 1)
	assert.Equal(t, "Unlike [[rust   ]], allocation is explicit.", toTopic[0].Excerpt)
}
