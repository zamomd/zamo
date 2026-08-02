package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// The schemas below are what earlier builds actually created. They are written
// out rather than derived from migrations[0], because the point of the test is
// that migrations[0] no longer describes them: it was edited in place, so a
// database made by those builds already reports the current version.

// schemaBeforeRefs is the schema between #95 and #115: revisions exist,
// inline references do not.
const schemaBeforeRefs = `
CREATE TABLE blocks (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT REFERENCES blocks(id),
    position   INTEGER NOT NULL DEFAULT 0,
    content    TEXT NOT NULL,
    metadata   TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_blocks_parent ON blocks(parent_id, position);

CREATE TABLE block_revisions (
    id         TEXT PRIMARY KEY,
    block_id   TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    content    TEXT NOT NULL,
    parent_id  TEXT,
    position   INTEGER NOT NULL,
    deleted    INTEGER NOT NULL DEFAULT 0,
    author     TEXT NOT NULL,
    source     TEXT,
    origin     TEXT,
    rationale  TEXT,
    reviewed   INTEGER NOT NULL,
    group_id   TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (block_id, seq)
);
CREATE INDEX idx_revisions_unreviewed ON block_revisions(block_id) WHERE reviewed = 0;

CREATE TABLE revision_groups (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    source     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE blocks_fts USING fts5(
    content, content='blocks', content_rowid='rowid'
);
`

// schemaBeforeRevisions is the schema before #95: a proposal queue and typed
// edges, no revisions at all.
const schemaBeforeRevisions = `
CREATE TABLE blocks (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT REFERENCES blocks(id),
    position   INTEGER NOT NULL DEFAULT 0,
    content    TEXT NOT NULL,
    metadata   TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_blocks_parent ON blocks(parent_id, position);

CREATE TABLE edges (
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    kind      TEXT NOT NULL,
    PRIMARY KEY (source_id, target_id, kind)
);

CREATE TABLE proposals (
    id         TEXT PRIMARY KEY,
    source     TEXT NOT NULL,
    ops        TEXT NOT NULL,
    status     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE blocks_fts USING fts5(
    content, content='blocks', content_rowid='rowid'
);
`

// openLegacy creates a database carrying schema, stamped with the current
// version exactly as the build that wrote it would have stamped it.
func openLegacy(t *testing.T, schema string, seed ...string) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.ExecContext(t.Context(), schema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	for _, stmt := range seed {
		if _, err := db.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateRepairsBaseWithoutRefs(t *testing.T) {
	db := openLegacy(t, schemaBeforeRefs,
		`INSERT INTO blocks (id, parent_id, position, content, created_at, updated_at)
		 VALUES ('b1', NULL, 0, 'Contexts in Go', '2026-07-20', '2026-07-20'),
		        ('b2', 'b1', 0, 'See [[Marine navigation basics]] for the analogy', '2026-07-20', '2026-07-20'),
		        ('b3', NULL, 1, 'Marine navigation basics', '2026-07-20', '2026-07-20')`)

	if err := Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// The topic name is what a [[reference]] resolves by, so it has to be
	// backfilled for roots and left alone for children.
	var key sql.NullString
	if err := db.QueryRowContext(t.Context(), `SELECT title_key FROM blocks WHERE id = 'b3'`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if !key.Valid || key.String == "" {
		t.Errorf("title_key of the root = %v, want the normalized topic name", key)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT title_key FROM blocks WHERE id = 'b2'`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key.Valid {
		t.Errorf("title_key of a child = %q, want NULL — only roots carry a name", key.String)
	}

	// The reference already written in the text must be found after the
	// upgrade, or a base that reads fine still answers "no backlinks".
	var refs int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM refs WHERE source_id = 'b2'`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if refs != 1 {
		t.Errorf("refs from b2 = %d, want 1 backfilled from its content", refs)
	}
}

func TestMigrateRepairsBaseWithoutRevisions(t *testing.T) {
	db := openLegacy(t, schemaBeforeRevisions,
		`INSERT INTO blocks (id, parent_id, position, content, metadata, created_at, updated_at)
		 VALUES ('b1', NULL, 0, 'Contexts in Go', '{"author":"ai"}', '2026-07-10', '2026-07-10'),
		        ('b2', 'b1', 0, 'Cancellation propagates down', '{}', '2026-07-10', '2026-07-10')`,
		`INSERT INTO edges (source_id, target_id, kind) VALUES ('b2', 'b1', 'related')`)

	if err := Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Every block needs a head revision: blocks is a projection of one, and
	// without it history, review and undo have nothing to stand on.
	var revs int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM block_revisions`).Scan(&revs); err != nil {
		t.Fatal(err)
	}
	if revs != 2 {
		t.Errorf("revisions = %d, want one head revision per existing block", revs)
	}

	// Authorship was recorded in metadata under the old model; losing it would
	// erase the answer to "was this mine or the AI's".
	var author string
	const authorOfB1 = `SELECT author FROM block_revisions WHERE block_id = 'b1'`
	if err := db.QueryRowContext(t.Context(), authorOfB1).Scan(&author); err != nil {
		t.Fatal(err)
	}
	if author != "ai" {
		t.Errorf("author of b1 = %q, want ai — its metadata said so", author)
	}

	// Blocks that predate the upgrade are the owner's settled knowledge, not a
	// pile of changes to review.
	var unreviewed int
	const unreviewedCount = `SELECT count(*) FROM block_revisions WHERE reviewed = 0`
	if err := db.QueryRowContext(t.Context(), unreviewedCount).Scan(&unreviewed); err != nil {
		t.Fatal(err)
	}
	if unreviewed != 0 {
		t.Errorf("unreviewed revisions = %d, want 0 — an upgrade must not invent a review backlog", unreviewed)
	}
}

// The content of a repaired base has to survive, or the upgrade is just a
// slower delete.
func TestMigrateKeepsContent(t *testing.T) {
	db := openLegacy(t, schemaBeforeRevisions,
		`INSERT INTO blocks (id, parent_id, position, content, created_at, updated_at)
		 VALUES ('b1', NULL, 0, 'Contexts in Go', '2026-07-10', '2026-07-10')`)

	if err := Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var content string
	if err := db.QueryRowContext(t.Context(), `SELECT content FROM blocks WHERE id = 'b1'`).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "Contexts in Go" {
		t.Errorf("content = %q, want it untouched", content)
	}
}

// A database already carrying the current schema must come out unchanged:
// the repair runs on every start, so it has to be a no-op when there is
// nothing to repair.
func TestMigrateIsIdempotentOnACurrentBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO blocks (id, position, content, title_key, created_at, updated_at)
		 VALUES ('b1', 0, 'Topic', 'topic', '2026-08-02', '2026-08-02')`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var blocks int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM blocks`).Scan(&blocks); err != nil {
		t.Fatal(err)
	}
	if blocks != 1 {
		t.Errorf("blocks = %d, want the row left alone", blocks)
	}
}
