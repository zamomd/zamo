package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

// renameService wires the domain to the real repository, so a rename runs the
// way the app runs it: the expansion in the domain, one transaction below it.
func renameService(repo *Repo) *knowledge.Service {
	seq := 0
	newID := func() knowledge.ID { seq++; return knowledge.ID(fmt.Sprintf("grp-%03d", seq)) }
	return knowledge.NewService(repo, newID)
}

// headGroup returns the group id of a block's most recent revision.
func headGroup(t *testing.T, repo *Repo, id string) string {
	t.Helper()
	var group string
	require.NoError(t, repo.db.QueryRowContext(t.Context(),
		`SELECT group_id FROM block_revisions WHERE block_id = ? ORDER BY seq DESC LIMIT 1`, id).Scan(&group),
		"read head revision of %s", id)
	return group
}

func renameBase(t *testing.T, repo *Repo) {
	t.Helper()
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01LOCAL", Content: "Local-first"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "We use [[Local-first]] and [[local-first]] daily.",
		},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01OTHER", Parent: "01ZIG",
			Content: "Nothing to see here.",
		},
	), "setup")
}

func TestBlocksReferencingTopic(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)

	got, err := repo.BlocksReferencingTopic(t.Context(), "local-first")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, knowledge.ID("01NOTE"), got[0].ID)
	assert.Equal(t, "We use [[Local-first]] and [[local-first]] daily.", got[0].Content,
		"want the block's full text to rewrite")
	assert.Equal(t, knowledge.ID("01ZIG"), got[0].ParentID, "want the block fully hydrated")

	none, err := repo.BlocksReferencingTopic(t.Context(), "nothing here")
	require.NoError(t, err)
	assert.Empty(t, none, "unknown topic")
}

func TestRenameTopicRewritesReferencesInOneGroup(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	svc := renameService(repo)

	require.NoError(t, svc.EditBlockContent(t.Context(), "01LOCAL", "Offline-first"))

	_, _, content, ok := blockRow(t, repo, "01NOTE")
	assert.True(t, ok)
	assert.Equal(t, "We use [[Offline-first]] and [[Offline-first]] daily.", content, "referencing block")
	assert.Equal(t, "offline-first", titleKeyOf(t, repo, "01LOCAL"))
	assert.Equal(t, headGroup(t, repo, "01NOTE"), headGroup(t, repo, "01LOCAL"),
		"root and rewritten block landed in different groups, want one reviewable group")
	links, err := repo.GetBacklinks(t.Context(), "01LOCAL")
	require.NoError(t, err)
	require.Len(t, links, 1, "want the rewritten reference still resolving to the topic")
	assert.Equal(t, knowledge.ID("01NOTE"), links[0].Block.ID)
}

// concurrentRenameRepo lands one write between the moment a rename reads the
// reference index and the moment it applies — the window the guards exist for.
type concurrentRenameRepo struct {
	*Repo
	t  *testing.T
	op knowledge.Op
}

func (r *concurrentRenameRepo) BlocksReferencingTopic(ctx context.Context, key string) ([]knowledge.Block, error) {
	blocks, err := r.Repo.BlocksReferencingTopic(ctx, key)
	if err != nil {
		return nil, err
	}
	require.NoError(r.t, applyOps(ctx, r.Repo, "concurrent", ownerMeta("concurrent write"), []knowledge.Op{r.op}),
		"concurrent write")
	return blocks, nil
}

func TestRenameRefusesANewConcurrentReference(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	concurrent := &concurrentRenameRepo{Repo: repo, t: t, op: knowledge.Op{
		Kind: knowledge.OpAdd, BlockID: "01NEW", Parent: "01ZIG",
		Content: "A new [[Local-first]] reference.",
	}}
	svc := knowledge.NewService(concurrent, func() knowledge.ID { return "rename" })

	err := svc.EditBlockContent(t.Context(), "01LOCAL", "Offline-first")
	require.ErrorIs(t, err, knowledge.ErrConcurrentWrite)
	_, _, content, _ := blockRow(t, repo, "01LOCAL")
	assert.Equal(t, "Local-first", content, "want the rename rolled back")
	_, _, content, ok := blockRow(t, repo, "01NEW")
	assert.True(t, ok)
	assert.Equal(t, "A new [[Local-first]] reference.", content, "want the concurrent reference preserved")
}

func TestRenameRefusesToOverwriteAConcurrentEdit(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	concurrent := &concurrentRenameRepo{Repo: repo, t: t, op: knowledge.Op{
		Kind: knowledge.OpEdit, BlockID: "01NOTE", Content: "A concurrent [[Local-first]] edit.",
	}}
	svc := knowledge.NewService(concurrent, func() knowledge.ID { return "rename" })

	err := svc.EditBlockContent(t.Context(), "01LOCAL", "Offline-first")
	require.ErrorIs(t, err, knowledge.ErrConcurrentWrite)
	_, _, content, _ := blockRow(t, repo, "01LOCAL")
	assert.Equal(t, "Local-first", content, "want the rename rolled back")
	_, _, content, _ = blockRow(t, repo, "01NOTE")
	assert.Equal(t, "A concurrent [[Local-first]] edit.", content, "want the concurrent edit preserved")
}

// A guarded block that is gone was written over just as surely as one holding
// different text, and the caller's answer to both is the same: prepare the
// rename again. Reporting ErrNotFound instead would send it looking for a block
// it never named.
func TestRenameReportsAConcurrentlyDeletedBlockAsAConcurrentWrite(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	concurrent := &concurrentRenameRepo{Repo: repo, t: t, op: knowledge.Op{
		Kind: knowledge.OpDelete, BlockID: "01LOCAL",
	}}
	svc := knowledge.NewService(concurrent, func() knowledge.ID { return "rename" })

	err := svc.EditBlockContent(t.Context(), "01LOCAL", "Offline-first")
	require.ErrorIs(t, err, knowledge.ErrConcurrentWrite)
	_, _, _, ok := blockRow(t, repo, "01LOCAL")
	assert.False(t, ok, "the topic is back, want the concurrent delete preserved")
}

// An AI rename arrives as a plain edit op through ApplyOps, the path every
// write_blocks call takes. Without expansion there the refs rows keep
// the old key, so the topic loses every backlink the moment it is renamed.
func TestAIRenameKeepsReferencesResolving(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	svc := renameService(repo)

	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "rename the topic"}
	_, err := svc.ApplyOps(t.Context(), meta,
		[]knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "01LOCAL", Content: "Offline-first"}})
	require.NoError(t, err)

	_, _, content, ok := blockRow(t, repo, "01NOTE")
	assert.True(t, ok)
	assert.Equal(t, "We use [[Offline-first]] and [[Offline-first]] daily.", content, "referencing block")
	links, err := repo.GetBacklinks(t.Context(), "01LOCAL")
	require.NoError(t, err)
	require.Len(t, links, 1, "want the reference still resolving after an AI rename")
	assert.Equal(t, knowledge.ID("01NOTE"), links[0].Block.ID)
	assert.Equal(t, headGroup(t, repo, "01NOTE"), headGroup(t, repo, "01LOCAL"),
		"root and rewritten block landed in different groups, want one reviewable group")
	assert.False(t, reviewedHead(t, repo, "01NOTE"),
		"the rewrite is already reviewed, want an AI batch awaiting the owner's decision")
}

// "Rename a topic and move a block referencing it" arrives as one batch from
// write_blocks. The move carries no content, so the stored text keeps the old
// name unless the rename appends the rewrite next to it — and both ops touching
// one block has to survive the apply.
func TestRenameRewritesABlockTheBatchOnlyMoves(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	svc := renameService(repo)

	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "rename and move"}
	_, err := svc.ApplyOps(t.Context(), meta, []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "01LOCAL", Content: "Offline-first"},
		{Kind: knowledge.OpMove, BlockID: "01NOTE", Parent: "01OTHER"},
	})
	require.NoError(t, err)

	parent, _, content, ok := blockRow(t, repo, "01NOTE")
	assert.True(t, ok)
	assert.Equal(t, "We use [[Offline-first]] and [[Offline-first]] daily.", content, "moved block")
	assert.Equal(t, "01OTHER", parent, "want the move applied too")
	links, err := repo.GetBacklinks(t.Context(), "01LOCAL")
	require.NoError(t, err)
	require.Len(t, links, 1, "want the reference still resolving after the batch")
	assert.Equal(t, knowledge.ID("01NOTE"), links[0].Block.ID)
	assert.Equal(t, headGroup(t, repo, "01NOTE"), headGroup(t, repo, "01LOCAL"),
		"rename and rewrite landed in different groups, want one reviewable group")
}

// The point of CreateTopic: the reference the user clicked must resolve
// afterwards, which only holds because the name is written verbatim and
// title_key is derived from exactly that text.
func TestCreateTopicResolvesTheReferenceThatAskedForIt(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	svc := renameService(repo)

	id, err := svc.CreateTopic(t.Context(), "Nothing to see here", "")
	require.NoError(t, err)
	require.Equal(t, "nothing to see here", titleKeyOf(t, repo, id))

	// 01OTHER's text is a plain sentence, so a reference to it resolves only
	// once a topic of that exact name exists.
	require.NoError(t, svc.EditBlockContent(t.Context(), "01OTHER", "See [[Nothing to see here]]."))
	links, err := repo.GetBacklinks(t.Context(), id)
	require.NoError(t, err)
	require.Len(t, links, 1, "want the reference resolving to the created topic")
	assert.Equal(t, knowledge.ID("01OTHER"), links[0].Block.ID)

	_, err = svc.CreateTopic(t.Context(), "nothing to SEE here", "")
	assert.ErrorIs(t, err, knowledge.ErrDuplicateTopic, "second CreateTopic")
}

// aiRename renames 01LOCAL through the AI path and returns the revision group
// carrying the rename and the rewrite it implies.
func aiRename(t *testing.T, repo *Repo) knowledge.ID {
	t.Helper()
	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "rename the topic"}
	applied, err := renameService(repo).ApplyOps(t.Context(), meta,
		[]knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "01LOCAL", Content: "Offline-first"}})
	require.NoError(t, err)
	return applied.GroupID
}

// A rename and the references it rewrote are only true together. Reviewing one
// block of the pair would leave the other saying something the base no longer
// supports — an accepted [[Offline-first]] pointing at a topic still called
// Local-first, or the reverse.
func TestHalfARenameCannotBeReviewed(t *testing.T) {
	cases := []struct {
		name string
		do   func(*Repo) error
	}{
		{"accept the rewritten reference", func(r *Repo) error {
			return r.MarkBlockReviewed(t.Context(), "01NOTE")
		}},
		{"reject the rename itself", func(r *Repo) error {
			return r.RejectBlock(t.Context(), "01LOCAL")
		}},
		{"accept the page the rewrite lives on", func(r *Repo) error {
			return r.MarkPageReviewed(t.Context(), "01ZIG")
		}},
		{"reject the page the rewrite lives on", func(r *Repo) error {
			return r.RejectPage(t.Context(), "01ZIG")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepo(t)
			renameBase(t, repo)
			aiRename(t, repo)

			require.ErrorIs(t, tc.do(repo), knowledge.ErrAtomicGroup)
			assert.False(t, reviewedHead(t, repo, "01LOCAL"), "the refused decision reviewed the root anyway")
			assert.False(t, reviewedHead(t, repo, "01NOTE"), "the refused decision reviewed the reference anyway")
			_, _, content, _ := blockRow(t, repo, "01NOTE")
			assert.Equal(t, "We use [[Offline-first]] and [[Offline-first]] daily.", content,
				"the refusal must write nothing")
		})
	}
}

// headRevisionID returns the id of a block's most recent revision — what the
// history panel's newest row deletes.
func headRevisionID(t *testing.T, repo *Repo, id string) knowledge.ID {
	t.Helper()
	var rev string
	require.NoError(t, repo.db.QueryRowContext(t.Context(),
		`SELECT id FROM block_revisions WHERE block_id = ? ORDER BY seq DESC LIMIT 1`, id).Scan(&rev),
		"read head revision of %s", id)
	return knowledge.ID(rev)
}

// Reverting the newest revision from the history panel deletes it, which is a
// decision on that block alone taken outside the review controls. Left open, it
// undoes the rename and leaves the rewritten reference as the group's only
// pending member, so accepting the batch then succeeds and the base ends with a
// topic under its old name and accepted text pointing at nothing.
func TestRevertingHalfARenameFromHistoryIsRefused(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	aiRename(t, repo)
	revisions := countRows(t, repo, "block_revisions")

	err := repo.DeleteRevision(t.Context(), "01LOCAL", headRevisionID(t, repo, "01LOCAL"))
	require.ErrorIs(t, err, knowledge.ErrAtomicGroup)
	_, _, content, _ := blockRow(t, repo, "01LOCAL")
	assert.Equal(t, "Offline-first", content, "want the refusal to have written nothing")
	assert.Equal(t, revisions, countRows(t, repo, "block_revisions"), "the refusal must delete nothing")
	// The group is still whole, so the review controls still cover it.
	assert.ErrorIs(t, repo.RejectBlock(t.Context(), "01NOTE"), knowledge.ErrAtomicGroup,
		"RejectBlock after the refusal")
}

// The rule is about the review queue, not about history: once the batch has
// been decided on, its revisions are editable like any others.
func TestHistoryEditingSurvivesAReviewedRename(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	group := aiRename(t, repo)
	require.NoError(t, repo.MarkGroupReviewed(t.Context(), group))

	require.NoError(t, repo.DeleteRevision(t.Context(), "01LOCAL", headRevisionID(t, repo, "01LOCAL")))
	_, _, content, _ := blockRow(t, repo, "01LOCAL")
	assert.Equal(t, "Local-first", content, "want the reviewed rename revertible from history")
}

func TestAWholeRenameIsAcceptedAndRejected(t *testing.T) {
	t.Run("accept the batch", func(t *testing.T) {
		repo := testRepo(t)
		renameBase(t, repo)
		group := aiRename(t, repo)

		require.NoError(t, repo.MarkGroupReviewed(t.Context(), group))
		assert.True(t, reviewedHead(t, repo, "01LOCAL"), "the batch left the root unreviewed")
		assert.True(t, reviewedHead(t, repo, "01NOTE"), "the batch left the reference unreviewed")
	})

	t.Run("reject the batch", func(t *testing.T) {
		repo := testRepo(t)
		renameBase(t, repo)
		group := aiRename(t, repo)

		require.NoError(t, repo.UndoGroup(t.Context(), group))
		_, _, content, _ := blockRow(t, repo, "01LOCAL")
		assert.Equal(t, "Local-first", content, "want the rename rolled back")
		_, _, content, _ = blockRow(t, repo, "01NOTE")
		assert.Equal(t, "We use [[Local-first]] and [[local-first]] daily.", content, "referencing block")
	})
}

// An owner edit landing on one member of a rename batch leaves it with no
// reject route at all: a per-block reject would split the group, and the
// batch-wide undo would pull revisions out from under the newer edit. Nothing
// can be rolled back from there, so what the user gets has to name the reason
// and the way out — accepting the batch, which still works — rather than the
// generic "that version can't be rolled back to".
func TestARenameWrittenOverSaysWhyItCannotBeRejected(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	group := aiRename(t, repo)
	require.NoError(t, ownerOps(t, repo, "01G3",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01NOTE", Content: "Rewritten by hand."}), "owner edit")

	require.ErrorIs(t, repo.RejectBlock(t.Context(), "01LOCAL"), knowledge.ErrAtomicGroup, "RejectBlock")
	require.ErrorIs(t, repo.UndoGroup(t.Context(), group), knowledge.ErrGroupWrittenOver, "UndoGroup")
	require.NoError(t, repo.MarkGroupReviewed(t.Context(), group), "accepting the batch is the route the message names")
}

// A page that happens to hold the whole group splits nothing, so it goes
// through: the rule is about leaving half a group behind, not about pages.
func TestAPageHoldingAWholeRenameIsAccepted(t *testing.T) {
	repo := testRepo(t)
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG", Content: "About [[Zig]] itself."},
	), "setup")
	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "rename"}
	_, err := renameService(repo).ApplyOps(t.Context(), meta,
		[]knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "01ZIG", Content: "Ziglang"}})
	require.NoError(t, err)

	require.NoError(t, repo.MarkPageReviewed(t.Context(), "01ZIG"))
	assert.True(t, reviewedHead(t, repo, "01ZIG"), "the page left the root unreviewed")
	assert.True(t, reviewedHead(t, repo, "01NOTE"), "the page left the reference unreviewed")
}

// The rule must not spread to batches that are merely large.
func TestAnOrdinaryBatchIsStillReviewedPerBlock(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	require.NoError(t, applyOps(t.Context(), repo, "01G2", aiMeta("two edits"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "01NOTE", Content: "Rewritten by the AI."},
		{Kind: knowledge.OpEdit, BlockID: "01OTHER", Content: "Also rewritten."},
	}))

	require.NoError(t, repo.MarkBlockReviewed(t.Context(), "01NOTE"))
	assert.True(t, reviewedHead(t, repo, "01NOTE"), "the accepted block")
	assert.False(t, reviewedHead(t, repo, "01OTHER"), "a plain batch should be reviewable one block at a time")
}

// The Changes screen decides which controls to offer from this flag.
func TestUnreviewedReportsARenameGroupAsAtomic(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	aiRename(t, repo)

	changes, err := repo.ListUnreviewed(t.Context())
	require.NoError(t, err)
	require.Len(t, changes.Blocks, 2, "want the root and the rewritten reference")
	for _, b := range changes.Blocks {
		assert.True(t, b.Atomic,
			"block %s is not marked atomic, so the screen would offer per-block buttons", b.Head.BlockID)
	}
}

// reviewedHead reports the review state of a block's most recent revision.
func reviewedHead(t *testing.T, repo *Repo, id string) bool {
	t.Helper()
	var reviewed bool
	require.NoError(t, repo.db.QueryRowContext(t.Context(),
		`SELECT reviewed FROM block_revisions WHERE block_id = ? ORDER BY seq DESC LIMIT 1`, id).Scan(&reviewed),
		"read head revision of %s", id)
	return reviewed
}

func TestRenameOntoATakenNameWritesNothing(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	require.NoError(t, ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01OFFLINE", Content: "Offline-first"},
	), "setup rival topic")
	revisions, groups := countRows(t, repo, "block_revisions"), countRows(t, repo, "revision_groups")
	svc := renameService(repo)

	err := svc.EditBlockContent(t.Context(), "01LOCAL", "Offline-first")
	require.ErrorIs(t, err, knowledge.ErrDuplicateTopic)

	_, _, content, _ := blockRow(t, repo, "01LOCAL")
	assert.Equal(t, "Local-first", content, "want the rejected rename rolled back")
	assert.Equal(t, "local-first", titleKeyOf(t, repo, "01LOCAL"))
	_, _, content, _ = blockRow(t, repo, "01NOTE")
	assert.Equal(t, "We use [[Local-first]] and [[local-first]] daily.", content, "want the rewrite rolled back too")
	assert.Equal(t, revisions, countRows(t, repo, "block_revisions"), "the failed batch left revisions behind")
	assert.Equal(t, groups, countRows(t, repo, "revision_groups"), "the failed batch left its group behind")
	refs, err := repo.BlocksReferencingTopic(t.Context(), "local-first")
	require.NoError(t, err)
	require.Len(t, refs, 1, "want the old reference untouched")
	assert.Equal(t, knowledge.ID("01NOTE"), refs[0].ID)
}
