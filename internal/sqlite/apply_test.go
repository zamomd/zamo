package sqlite

import (
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
