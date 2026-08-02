package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// Repair brings forward a database that already reports the current schema
// version but was written before migrations[0] was edited in place. Such a
// file cannot be reached by the version chain — it has nothing left to apply —
// so the work here is driven by what the schema actually carries, probed piece
// by piece, in the same order checkCurrent verifies afterwards.
//
// It runs on every start and must therefore be a no-op on a current database:
// every step asks first and does nothing when its piece is already there.
//
// It adds and backfills; it never drops. The legacy edges and proposals tables
// are left where they are. Typed edges have no representation after inline
// references and cannot become them without rewriting text the owner wrote, so
// discarding them is a decision for the owner rather than for a migration that
// runs unattended.
func repair(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, step := range []struct {
		what string
		fn   func(context.Context, *sql.Tx) error
	}{
		{"revisions", repairRevisions},
		{"revision_groups.atomic", repairAtomic},
		{"blocks.title_key", repairTitleKey},
		{"refs", repairRefs},
	} {
		if err := step.fn(ctx, tx); err != nil {
			return fmt.Errorf("repair %s: %w", step.what, translate(err))
		}
	}
	return tx.Commit()
}

// readable reports whether a table carries every column a repair step needs to
// read. A schema that does not is one this code has no account of — a corrupt
// file, or a shape from a branch that never shipped — and the step skips
// rather than failing on a raw driver error. checkCurrent then refuses with the
// message written for the user, which is the right answer for a base nothing
// here can rescue.
func readable(ctx context.Context, tx *sql.Tx, table string, columns ...string) (bool, error) {
	for _, c := range columns {
		has, err := columnExists(ctx, tx, table, c)
		if err != nil || !has {
			return false, err
		}
	}
	return true, nil
}

// tableExists reports whether the schema carries a table by that name.
func tableExists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`,
		name).Scan(&exists)
	return exists, err
}

// columnExists reports whether a table carries a column by that name.
func columnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM pragma_table_info('%s') WHERE name = ?)`, table),
		column).Scan(&exists)
	return exists, err
}

// migrationGroupID ties every revision this repair creates to one batch, so a
// reviewer looking at history can see where the blocks came from rather than
// finding history that claims to start with an edit nobody made.
const migrationGroupID = "00000000000000000000000000"

// repairRevisions gives a pre-revisions database the revision tables and one
// head revision per existing block. Without it the tree renders while history,
// review and undo have nothing underneath: blocks is a projection of a head
// revision that does not exist.
//
// Existing blocks are recorded as reviewed. They were the owner's settled
// knowledge before the upgrade, and an upgrade that invents a review backlog
// out of an entire base teaches the owner to click Accept all without looking,
// which is the one habit the review model cannot survive.
func repairRevisions(ctx context.Context, tx *sql.Tx) error {
	has, err := tableExists(ctx, tx, "block_revisions")
	if err != nil || has {
		return err
	}
	ok, err := readable(ctx, tx, "blocks", "id", "parent_id", "position", "content", "metadata", "created_at")
	if err != nil || !ok {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
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
CREATE INDEX IF NOT EXISTS idx_revisions_unreviewed ON block_revisions(block_id) WHERE reviewed = 0;
CREATE TABLE IF NOT EXISTS revision_groups (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    source     TEXT NOT NULL,
    atomic     INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);`); err != nil {
		return err
	}

	blocks, err := legacyBlocks(ctx, tx)
	if err != nil {
		return err
	}
	if len(blocks) == 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO revision_groups (id, title, source, atomic, created_at)
		 VALUES (?, 'blocks carried over from an earlier version', 'migration', 0, ?)`,
		migrationGroupID, blocks[0].createdAt); err != nil {
		return err
	}
	for _, b := range blocks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO block_revisions
			   (id, block_id, seq, content, parent_id, position, deleted,
			    author, source, origin, rationale, reviewed, group_id, created_at)
			 VALUES (?, ?, 1, ?, ?, ?, 0, ?, 'migration', NULL, NULL, 1, ?, ?)`,
			"migrated-"+b.id, b.id, b.content, b.parentID, b.position,
			b.author, migrationGroupID, b.createdAt); err != nil {
			return err
		}
	}
	return nil
}

// legacyBlock is one row of a pre-revisions blocks table, with the author
// recovered from the metadata the old model stamped there.
type legacyBlock struct {
	id        string
	parentID  sql.NullString
	position  int
	content   string
	author    string
	createdAt string
}

// legacyBlocks reads the blocks table in tree order, so a parent's revision is
// written before its children's and history reads in the order things were
// made.
func legacyBlocks(ctx context.Context, tx *sql.Tx) ([]legacyBlock, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, parent_id, position, content, metadata, created_at
		   FROM blocks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []legacyBlock
	for rows.Next() {
		var b legacyBlock
		var metadata string
		if err := rows.Scan(&b.id, &b.parentID, &b.position, &b.content, &metadata, &b.createdAt); err != nil {
			return nil, err
		}
		b.author = authorFromMetadata(metadata)
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

// authorFromMetadata recovers who wrote a block from the metadata the
// pre-revisions model stamped on it. Anything unreadable or unset reads as the
// owner: claiming the AI wrote something it may not have is the more damaging
// of the two mistakes, since authorship is what the owner checks a year later.
func authorFromMetadata(metadata string) string {
	var meta struct {
		Author string `json:"author"`
	}
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		return string(knowledge.AuthorOwner)
	}
	if meta.Author == string(knowledge.AuthorAI) {
		return string(knowledge.AuthorAI)
	}
	return string(knowledge.AuthorOwner)
}

// repairAtomic adds the column that marks a batch whose blocks are only true
// together. Databases written between the revisions model and topic renames
// carry revision_groups without it.
func repairAtomic(ctx context.Context, tx *sql.Tx) error {
	table, err := tableExists(ctx, tx, "revision_groups")
	if err != nil || !table {
		return err
	}
	has, err := columnExists(ctx, tx, "revision_groups", "atomic")
	if err != nil || has {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE revision_groups ADD COLUMN atomic INTEGER NOT NULL DEFAULT 0`)
	return err
}

// repairTitleKey adds the topic name a [[reference]] resolves by and fills it
// in for every root.
//
// The name has to be unique among roots, and an older base was never held to
// that. Where two roots normalize to the same name the earlier one keeps it
// and the others are left unnamed: a root without a title_key is legal — it is
// simply not addressable by name — whereas renaming one to break the tie would
// edit text the owner wrote to satisfy a migration.
func repairTitleKey(ctx context.Context, tx *sql.Tx) error {
	has, err := columnExists(ctx, tx, "blocks", "title_key")
	if err != nil || has {
		return err
	}
	ok, err := readable(ctx, tx, "blocks", "id", "parent_id", "content", "created_at")
	if err != nil || !ok {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE blocks ADD COLUMN title_key TEXT`); err != nil {
		return err
	}

	roots, err := namedRoots(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range roots {
		if _, err := tx.ExecContext(ctx,
			`UPDATE blocks SET title_key = ? WHERE id = ?`, r.key, r.id); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_topic_title ON blocks(title_key) WHERE parent_id IS NULL`)
	return err
}

// namedRoot is a root block paired with the name it will answer to.
type namedRoot struct{ id, key string }

// namedRoots reads the roots in the order they were made and assigns each the
// name its first line yields, skipping any name an earlier root already took.
// Reading finishes before the caller writes: a query left open across writes
// on the same transaction is how SQLite deadlocks against itself.
func namedRoots(ctx context.Context, tx *sql.Tx) ([]namedRoot, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, content FROM blocks WHERE parent_id IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roots []namedRoot
	taken := make(map[string]bool)
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, err
		}
		key := knowledge.TopicKey(content)
		if key == "" || taken[key] {
			continue
		}
		taken[key] = true
		roots = append(roots, namedRoot{id: id, key: key})
	}
	return roots, rows.Err()
}

// repairRefs adds the reference index and derives it from block content, the
// same way every write does. Without the backfill a base opens and reads while
// every reference already written in its text answers "no backlinks".
func repairRefs(ctx context.Context, tx *sql.Tx) error {
	has, err := tableExists(ctx, tx, "refs")
	if err != nil || has {
		return err
	}
	ok, err := readable(ctx, tx, "blocks", "id", "content")
	if err != nil || !ok {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE refs (
    source_id  TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('topic', 'block')),
    target_key TEXT NOT NULL,
    PRIMARY KEY (source_id, kind, target_key)
);
CREATE INDEX IF NOT EXISTS idx_refs_target ON refs(target_key);`); err != nil {
		return err
	}

	contents, err := blockContents(ctx, tx)
	if err != nil {
		return err
	}
	for id, content := range contents {
		if err := putRefs(ctx, tx, id, content); err != nil {
			return err
		}
	}
	return nil
}

// blockContents reads every block's text up front, for the same reason
// namedRoots does: the refs it derives are written back to the same
// transaction.
func blockContents(ctx context.Context, tx *sql.Tx) (map[knowledge.ID]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, content FROM blocks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contents := make(map[knowledge.ID]string)
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, err
		}
		contents[knowledge.ID(id)] = content
	}
	return contents, rows.Err()
}
