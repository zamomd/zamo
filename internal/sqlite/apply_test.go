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

func TestApplyProposalAddsBlocks(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	savePending(t, repo, "01PROP", knowledge.SourceGUIExpand)

	// Resolved ops: BlockID holds the final ULID, Parent the final parent id,
	// ordered parents-before-children (the domain does this in #7).
	ops := []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "# Contexts in Go", Origin: "anthropic"},
		{Kind: knowledge.OpAdd, BlockID: "01CHILD", Parent: "01ROOT", Content: "Why context.Context"},
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
	if len(rows) != 2 {
		t.Fatalf("blocks table has %d rows, want 2", len(rows))
	}
	if rows["01ROOT"].parent != "" || rows["01ROOT"].content != "# Contexts in Go" {
		t.Errorf("root block wrong: %+v", rows["01ROOT"])
	}
	if rows["01CHILD"].parent != "01ROOT" {
		t.Errorf("child parent = %q, want 01ROOT", rows["01CHILD"].parent)
	}
	// Provenance: AI authorship and the proposal source are recorded in metadata.
	if m := rows["01ROOT"].meta; !strings.Contains(m, `"ai"`) || !strings.Contains(m, "gui_expand") {
		t.Errorf("root metadata missing provenance: %s", m)
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
