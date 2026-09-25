package sqlite

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenRefusesAPreReferencesDatabase pins the one failure migrations[0]
// being rewritten in place left behind: a base from an earlier release reports
// the current schema version, so nothing is applied and nothing complains until
// the first write.
func TestOpenRefusesAPreReferencesDatabase(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "old.db")

	old, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err, "open raw database")
	// The shape of a released base: current version, no refs table.
	_, err = old.ExecContext(ctx, `
		CREATE TABLE blocks (id TEXT PRIMARY KEY, parent_id TEXT, content TEXT NOT NULL);
		PRAGMA user_version = 1`)
	require.NoError(t, err, "build the old schema")
	require.NoError(t, old.Close(), "close raw database")

	db, err := Open(ctx, path)
	if err == nil {
		db.Close()
	}
	require.ErrorIs(t, err, ErrSchemaTooOld, "Open must refuse a database predating inline references")
	assert.ErrorContains(t, err, "delete the database file", "want the remedy spelled out for the user")
}

// Every in-place edit of migrations[0] leaves a shape behind: a base created
// mid-branch reports the current version and carries part of the schema. It
// opens and renders, then fails on the first write with an untranslated driver
// error the user reads as "Something went wrong".
func TestOpenRefusesAMidBranchDatabase(t *testing.T) {
	schemas := map[string]string{
		"no refs table": `
			CREATE TABLE blocks (id TEXT PRIMARY KEY, title_key TEXT);
			CREATE TABLE revision_groups (id TEXT PRIMARY KEY, atomic INTEGER NOT NULL DEFAULT 0)`,
		"no blocks.title_key": `
			CREATE TABLE blocks (id TEXT PRIMARY KEY);
			CREATE TABLE refs (source_id TEXT NOT NULL);
			CREATE TABLE revision_groups (id TEXT PRIMARY KEY, atomic INTEGER NOT NULL DEFAULT 0)`,
		"no revision_groups.atomic": `
			CREATE TABLE blocks (id TEXT PRIMARY KEY, title_key TEXT);
			CREATE TABLE refs (source_id TEXT NOT NULL);
			CREATE TABLE revision_groups (id TEXT PRIMARY KEY, title TEXT NOT NULL)`,
	}
	for name, schema := range schemas {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "mid.db")

			old, err := sql.Open("sqlite", "file:"+path)
			require.NoError(t, err, "open raw database")
			_, err = old.ExecContext(ctx, schema+"; PRAGMA user_version = 1")
			require.NoError(t, err, "build the partial schema")
			require.NoError(t, old.Close(), "close raw database")

			db, err := Open(ctx, path)
			if err == nil {
				db.Close()
			}
			require.ErrorIs(t, err, ErrSchemaTooOld, "Open must refuse a partial schema")
		})
	}
}

// A database the current migration built carries every feature the probe knows
// about, so the list cannot drift into refusing fresh bases.
func TestCurrentSchemaPassesEveryProbe(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "fresh.db"))
	require.NoError(t, err)
	defer db.Close()

	assert.NoError(t, checkCurrent(t.Context(), db))
}

func TestMigrateRefusesDatabaseWithoutMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.ExecContext(t.Context(),
		`CREATE TABLE blocks (id TEXT PRIMARY KEY, title_key TEXT);
		 CREATE TABLE refs (source_id TEXT);
		 CREATE TABLE revision_groups (id TEXT, atomic INTEGER);
		 PRAGMA user_version = 1`)
	require.NoError(t, err)
	require.ErrorIs(t, Migrate(t.Context(), db), ErrSchemaTooOld)
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
		assert.NoError(t, err, "concurrent Open #%d", i)
	}
}

func TestMigrateAndFTS(t *testing.T) {
	ctx := t.Context()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO blocks (id, parent_id, position, content, created_at, updated_at)
		VALUES ('01TEST', NULL, 0, 'pgx connection pooling', '2026-07-05', '2026-07-05')`)
	require.NoError(t, err, "insert block")

	var id string
	err = db.QueryRowContext(ctx, `
		SELECT b.id FROM blocks_fts f
		JOIN blocks b ON b.rowid = f.rowid
		WHERE blocks_fts MATCH 'pgx'`).Scan(&id)
	require.NoError(t, err, "full-text search after insert")
	require.Equal(t, "01TEST", id)

	_, err = db.ExecContext(ctx,
		`UPDATE blocks SET content = 'contexts in Go' WHERE id = '01TEST'`)
	require.NoError(t, err, "update block")

	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM blocks_fts WHERE blocks_fts MATCH 'pgx'`).Scan(&n), "search after update")
	require.Zero(t, n, "index is stale: old text still matches")
}

func TestMigrateRevisionTables(t *testing.T) {
	ctx := t.Context()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO revision_groups (id, title, source, created_at)
		VALUES ('group1', 'Test Group', 'ai', '2026-07-18')`)
	require.NoError(t, err, "insert revision_groups")

	_, err = db.ExecContext(ctx, `
		INSERT INTO block_revisions (id, block_id, seq, content, parent_id, position, author, reviewed, group_id, created_at)
		VALUES ('rev1', 'block1', 1, 'test content', NULL, 0, 'owner', 0, 'group1', '2026-07-18')`)
	require.NoError(t, err, "insert block_revisions")

	var count int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM block_revisions WHERE reviewed = 0`).Scan(&count)
	require.NoError(t, err, "query unreviewed revisions")
	require.Equal(t, 1, count, "unreviewed revisions")
}
