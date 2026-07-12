package sqlite

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// savePending stores a fresh pending proposal so ApplyProposal has a row to
// mark applied and a source to record as provenance.
func savePending(t *testing.T, repo *Repo, id knowledge.ID, source knowledge.Source) {
	t.Helper()
	p := knowledge.Proposal{
		ID:        id,
		Source:    source,
		Status:    knowledge.StatusPending,
		CreatedAt: time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveProposal(t.Context(), p); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
}

// blockRow reads a block's parent and content; ok is false if it is gone.
func blockRow(t *testing.T, repo *Repo, id string) (parent, content string, ok bool) {
	t.Helper()
	err := repo.db.QueryRowContext(t.Context(),
		`SELECT COALESCE(parent_id, ''), content FROM blocks WHERE id = ?`, id).Scan(&parent, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false
	}
	if err != nil {
		t.Fatalf("read block %s: %v", id, err)
	}
	return parent, content, true
}

func TestApplyProposalEditMoveDelete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	// Base: root with children A and B.
	savePending(t, repo, "01SETUP", knowledge.SourceGUIExpand)
	setup := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "01A", Parent: "01ROOT", Content: "A old"},
		{Kind: knowledge.OpAdd, BlockID: "01B", Parent: "01ROOT", Content: "B"},
	}
	if err := repo.ApplyProposal(ctx, "01SETUP", setup); err != nil {
		t.Fatalf("setup apply: %v", err)
	}

	// Edit A's text and move B under A.
	savePending(t, repo, "01EDIT", knowledge.SourceGUIEdit)
	if err := repo.ApplyProposal(ctx, "01EDIT", []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "01A", Content: "A new"},
		{Kind: knowledge.OpMove, BlockID: "01B", Parent: "01A"},
	}); err != nil {
		t.Fatalf("edit/move apply: %v", err)
	}
	if _, content, ok := blockRow(t, repo, "01A"); !ok || content != "A new" {
		t.Errorf("A after edit = %q (ok=%v), want \"A new\"", content, ok)
	}
	if parent, _, ok := blockRow(t, repo, "01B"); !ok || parent != "01A" {
		t.Errorf("B parent after move = %q (ok=%v), want 01A", parent, ok)
	}
	// B is A's first child, so it appends at position 0 under its new parent.
	var pos int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT position FROM blocks WHERE id = '01B'`).Scan(&pos); err != nil {
		t.Fatalf("read B position: %v", err)
	}
	if pos != 0 {
		t.Errorf("B position after move = %d, want 0", pos)
	}

	// Delete B.
	savePending(t, repo, "01DEL", knowledge.SourceGUIEdit)
	if err := repo.ApplyProposal(ctx, "01DEL", []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "01B"},
	}); err != nil {
		t.Fatalf("delete apply: %v", err)
	}
	if _, _, ok := blockRow(t, repo, "01B"); ok {
		t.Errorf("B still present after delete")
	}
}

func TestApplyProposalDeleteWithChildFails(t *testing.T) {
	// Deleting a block whose child still references it violates the foreign key
	// (no ON DELETE CASCADE): the apply must fail and roll back, leaving the tree
	// and the proposal untouched, so the domain is forced to delete descendants
	// in the same op list.
	repo := newTestRepo(t)
	ctx := t.Context()
	savePending(t, repo, "01SETUP", knowledge.SourceGUIExpand)
	if err := repo.ApplyProposal(ctx, "01SETUP", []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "01KID", Parent: "01ROOT", Content: "Kid"},
	}); err != nil {
		t.Fatalf("setup apply: %v", err)
	}

	savePending(t, repo, "01DEL", knowledge.SourceGUIEdit)
	if err := repo.ApplyProposal(ctx, "01DEL", []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "01ROOT"},
	}); err == nil {
		t.Fatal("deleting a block with a surviving child must fail, got nil")
	}

	// Rolled back: the parent is still there and the proposal is still pending.
	if _, _, ok := blockRow(t, repo, "01ROOT"); !ok {
		t.Error("root deleted despite the foreign-key violation")
	}
	pending, err := repo.ListProposals(ctx, knowledge.StatusPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "01DEL" {
		t.Errorf("delete proposal not left pending after FK failure: %+v", pending)
	}
}

func TestApplyProposalStaleTarget(t *testing.T) {
	// edit/move/delete each target a block that no longer exists (a concurrent
	// apply removed it). The proposal is stale and must fail with ErrStale, not
	// silently no-op, and must not mark itself applied.
	stale := []struct {
		name string
		op   knowledge.Op
	}{
		{"edit", knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01GONE", Content: "x"}},
		{"move", knowledge.Op{Kind: knowledge.OpMove, BlockID: "01GONE", Parent: "01ROOT"}},
		{"delete", knowledge.Op{Kind: knowledge.OpDelete, BlockID: "01GONE"}},
	}
	for _, tc := range stale {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			ctx := t.Context()
			savePending(t, repo, "01P", knowledge.SourceGUIEdit)

			err := repo.ApplyProposal(ctx, "01P", []knowledge.Op{tc.op})
			if !errors.Is(err, knowledge.ErrStale) {
				t.Fatalf("apply on vanished block: got %v, want ErrStale", err)
			}
			// The failed apply must roll back: the proposal stays pending.
			pending, err := repo.ListProposals(ctx, knowledge.StatusPending)
			if err != nil {
				t.Fatalf("list pending: %v", err)
			}
			if len(pending) != 1 {
				t.Errorf("proposal not left pending after stale apply: %d pending", len(pending))
			}
		})
	}
}

func TestApplyProposalAtomicRollback(t *testing.T) {
	// A proposal whose first op succeeds but whose second op fails must leave no
	// trace: the whole transaction rolls back, so the added block is absent, FTS
	// never sees it, and the proposal stays pending for the human to retry.
	repo := newTestRepo(t)
	ctx := t.Context()
	savePending(t, repo, "01P", knowledge.SourceGUIExpand)

	err := repo.ApplyProposal(ctx, "01P", []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01NEW", Content: "half-applied"},
		{Kind: knowledge.OpEdit, BlockID: "01GONE", Content: "never lands"},
	})
	if !errors.Is(err, knowledge.ErrStale) {
		t.Fatalf("apply with a stale op: got %v, want ErrStale", err)
	}

	if _, _, ok := blockRow(t, repo, "01NEW"); ok {
		t.Error("the first op's block survived a rolled-back apply")
	}
	var n int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT count(*) FROM blocks_fts WHERE blocks_fts MATCH 'applied'`).Scan(&n); err != nil {
		t.Fatalf("fts search: %v", err)
	}
	if n != 0 {
		t.Errorf("FTS indexed a block from a rolled-back apply: %d hits", n)
	}
	pending, err := repo.ListProposals(ctx, knowledge.StatusPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("proposal not left pending after failed apply: %d pending", len(pending))
	}
}

func TestApplyProposalStaleParent(t *testing.T) {
	// add/move that name a parent which vanished between the proposal and its apply
	// must fail with ErrStale — the same "base moved" signal a vanished target
	// gives — not a raw foreign-key error the GUI cannot branch on.
	assertPending := func(t *testing.T, repo *Repo) {
		t.Helper()
		pending, err := repo.ListProposals(t.Context(), knowledge.StatusPending)
		if err != nil {
			t.Fatalf("list pending: %v", err)
		}
		if len(pending) != 1 || pending[0].ID != "01P" {
			t.Errorf("proposal not left pending after stale apply: %+v", pending)
		}
	}

	t.Run("add", func(t *testing.T) {
		repo := newTestRepo(t)
		ctx := t.Context()
		savePending(t, repo, "01P", knowledge.SourceGUIExpand)

		err := repo.ApplyProposal(ctx, "01P", []knowledge.Op{
			{Kind: knowledge.OpAdd, BlockID: "01NEW", Parent: "01GONE", Content: "orphan"},
		})
		if !errors.Is(err, knowledge.ErrStale) {
			t.Fatalf("add under a vanished parent: got %v, want ErrStale", err)
		}
		if _, _, ok := blockRow(t, repo, "01NEW"); ok {
			t.Error("block added despite a vanished parent")
		}
		assertPending(t, repo)
	})

	t.Run("move", func(t *testing.T) {
		repo := newTestRepo(t)
		ctx := t.Context()
		// A real block to move, so the failure is about the parent, not the target.
		savePending(t, repo, "01SETUP", knowledge.SourceGUIExpand)
		if err := repo.ApplyProposal(ctx, "01SETUP", []knowledge.Op{
			{Kind: knowledge.OpAdd, BlockID: "01BLK", Content: "block"},
		}); err != nil {
			t.Fatalf("setup: %v", err)
		}

		savePending(t, repo, "01P", knowledge.SourceGUIEdit)
		err := repo.ApplyProposal(ctx, "01P", []knowledge.Op{
			{Kind: knowledge.OpMove, BlockID: "01BLK", Parent: "01GONE"},
		})
		if !errors.Is(err, knowledge.ErrStale) {
			t.Fatalf("move under a vanished parent: got %v, want ErrStale", err)
		}
		if parent, _, _ := blockRow(t, repo, "01BLK"); parent != "" {
			t.Errorf("block reparented despite a vanished parent: parent=%q", parent)
		}
		assertPending(t, repo)
	})
}

func TestApplyProposalAddsBlocks(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	savePending(t, repo, "01PROP", knowledge.SourceGUIExpand)

	// Resolved ops: BlockID holds the final ULID, Parent the final parent id,
	// ordered parents-before-children (the domain does this in #7).
	ops := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "# Contexts in Go", Origin: "anthropic"},
		{Kind: knowledge.OpAdd, BlockID: "01CHILD", Parent: "01ROOT", Content: "Why context.Context"},
		{Kind: knowledge.OpAdd, BlockID: "01SIB", Parent: "01ROOT", Content: "Cancellation"},
	}
	if err := repo.ApplyProposal(ctx, "01PROP", ops); err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}

	// Both blocks exist with the right hierarchy and positions.
	type row struct {
		parent  string
		pos     int
		content string
		meta    string
	}
	rows := map[string]row{}
	q, err := repo.db.QueryContext(ctx,
		`SELECT id, COALESCE(parent_id, ''), position, content, metadata FROM blocks`)
	if err != nil {
		t.Fatalf("query blocks: %v", err)
	}
	defer q.Close()
	for q.Next() {
		var id string
		var r row
		if err := q.Scan(&id, &r.parent, &r.pos, &r.content, &r.meta); err != nil {
			t.Fatalf("scan block: %v", err)
		}
		rows[id] = r
	}
	if err := q.Err(); err != nil {
		t.Fatalf("iterate blocks: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("blocks table has %d rows, want 3", len(rows))
	}
	if rows["01ROOT"].parent != "" || rows["01ROOT"].content != "# Contexts in Go" {
		t.Errorf("root block wrong: %+v", rows["01ROOT"])
	}
	if rows["01CHILD"].parent != "01ROOT" {
		t.Errorf("child parent = %q, want 01ROOT", rows["01CHILD"].parent)
	}
	// Positions: the root is first among roots (0); the two children append in op
	// order (0, then 1) — this is the COALESCE(MAX(position)+1, 0) logic that
	// ORDER BY position later depends on.
	if got := rows["01ROOT"].pos; got != 0 {
		t.Errorf("root position = %d, want 0", got)
	}
	if got := rows["01CHILD"].pos; got != 0 {
		t.Errorf("first child position = %d, want 0", got)
	}
	if got := rows["01SIB"].pos; got != 1 {
		t.Errorf("second child position = %d, want 1", got)
	}
	// Provenance: AI authorship, the proposal source, and the op's origin are all
	// recorded in metadata; a block with no origin carries no origin key.
	if m := rows["01ROOT"].meta; !strings.Contains(m, `"ai"`) ||
		!strings.Contains(m, "gui_expand") || !strings.Contains(m, "anthropic") {
		t.Errorf("root metadata missing provenance: %s", m)
	}
	if m := rows["01CHILD"].meta; strings.Contains(m, "origin") {
		t.Errorf("child block has no origin, but metadata records one: %s", m)
	}

	// The proposal is now applied, with decided_at stamped.
	var status, decided string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT status, COALESCE(decided_at, '') FROM proposals WHERE id = '01PROP'`).
		Scan(&status, &decided); err != nil {
		t.Fatalf("read proposal: %v", err)
	}
	if status != string(knowledge.StatusApplied) {
		t.Errorf("status = %q, want applied", status)
	}
	if decided == "" {
		t.Errorf("decided_at not stamped")
	}

	// FTS is in sync: the new block is searchable immediately.
	var n int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT count(*) FROM blocks_fts WHERE blocks_fts MATCH 'context'`).Scan(&n); err != nil {
		t.Fatalf("fts search: %v", err)
	}
	if n == 0 {
		t.Errorf("full-text search does not find the new blocks")
	}
}
