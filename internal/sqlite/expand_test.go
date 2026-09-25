package sqlite

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type treeLLM struct{ tree []knowledge.Op }

func (l *treeLLM) GenerateTree(context.Context, string, string) ([]knowledge.Op, error) {
	return l.tree, nil
}

func (l *treeLLM) GenerateEdit(context.Context, []knowledge.Block, string) ([]knowledge.Op, error) {
	return nil, nil
}

func (l *treeLLM) GenerateDocument(context.Context, string, string) (string, error) {
	return "", nil
}

func (l *treeLLM) GenerateDocumentEdit(context.Context, knowledge.Block, string) (string, error) {
	return "", nil
}

func (l *treeLLM) Ping(context.Context) error { return nil }

func topicRootsOf(t *testing.T, repo *Repo) []knowledge.Block {
	t.Helper()
	blocks, err := repo.GetTopics(t.Context())
	require.NoError(t, err)
	return slices.DeleteFunc(blocks, func(b knowledge.Block) bool { return b.ParentID != "" })
}

func expandService(repo *Repo, llm knowledge.LLMClient) *knowledge.Service {
	seq := 0
	return knowledge.NewService(llm, repo, func() knowledge.ID {
		seq++
		return knowledge.ID(fmt.Sprintf("blk-%03d", seq))
	})
}

func TestExpandOutlineGrowsTheTopicItWasGiven(t *testing.T) {
	repo := testRepo(t)
	svc := expandService(repo, &treeLLM{tree: []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "t1", Content: "Go Contexts"},
		{Kind: knowledge.OpAdd, TempID: "t2", Parent: "t1", Content: "Cancellation propagates."},
		{Kind: knowledge.OpAdd, TempID: "t3", Parent: "t2", Content: "A derived context goes with it."},
	}})

	topicID, err := svc.CreateTopic(t.Context(), "Go Contexts", knowledge.ModeOutline)
	require.NoError(t, err, "create topic")
	_, err = svc.ExpandTopicByID(t.Context(), topicID, "")
	require.NoError(t, err, "expand")

	roots := topicRootsOf(t, repo)
	require.Len(t, roots, 1, "want only the topic that was expanded")
	require.Equal(t, topicID, roots[0].ID)
	assert.Equal(t, "Go Contexts", roots[0].Content, "topic content must stay untouched")

	sub, err := repo.GetSubtree(t.Context(), topicID)
	require.NoError(t, err)
	require.Len(t, sub, 3, "want the root and the two generated blocks")
	assert.Equal(t, topicID, sub[1].ParentID, "%q must hang from the topic root", sub[1].Content)
	assert.Equal(t, sub[1].ID, sub[2].ParentID, "%q parent", sub[2].Content)
}

func TestExpandOutlineMakesNoSecondTopic(t *testing.T) {
	repo := testRepo(t)
	svc := expandService(repo, &treeLLM{tree: []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "t1", Content: "Go contexts in depth"},
		{Kind: knowledge.OpAdd, TempID: "t2", Parent: "t1", Content: "Cancellation propagates."},
	}})

	topicID, err := svc.CreateTopic(t.Context(), "Go Contexts", knowledge.ModeOutline)
	require.NoError(t, err, "create topic")
	_, err = svc.ExpandTopicByID(t.Context(), topicID, "")
	require.NoError(t, err, "expand")

	roots := topicRootsOf(t, repo)
	require.Len(t, roots, 1)
	require.Equal(t, topicID, roots[0].ID)
	sub, err := repo.GetSubtree(t.Context(), topicID)
	require.NoError(t, err)
	require.Len(t, sub, 2)
	require.Equal(t, "Cancellation propagates.", sub[1].Content, "want the generated child under the topic")
}
