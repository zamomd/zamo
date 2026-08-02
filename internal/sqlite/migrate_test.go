package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestOpenRefusesAnUnreadableDatabase pins what is left of the old refusal.
// A base whose blocks table this code cannot read is one it has no account of
// — a corrupt file, or a shape from a branch that never shipped. Repair skips
// what it cannot make sense of rather than failing on a raw driver error, and
// the refusal the user reads is the written one, with the remedy in it.
func TestOpenRefusesAnUnreadableDatabase(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "old.db")

	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	// Current version, and a blocks table missing everything a repair reads.
	if _, err := old.ExecContext(ctx, `
		CREATE TABLE blocks (id TEXT PRIMARY KEY);
		PRAGMA user_version = 1`); err != nil {
		t.Fatalf("build the old schema: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close raw database: %v", err)
	}

	db, err := Open(ctx, path)
	if err == nil {
		db.Close()
		t.Fatal("Open succeeded on a schema nothing can repair, want a refusal")
	}
	if !errors.Is(err, ErrSchemaTooOld) {
		t.Fatalf("error = %v, want ErrSchemaTooOld", err)
	}
	if got := err.Error(); !strings.Contains(got, "delete the database file") {
		t.Errorf("message = %q, want the remedy spelled out for the user", got)
	}
}

// Every in-place edit of migrations[0] left a shape behind: a base created
// mid-branch reports the current version while carrying part of the schema.
// Those shapes are now repaired on open rather than refused, which is the
// whole point of the repair — the user never learns the schema moved.
func TestOpenRepairsAMidBranchDatabase(t *testing.T) {
	schemas := map[string]string{
		"no revision_groups.atomic": `
			CREATE TABLE blocks (
				id TEXT PRIMARY KEY, parent_id TEXT, position INTEGER NOT NULL DEFAULT 0,
				content TEXT NOT NULL, title_key TEXT, metadata TEXT NOT NULL DEFAULT '{}',
				created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
			CREATE TABLE refs (source_id TEXT NOT NULL, kind TEXT NOT NULL, target_key TEXT NOT NULL);
			CREATE TABLE block_revisions (id TEXT PRIMARY KEY);
			CREATE TABLE revision_groups (id TEXT PRIMARY KEY, title TEXT NOT NULL)`,
		"no refs table": `
			CREATE TABLE blocks (
				id TEXT PRIMARY KEY, parent_id TEXT, position INTEGER NOT NULL DEFAULT 0,
				content TEXT NOT NULL, title_key TEXT, metadata TEXT NOT NULL DEFAULT '{}',
				created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
			CREATE TABLE block_revisions (id TEXT PRIMARY KEY);
			CREATE TABLE revision_groups (id TEXT PRIMARY KEY, atomic INTEGER NOT NULL DEFAULT 0)`,
	}
	for name, schema := range schemas {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "mid.db")

			old, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatalf("open raw database: %v", err)
			}
			if _, err := old.ExecContext(ctx, schema+"; PRAGMA user_version = 1"); err != nil {
				t.Fatalf("build the partial schema: %v", err)
			}
			if err := old.Close(); err != nil {
				t.Fatalf("close raw database: %v", err)
			}

			db, err := Open(ctx, path)
			if err != nil {
				t.Fatalf("Open on a repairable schema = %v, want it brought forward", err)
			}
			db.Close()
		})
	}
}

// A database the current migration built carries every feature the probe knows
// about, so the list cannot drift into refusing fresh bases.
func TestCurrentSchemaPassesEveryProbe(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := checkCurrent(t.Context(), db); err != nil {
		t.Errorf("checkCurrent on a freshly migrated database: %v", err)
	}
}

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
