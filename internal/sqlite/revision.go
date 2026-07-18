package sqlite

// revision row helpers shared by the write path (sqlite.go) and the review
// operations (Task 4: reject, mark-reviewed, list-unreviewed).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cominddev/comind/internal/knowledge"
)

// revisionColumns is the block_revisions projection every read shares, in
// the order scanRevision expects.
const revisionColumns = `id, block_id, seq, content, parent_id, position,
	deleted, author, source, origin, rationale, reviewed, group_id, created_at`

// rowQueryer is the read surface shared by *sql.DB and *sql.Tx, so
// headRevision works standalone (a plain read) or inside a write
// transaction (rebuilding the projection after an insert).
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanner is satisfied by *sql.Row and *sql.Rows, so scanRevision maps
// either a single lookup (headRevision) or one row of a multi-row listing
// (Task 4's ListRevisions and friends).
type scanner interface {
	Scan(dest ...any) error
}

// nextSeq returns the next per-block sequence number inside tx. The first
// revision of a block is seq 1.
func nextSeq(ctx context.Context, tx *sql.Tx, blockID string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM block_revisions WHERE block_id = ?`,
		blockID).Scan(&n)
	return n, err
}

// insertRevision writes one snapshot row. ParentID is empty for a topic root.
func insertRevision(ctx context.Context, tx *sql.Tx, rev knowledge.Revision, now string) error {
	var parent any
	if rev.ParentID != "" {
		parent = string(rev.ParentID)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO block_revisions
		(id, block_id, seq, content, parent_id, position, deleted, author,
		 source, origin, rationale, reviewed, group_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(rev.ID), string(rev.BlockID), rev.Seq, rev.Content, parent,
		rev.Position, rev.Deleted, string(rev.Author), string(rev.Source),
		rev.Origin, rev.Rationale, rev.Reviewed, string(rev.GroupID), now)
	return err
}

// headRevision returns the block's highest-seq revision, or ok=false when
// the block has none.
func headRevision(ctx context.Context, q rowQueryer, blockID string) (knowledge.Revision, bool, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+revisionColumns+` FROM block_revisions WHERE block_id = ? ORDER BY seq DESC LIMIT 1`,
		blockID)
	rev, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return knowledge.Revision{}, false, nil
	}
	if err != nil {
		return knowledge.Revision{}, false, err
	}
	return rev, true, nil
}

// scanRevision maps one revisionColumns row into a domain Revision: NULL
// parent_id becomes an empty ID (a topic root), the nullable provenance
// columns (source, origin, rationale, group_id) become their zero value, and
// created_at is parsed via parseTime.
func scanRevision(row scanner) (knowledge.Revision, error) {
	var (
		rev                                      knowledge.Revision
		id, blockID, author                      string
		parent, source, origin, rationale, group sql.NullString
		created                                  string
	)
	if err := row.Scan(&id, &blockID, &rev.Seq, &rev.Content, &parent, &rev.Position,
		&rev.Deleted, &author, &source, &origin, &rationale, &rev.Reviewed, &group, &created); err != nil {
		return knowledge.Revision{}, err
	}
	rev.ID = knowledge.ID(id)
	rev.BlockID = knowledge.ID(blockID)
	rev.ParentID = knowledge.ID(parent.String)
	rev.Author = knowledge.Author(author)
	rev.Source = knowledge.Source(source.String)
	rev.Origin = origin.String
	rev.Rationale = rationale.String
	rev.GroupID = knowledge.ID(group.String)
	t, err := parseTime(created)
	if err != nil {
		return knowledge.Revision{}, fmt.Errorf("parse created_at of revision %s: %w", id, err)
	}
	rev.CreatedAt = t
	return rev, nil
}

// projectHead rebuilds the blocks projection row for blockID from its
// current head revision (highest seq): the block whose recent revisions
// were just stripped (Task 4's RejectBlock) is restored to whatever
// revision is now the head. A deleted head — or no revisions at all —
// removes the projection row: there is nothing live left to show. Metadata
// (the JSON authorship stamp set once at creation, see applyAdd) is not
// part of a revision snapshot and is left untouched.
func projectHead(ctx context.Context, tx *sql.Tx, blockID knowledge.ID, now string) error {
	head, ok, err := headRevision(ctx, tx, string(blockID))
	if err != nil {
		return fmt.Errorf("project head of %s: %w", blockID, translate(err))
	}
	if !ok || head.Deleted {
		if _, err := tx.ExecContext(ctx, `DELETE FROM blocks WHERE id = ?`, string(blockID)); err != nil {
			return fmt.Errorf("project head of %s: %w", blockID, translate(err))
		}
		return nil
	}
	var parent any
	if head.ParentID != "" {
		parent = string(head.ParentID)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE blocks SET content = ?, parent_id = ?, position = ?, updated_at = ? WHERE id = ?`,
		head.Content, parent, head.Position, now, string(blockID)); err != nil {
		return fmt.Errorf("project head of %s: %w", blockID, translate(err))
	}
	return nil
}
