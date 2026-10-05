package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
)

// topicNames lists every key a [[Topic]] reference resolves by, with the live
// root it reaches: each root's own name, then its aliases. An alias of a topic
// that is gone, or that stopped being a root, joins nothing and drops out.
const topicNames = `(
    SELECT title_key AS name_key, id AS topic_id FROM blocks
    WHERE parent_id IS NULL AND title_key IS NOT NULL
    UNION ALL
    SELECT a.alias_key, a.topic_id FROM topic_aliases a
    JOIN blocks b ON b.id = a.topic_id AND b.parent_id IS NULL
)`

// requireTopic reports ErrNotFound unless id is a live topic root.
func requireTopic(ctx context.Context, q queryRower, id knowledge.ID) error {
	var root bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM blocks WHERE id = ? AND parent_id IS NULL)`,
		string(id)).Scan(&root); err != nil {
		return translate(err)
	}
	if !root {
		return fmt.Errorf("topic %s: %w", id, knowledge.ErrNotFound)
	}
	return nil
}

// queryRower is the single-row read surface shared by *sql.DB and *sql.Tx.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// GetTopicProps implements knowledge.Repository.
func (r *Repo) GetTopicProps(ctx context.Context, topic knowledge.ID) (knowledge.TopicProps, error) {
	if err := requireTopic(ctx, r.db, topic); err != nil {
		return knowledge.TopicProps{}, err
	}
	var props knowledge.TopicProps
	var err error
	if props.Aliases, err = queryIDs(ctx, r.db,
		`SELECT alias FROM topic_aliases WHERE topic_id = ? ORDER BY position`, string(topic)); err != nil {
		return knowledge.TopicProps{}, fmt.Errorf("read aliases of %s: %w", topic, err)
	}
	if props.Tags, err = queryIDs(ctx, r.db,
		`SELECT tag FROM topic_tags WHERE topic_id = ? ORDER BY position`, string(topic)); err != nil {
		return knowledge.TopicProps{}, fmt.Errorf("read tags of %s: %w", topic, err)
	}
	return props, nil
}

// SetTopicProps implements knowledge.Repository. An alias equal to the topic's
// own name is dropped rather than refused: it says nothing the name does not.
func (r *Repo) SetTopicProps(ctx context.Context, topic knowledge.ID, props knowledge.TopicProps) error {
	return r.withTx(ctx, fmt.Sprintf("set properties of %s", topic), func(tx *sql.Tx) error {
		return setTopicProps(ctx, tx, topic, props)
	})
}

func setTopicProps(ctx context.Context, tx *sql.Tx, topic knowledge.ID, props knowledge.TopicProps) error {
	if err := requireTopic(ctx, tx, topic); err != nil {
		return err
	}
	var own sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT title_key FROM blocks WHERE id = ?`, string(topic)).Scan(&own); err != nil {
		return translate(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM topic_aliases WHERE topic_id = ?`, string(topic)); err != nil {
		return fmt.Errorf("clear aliases of %s: %w", topic, translate(err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM topic_tags WHERE topic_id = ?`, string(topic)); err != nil {
		return fmt.Errorf("clear tags of %s: %w", topic, translate(err))
	}

	for i, alias := range props.Aliases {
		key := knowledge.TopicKey(alias)
		if key == "" {
			return fmt.Errorf("alias %q: %w", alias, knowledge.ErrUnreferenceableTopic)
		}
		if own.Valid && own.String == key {
			continue
		}
		var taken bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM `+topicNames+` WHERE name_key = ?)`, key).Scan(&taken); err != nil {
			return translate(err)
		}
		if taken {
			return fmt.Errorf("alias %q: %w", alias, knowledge.ErrDuplicateTopic)
		}
		// A row left by a topic that is gone holds the key without resolving
		// to anything; the alias is free, so the stale row gives way.
		if _, err := tx.ExecContext(ctx, `DELETE FROM topic_aliases WHERE alias_key = ?`, key); err != nil {
			return translate(err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO topic_aliases (topic_id, alias_key, alias, position) VALUES (?, ?, ?, ?)`,
			string(topic), key, alias, i); err != nil {
			return fmt.Errorf("alias %q: %w", alias, translate(err))
		}
	}

	for i, tag := range props.Tags {
		key := knowledge.TagKey(tag)
		if key == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO topic_tags (topic_id, tag_key, tag, position) VALUES (?, ?, ?, ?)`,
			string(topic), key, tag, i); err != nil {
			return fmt.Errorf("tag %q: %w", tag, translate(err))
		}
	}
	return nil
}

// ListTopicAliases implements knowledge.Repository. Only aliases of live
// topics are listed.
func (r *Repo) ListTopicAliases(ctx context.Context) ([]knowledge.TopicAlias, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT a.topic_id, a.alias FROM topic_aliases a
JOIN blocks b ON b.id = a.topic_id AND b.parent_id IS NULL
ORDER BY a.topic_id, a.position`)
	if err != nil {
		return nil, fmt.Errorf("list aliases: %w", translate(err))
	}
	defer rows.Close()

	var out []knowledge.TopicAlias
	for rows.Next() {
		var a knowledge.TopicAlias
		if err := rows.Scan(&a.TopicID, &a.Alias); err != nil {
			return nil, fmt.Errorf("list aliases: %w", translate(err))
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list aliases: %w", translate(err))
	}
	return out, nil
}

// ListTopicTags implements knowledge.Repository. Only tags of live topics are
// listed.
func (r *Repo) ListTopicTags(ctx context.Context) ([]knowledge.TopicTag, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT g.topic_id, g.tag FROM topic_tags g
JOIN blocks b ON b.id = g.topic_id AND b.parent_id IS NULL
ORDER BY g.topic_id, g.position`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", translate(err))
	}
	defer rows.Close()

	var out []knowledge.TopicTag
	for rows.Next() {
		var g knowledge.TopicTag
		if err := rows.Scan(&g.TopicID, &g.Tag); err != nil {
			return nil, fmt.Errorf("list tags: %w", translate(err))
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tags: %w", translate(err))
	}
	return out, nil
}

// requireNameFree refuses a root name that is an alias of another topic: the
// two would answer to one reference. key is what titleKey returned, so nil —
// a child, or a root nothing can reference — has nothing to collide with.
func requireNameFree(ctx context.Context, tx *sql.Tx, self knowledge.ID, key any) error {
	if key == nil {
		return nil
	}
	var taken bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM topic_aliases a
JOIN blocks b ON b.id = a.topic_id AND b.parent_id IS NULL
WHERE a.alias_key = ? AND a.topic_id <> ?)`, key, string(self)).Scan(&taken); err != nil {
		return translate(err)
	}
	if taken {
		return fmt.Errorf("%q is an alias of another topic: %w", key, knowledge.ErrDuplicateTopic)
	}
	return nil
}
