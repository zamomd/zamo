package sqlite

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestMigrateConcurrentStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")

	ctx := t.Context()

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() {
			db, err := Open(ctx, path)
			if err != nil {
				errs[i] = err
				return
			}
			db.Close()
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Open #%d: %v", i, err)
		}
	}
}

func TestMigrateAndFTS(t *testing.T) {
	ctx := t.Context()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO blocks (id, parent_id, position, content, created_at, updated_at)
		VALUES ('01TEST', NULL, 0, 'pgx connection pooling', '2026-07-05', '2026-07-05')`)
	if err != nil {
		t.Fatalf("insert block: %v", err)
	}

	var id string
	err = db.QueryRowContext(ctx, `
		SELECT b.id FROM blocks_fts f
		JOIN blocks b ON b.rowid = f.rowid
		WHERE blocks_fts MATCH 'pgx'`).Scan(&id)
	if err != nil {
		t.Fatalf("full-text search after insert: %v", err)
	}
	if id != "01TEST" {
		t.Fatalf("search returned %q, want 01TEST", id)
	}

	if _, err := db.ExecContext(ctx,
		`UPDATE blocks SET content = 'contexts in Go' WHERE id = '01TEST'`); err != nil {
		t.Fatalf("update block: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM blocks_fts WHERE blocks_fts MATCH 'pgx'`).Scan(&n); err != nil {
		t.Fatalf("search after update: %v", err)
	}
	if n != 0 {
		t.Fatalf("index is stale: old text still matches (%d hits)", n)
	}
}

func TestMigrateRevisionTables(t *testing.T) {
	ctx := t.Context()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO revision_groups (id, title, source, created_at)
		VALUES ('group1', 'Test Group', 'ai', '2026-07-18')`)
	if err != nil {
		t.Fatalf("insert revision_groups: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO block_revisions (id, block_id, seq, content, parent_id, position, author, reviewed, group_id, created_at)
		VALUES ('rev1', 'block1', 1, 'test content', NULL, 0, 'owner', 0, 'group1', '2026-07-18')`)
	if err != nil {
		t.Fatalf("insert block_revisions: %v", err)
	}

	var count int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM block_revisions WHERE reviewed = 0`).Scan(&count)
	if err != nil {
		t.Fatalf("query unreviewed revisions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 unreviewed revision, got %d", count)
	}
}
