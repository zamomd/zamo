package tools_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/sqlite"
	"github.com/zamomd/zamo/internal/tools"
	"github.com/zamomd/zamo/internal/ulid"
)

func openService(t *testing.T) *knowledge.Service {
	t.Helper()
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "zamo.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }
	return knowledge.NewService(sqlite.NewRepo(db, time.Now, newID), newID)
}

var owner = knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "seed"}

// seedTopic writes a topic with the given children and returns the topic id
// followed by the children's ids.
func seedTopic(t *testing.T, svc *knowledge.Service, name string, children ...string) []knowledge.ID {
	t.Helper()
	ops := []knowledge.Op{{Kind: knowledge.OpAdd, TempID: "root", Content: name}}
	for i, c := range children {
		ops = append(ops, knowledge.Op{
			Kind: knowledge.OpAdd, TempID: "c" + string(rune('0'+i)), Parent: "root", Content: c,
		})
	}
	applied, err := svc.ApplyOps(t.Context(), owner, ops)
	require.NoError(t, err)
	ids := []knowledge.ID{applied.IDs["root"]}
	for i := range children {
		ids = append(ids, applied.IDs["c"+string(rune('0'+i))])
	}
	return ids
}

func find(t *testing.T, set []tools.Tool, name string) tools.Tool {
	t.Helper()
	for _, tool := range set {
		if tool.Name == name {
			return tool
		}
	}
	require.Failf(t, "no such tool", "%q", name)
	return tools.Tool{}
}

// call runs a tool and decodes its result into T through JSON, the way an
// adapter hands it to a model.
func call[T any](t *testing.T, tool tools.Tool, args string) T {
	t.Helper()
	res, err := tool.Call(t.Context(), jsontext.Value(args))
	require.NoError(t, err, tool.Name)
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestReadOffersTheFourReadTools(t *testing.T) {
	set := tools.Read(openService(t), tools.Limits{})

	var names []string
	for _, tool := range set {
		names = append(names, tool.Name)
		assert.NotEmpty(t, tool.Description, tool.Name)
		var schema map[string]any
		require.NoError(t, json.Unmarshal(tool.Schema, &schema), tool.Name)
		assert.Equal(t, "object", schema["type"], "%s takes an arguments object", tool.Name)
	}
	assert.ElementsMatch(t, []string{"search", "get_subtree", "topic_map", "get_backlinks"}, names)
}

func TestSearchReturnsHitsWithSnippets(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "a deadline cancels the request")
	search := find(t, tools.Read(svc, tools.Limits{}), "search")

	out := call[tools.SearchOutput](t, search, `{"query":"deadline"}`)

	require.Len(t, out.Hits, 1)
	assert.Equal(t, string(ids[1]), out.Hits[0].ID)
	assert.Equal(t, string(ids[0]), out.Hits[0].Parent)
	assert.Contains(t, out.Hits[0].Snippet, "deadline")
}

func TestGetSubtreeReturnsTheBlockAndItsDescendants(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Go contexts", "first", "second")
	subtree := find(t, tools.Read(svc, tools.Limits{}), "get_subtree")

	out := call[tools.BlocksOutput](t, subtree, `{"root":"`+string(ids[0])+`"}`)

	require.Len(t, out.Blocks, 3)
	assert.Equal(t, "Go contexts", out.Blocks[0].Content)
	assert.Equal(t, "outline", out.Blocks[0].Mode, "a root reports its mode")
	assert.Empty(t, out.Blocks[1].Mode, "a child has none")
	assert.False(t, out.Truncated)
}

func TestGetSubtreeOfAnUnknownBlockIsAnError(t *testing.T) {
	subtree := find(t, tools.Read(openService(t), tools.Limits{}), "get_subtree")

	_, err := subtree.Call(t.Context(), jsontext.Value(`{"root":"01NOSUCHBLOCK"}`))

	require.ErrorIs(t, err, knowledge.ErrNotFound)
}

func TestTopicMapListsRoots(t *testing.T) {
	svc := openService(t)
	seedTopic(t, svc, "Go contexts", "first")
	seedTopic(t, svc, "Rust lifetimes")
	topicMap := find(t, tools.Read(svc, tools.Limits{}), "topic_map")

	out := call[tools.BlocksOutput](t, topicMap, `{}`)

	var contents []string
	for _, b := range out.Blocks {
		contents = append(contents, b.Content)
	}
	assert.Subset(t, contents, []string{"Go contexts", "Rust lifetimes", "first"})
}

func TestTopicMapAcceptsMissingArguments(t *testing.T) {
	topicMap := find(t, tools.Read(openService(t), tools.Limits{}), "topic_map")

	_, err := topicMap.Call(t.Context(), nil)

	assert.NoError(t, err)
}

func TestGetBacklinksNamesTheReferencingTopic(t *testing.T) {
	svc := openService(t)
	target := seedTopic(t, svc, "Go contexts")
	seedTopic(t, svc, "Servers", "handlers take [[Go contexts]] first")
	backlinks := find(t, tools.Read(svc, tools.Limits{}), "get_backlinks")

	out := call[tools.BacklinksOutput](t, backlinks, `{"id":"`+string(target[0])+`"}`)

	require.Len(t, out.Backlinks, 1)
	assert.Equal(t, "Servers", out.Backlinks[0].TopicTitle)
	assert.Contains(t, out.Backlinks[0].Content, "[[Go contexts]]")
}

func TestLimitsCutLongContentAndSaySo(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Long", strings.Repeat("é", 500))
	subtree := find(t, tools.Read(svc, tools.Limits{MaxContent: 100}), "get_subtree")

	out := call[tools.BlocksOutput](t, subtree, `{"root":"`+string(ids[0])+`"}`)

	content := out.Blocks[1].Content
	assert.True(t, strings.HasPrefix(content, strings.Repeat("é", 100)), "the first 100 characters survive whole")
	assert.NotContains(t, content, strings.Repeat("é", 101))
	assert.Contains(t, content, "400 more characters")
	assert.True(t, out.Truncated)
}

func TestLimitsCapTheNumberOfBlocks(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Wide", "a", "b", "c", "d")
	subtree := find(t, tools.Read(svc, tools.Limits{MaxBlocks: 2}), "get_subtree")

	out := call[tools.BlocksOutput](t, subtree, `{"root":"`+string(ids[0])+`"}`)

	assert.Len(t, out.Blocks, 2)
	assert.True(t, out.Truncated)
}

func TestArgumentsThatDoNotFitAreAnError(t *testing.T) {
	search := find(t, tools.Read(openService(t), tools.Limits{}), "search")

	for _, args := range []string{`{"query":7}`, `{"quarry":"x"}`, `not json`} {
		_, err := search.Call(context.Background(), jsontext.Value(args))
		assert.ErrorIs(t, err, tools.ErrBadArguments, args)
	}
}
