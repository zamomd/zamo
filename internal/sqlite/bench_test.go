package sqlite

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
)

// benchDB opens a migrated database seeded with n blocks: one root and n-1
// children under it. Every block carries distinct content so the FTS index has
// real terms to match. It reports allocations for the seeded read that follows.
func benchDB(b *testing.B, n int) (*sql.DB, string) {
	b.Helper()
	db, err := Open(b.Context(), filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { db.Close() })

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC).Format(timeLayout)
	const root = "root"
	insert := func(id, parent string, pos int, content string) {
		var parentArg any
		if parent != "" {
			parentArg = parent
		}
		_, err := db.ExecContext(b.Context(), `
			INSERT INTO blocks (id, parent_id, position, content, metadata, created_at, updated_at)
			VALUES (?, ?, ?, ?, '{}', ?, ?)`,
			id, parentArg, pos, content, now, now)
		if err != nil {
			b.Fatalf("seed %s: %v", id, err)
		}
	}
	insert(root, "", 0, "root topic on distributed systems")
	for i := range n - 1 {
		insert(fmt.Sprintf("b%d", i), root, i, fmt.Sprintf("block %d about concurrency and consensus", i))
	}
	return db, root
}

func BenchmarkSearch(b *testing.B) {
	db, _ := benchDB(b, 500)
	repo := NewRepo(db, time.Now)
	ctx := b.Context()
	// Guard the fixture: Search returns (nil, nil) on zero hits, so without this
	// a broken FTS index would silently benchmark the empty-result path and pass.
	if res, err := repo.Search(ctx, "concurrency consensus"); err != nil || len(res) == 0 {
		b.Fatalf("Search fixture: got %d hits, err=%v; want non-empty", len(res), err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := repo.Search(ctx, "concurrency consensus"); err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}

func BenchmarkGetSubtree(b *testing.B) {
	db, root := benchDB(b, 500)
	repo := NewRepo(db, time.Now)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := repo.GetSubtree(ctx, knowledge.ID(root)); err != nil {
			b.Fatalf("GetSubtree: %v", err)
		}
	}
}
