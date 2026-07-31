package sqlite

import (
	"context"
	"database/sql"
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

// Migrate applies missing migrations. The current schema version is kept
// in PRAGMA user_version.
func Migrate(ctx context.Context, db *sql.DB) error {
	for {
		done, err := migrateStep(ctx, db)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
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
