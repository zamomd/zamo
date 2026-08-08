package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The full-text index blocks_fts is kept in sync with blocks by
// triggers: search can never fall behind the data. block_revisions holds a
// full snapshot per change, and blocks is a projection of each block's head
// revision.
var migrations = []string{
	`
CREATE TABLE blocks (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT REFERENCES blocks(id),
    position   INTEGER NOT NULL DEFAULT 0,
    content    TEXT NOT NULL,
    title_key  TEXT,
    mode       TEXT NOT NULL DEFAULT 'outline' CHECK (mode IN ('outline','doc')),
    metadata   TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_blocks_parent ON blocks(parent_id, position);

-- Topic names are unique among roots. NULL means the root's first line
-- yields no name, and the index tolerates any number of those.
CREATE UNIQUE INDEX idx_topic_title ON blocks(title_key) WHERE parent_id IS NULL;

-- refs is derived from block content and rebuilt on every write; it is an
-- index, never a second source of truth. A topic reference stores the
-- normalized name and resolves by joining blocks.title_key, so creating or
-- deleting a topic needs no maintenance here. A block reference stores the
-- target id.
CREATE TABLE refs (
    source_id  TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('topic', 'block')),
    target_key TEXT NOT NULL,
    PRIMARY KEY (source_id, kind, target_key)
);

CREATE INDEX idx_refs_target ON refs(target_key);

CREATE TABLE block_revisions (
    id         TEXT PRIMARY KEY,
    block_id   TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    content    TEXT NOT NULL,
    parent_id  TEXT,
    position   INTEGER NOT NULL,
    mode       TEXT NOT NULL DEFAULT 'outline',
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

-- Whether a batch can still be undone is asked of its group id (see
-- groupWrittenOver), once per pending batch on a path the Changes screen polls.
-- Without this the question is a full scan of the revision history, which is
-- what it costs on a queue holding many small batches.
--
-- Deliberately absent from schemaFeatures: a missing index changes only speed,
-- and that list refuses to open the database at all.
CREATE INDEX idx_revisions_group ON block_revisions(group_id);

-- atomic marks a batch whose blocks are only true together: a topic rename
-- and the reference rewrites it implies. Review takes such a group whole.
CREATE TABLE revision_groups (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    source     TEXT NOT NULL,
    atomic     INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE blocks_fts USING fts5(
    content,
    content='blocks',
    content_rowid='rowid'
);

CREATE TRIGGER blocks_fts_insert AFTER INSERT ON blocks BEGIN
    INSERT INTO blocks_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER blocks_fts_delete AFTER DELETE ON blocks BEGIN
    INSERT INTO blocks_fts(blocks_fts, rowid, content)
    VALUES ('delete', old.rowid, old.content);
END;

CREATE TRIGGER blocks_fts_update AFTER UPDATE OF content ON blocks BEGIN
    INSERT INTO blocks_fts(blocks_fts, rowid, content)
    VALUES ('delete', old.rowid, old.content);
    INSERT INTO blocks_fts(rowid, content) VALUES (new.rowid, new.content);
END;
`,
}

// ErrSchemaTooOld reports a database whose schema predates some part of the
// current migrations[0]. It was rewritten in place rather than followed by a
// second migration, so such a file already reports the current schema version
// and no migration can bring it forward — whether it comes from an earlier
// release or from an earlier state of this branch. The message names the only
// remedy, because it is what the user reads when the app refuses to start.
var ErrSchemaTooOld = errors.New(
	"this database was created before inline references and cannot be upgraded: " +
		"delete the database file to start fresh")

// Migrate applies missing migrations. The current schema version is kept
// in PRAGMA user_version.
func Migrate(ctx context.Context, db *sql.DB) error {
	for {
		done, err := migrateStep(ctx, db)
		if err != nil {
			return err
		}
		if done {
			return checkCurrent(ctx, db)
		}
	}
}

// schemaFeatures are the pieces migrations[0] gained after it started being
// edited in place, each with the query that finds it. Every in-place edit adds
// one: a database created between two such edits reports the current version
// and carries some of the list but not all, so the probe has to be the whole
// list rather than whichever piece came last.
var schemaFeatures = []struct {
	what  string
	probe string
}{
	{"refs", `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'refs')`},
	{"blocks.title_key", `SELECT EXISTS(SELECT 1 FROM pragma_table_info('blocks') WHERE name = 'title_key')`},
	{"revision_groups.atomic", `SELECT EXISTS(SELECT 1 FROM pragma_table_info('revision_groups') WHERE name = 'atomic')`},
	{"blocks.mode", `SELECT EXISTS(SELECT 1 FROM pragma_table_info('blocks') WHERE name = 'mode')`},
}

// checkCurrent verifies that a database already at the current schema version
// carries the current schema. Such a base opens and reads perfectly well — the
// tree renders — while writes and queries hit whatever it is missing, which
// reaches the user as the catch-all "Something went wrong". Probing here turns
// that into one legible refusal at startup instead.
func checkCurrent(ctx context.Context, db *sql.DB) error {
	for _, f := range schemaFeatures {
		var exists bool
		if err := db.QueryRowContext(ctx, f.probe).Scan(&exists); err != nil {
			return fmt.Errorf("check schema for %s: %w", f.what, err)
		}
		if !exists {
			return fmt.Errorf("%w (schema has no %s)", ErrSchemaTooOld, f.what)
		}
	}
	return nil
}

// migrateStep applies at most one pending migration and reports whether
// the schema is up to date.
func migrateStep(ctx context.Context, db *sql.DB) (done bool, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return false, fmt.Errorf("read schema version: %w", err)
	}
	if version >= len(migrations) {
		return true, nil
	}

	if _, err := tx.ExecContext(ctx, migrations[version]); err != nil {
		return false, fmt.Errorf("migration %d: %w", version+1, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
		return false, fmt.Errorf("migration %d: set version: %w", version+1, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("migration %d: %w", version+1, err)
	}
	return false, nil
}
