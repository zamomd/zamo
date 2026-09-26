package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

// putRefs replaces a block's outgoing references with the ones its content
// carries. Called from every op that changes content, inside the apply
// transaction, so the index can never disagree with the text. Empty content
// (a tombstoned block) simply clears the rows.
func putRefs(ctx context.Context, tx *sql.Tx, source knowledge.ID, content string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM refs WHERE source_id = ?`, string(source)); err != nil {
		return fmt.Errorf("clear refs of %s: %w", source, translate(err))
	}
	for _, ref := range knowledge.ParseRefs(content) {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO refs (source_id, kind, target_key) VALUES (?, ?, ?)`,
			string(source), string(ref.Kind), ref.TargetKey); err != nil {
			return fmt.Errorf("index ref %s->%s: %w", source, ref.TargetKey, translate(err))
		}
	}
	return nil
}
