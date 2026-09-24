package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
)

// blockColumns is the block projection every read shares, in the order
// scanBlock expects. The trailing EXISTS subquery derives Unreviewed: never
// stored, always computed from whether the block has any pending revision.
const blockColumns = `id, parent_id, position, content, mode, metadata, created_at, updated_at,
	EXISTS(SELECT 1 FROM block_revisions r WHERE r.block_id = blocks.id AND r.reviewed = 0)`

// GetTopics implements knowledge.Repository: the root topics plus their first
// level of subtopics, the slice the map and tree screens render. Grandchildren
// and deeper are excluded. The result is flat but position-ordered — each root
// followed by its children, like GetSubtree; callers rebuild the hierarchy from
// ParentID and Position.
func (r *Repo) GetTopics(ctx context.Context) ([]knowledge.Block, error) {
	const q = `
SELECT ` + blockColumns + ` FROM blocks
WHERE parent_id IS NULL
   OR parent_id IN (SELECT id FROM blocks WHERE parent_id IS NULL)
ORDER BY COALESCE((SELECT p.position FROM blocks p WHERE p.id = blocks.parent_id), position),
         COALESCE(parent_id, id), parent_id IS NOT NULL, position`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	blocks, err := scanBlocks(rows)
	if err != nil {
		return nil, translate(err)
	}
	return blocks, nil
}

// subtreeQuery reads a root's whole subtree as blocks, unordered; callers run
// preorder over the result.
const subtreeQuery = subtreeCTE + `
SELECT ` + blockColumns + ` FROM blocks WHERE id IN (SELECT id FROM subtree)`

// GetSubtree implements knowledge.Repository. It collects the block and all its
// descendants with a recursive CTE, then orders them pre-order (parents before
// children, siblings by position) in Go. A missing root yields ErrNotFound.
func (r *Repo) GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	rows, err := r.db.QueryContext(ctx, subtreeQuery, string(root))
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	blocks, err := scanBlocks(rows)
	if err != nil {
		return nil, translate(err)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("subtree root %s: %w", root, knowledge.ErrNotFound)
	}
	return preorder(blocks, root), nil
}

// preorder reorders a flat block set into a pre-order depth-first walk from root:
// parents before children, siblings by position. A visited set guards against a
// cyclic parent_id so the walk cannot recurse forever even on a corrupt database.
func preorder(blocks []knowledge.Block, root knowledge.ID) []knowledge.Block {
	byID := make(map[knowledge.ID]knowledge.Block, len(blocks))
	kids := make(map[knowledge.ID][]knowledge.ID)
	for _, b := range blocks {
		byID[b.ID] = b
		kids[b.ParentID] = append(kids[b.ParentID], b.ID)
	}
	for parent := range kids {
		slices.SortStableFunc(kids[parent], func(a, b knowledge.ID) int {
			return cmp.Compare(byID[a].Position, byID[b].Position)
		})
	}

	out := make([]knowledge.Block, 0, len(blocks))
	visited := make(map[knowledge.ID]bool, len(blocks))
	var walk func(id knowledge.ID)
	walk = func(id knowledge.ID) {
		b, ok := byID[id]
		if !ok || visited[id] {
			return
		}
		visited[id] = true
		out = append(out, b)
		for _, child := range kids[id] {
			walk(child)
		}
	}
	walk(root)
	return out
}

// GetBlocks implements knowledge.Repository: the blocks with the given ids in
// one query. Missing ids are simply absent from the map — absence is the
// caller's stale signal, not an error.
func (r *Repo) GetBlocks(ctx context.Context, ids []knowledge.ID) (map[knowledge.ID]knowledge.Block, error) {
	out := make(map[knowledge.ID]knowledge.Block, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = string(id)
	}
	q := `SELECT ` + blockColumns + ` FROM blocks WHERE id IN (?` +
		strings.Repeat(",?", len(ids)-1) + `)`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()
	blocks, err := scanBlocks(rows)
	if err != nil {
		return nil, translate(err)
	}
	for _, b := range blocks {
		out[b.ID] = b
	}
	return out, nil
}

// searchLimit caps how many hits Search returns. Full-text results past the
// most relevant few hundred are noise for a personal knowledge base.
const searchLimit = 200

// Search implements knowledge.Repository: FTS5 full-text over block content,
// most relevant first (bm25). The user's words are matched as an implicit AND
// of literal terms — see ftsQuery. A blank query returns no hits without
// touching the database. Each hit carries an FTS-generated snippet around the
// match, since a block's first line alone can't locate a hit inside a
// document.
func (r *Repo) Search(ctx context.Context, query string) ([]knowledge.SearchHit, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}

	const q = `
SELECT b.id, b.parent_id, b.position, b.content, b.mode, b.metadata, b.created_at, b.updated_at,
	EXISTS(SELECT 1 FROM block_revisions r WHERE r.block_id = b.id AND r.reviewed = 0),
	snippet(blocks_fts, 0, '', '', '…', 24)
FROM blocks_fts
JOIN blocks b ON b.rowid = blocks_fts.rowid
WHERE blocks_fts MATCH ?
ORDER BY rank
LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, match, searchLimit)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	var hits []knowledge.SearchHit
	for rows.Next() {
		var (
			b                knowledge.Block
			parent           sql.NullString
			meta             string
			created, updated string
			snippet          string
		)
		if err := rows.Scan(&b.ID, &parent, &b.Position, &b.Content, &b.Mode, &meta,
			&created, &updated, &b.Unreviewed, &snippet); err != nil {
			return nil, translate(err)
		}
		if err := decodeBlockFields(&b, parent, meta, created, updated); err != nil {
			return nil, translate(err)
		}
		hits = append(hits, knowledge.SearchHit{Block: b, Snippet: snippet})
	}
	if err := rows.Err(); err != nil {
		return nil, translate(err)
	}
	return hits, nil
}

// ftsQuery turns a raw user string into a safe FTS5 MATCH expression. Each
// whitespace-separated token is wrapped in double quotes (embedded quotes
// doubled), which strips every FTS5 operator of its meaning: AND/OR/NEAR, the
// prefix '*', column filters and '-' become literal text. Space-joined quoted
// phrases are an implicit AND, so every word must appear. A query with no
// tokens yields an empty string, the signal for Search to skip the query.
func ftsQuery(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}
	quoted := make([]string, len(fields))
	for i, f := range fields {
		quoted[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " ")
}

// scanBlocks reads every row of a block query into domain blocks.
func scanBlocks(rows *sql.Rows) ([]knowledge.Block, error) {
	var blocks []knowledge.Block
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

// scanBlock maps one row (blockColumns order) into a domain block: a NULL
// parent_id becomes an empty ID (a topic root), metadata JSON is decoded, and
// the TEXT timestamps are parsed via parseTime.
func scanBlock(rows *sql.Rows) (knowledge.Block, error) {
	var (
		b                knowledge.Block
		parent           sql.NullString
		meta             string
		created, updated string
	)
	if err := rows.Scan(&b.ID, &parent, &b.Position, &b.Content, &b.Mode, &meta,
		&created, &updated, &b.Unreviewed); err != nil {
		return knowledge.Block{}, err
	}
	if err := decodeBlockFields(&b, parent, meta, created, updated); err != nil {
		return knowledge.Block{}, err
	}
	return b, nil
}

// decodeBlockFields fills in the fields the blockColumns projection carries
// beyond id/position/content — ParentID, Meta, CreatedAt, UpdatedAt — shared
// by every reader that scans that projection, including ones (like
// GetBacklinks) that cannot use blockColumns' literal SQL because it joins
// blocks under an alias.
func decodeBlockFields(b *knowledge.Block, parent sql.NullString, meta, created, updated string) error {
	b.ParentID = knowledge.ID(parent.String)
	if err := json.Unmarshal([]byte(meta), &b.Meta); err != nil {
		return fmt.Errorf("decode metadata for block %s: %w", b.ID, err)
	}
	var err error
	if b.CreatedAt, err = parseTime(created); err != nil {
		return fmt.Errorf("parse created_at for block %s: %w", b.ID, err)
	}
	if b.UpdatedAt, err = parseTime(updated); err != nil {
		return fmt.Errorf("parse updated_at for block %s: %w", b.ID, err)
	}
	return nil
}
