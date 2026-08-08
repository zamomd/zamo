package sqlite

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
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
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(blocks, func(b knowledge.Block) bool { return b.ParentID != "" })
}

func expandService(repo *Repo, llm knowledge.LLMClient) *knowledge.Service {
	seq := 0
	return knowledge.NewService(llm, repo, func() knowledge.ID {
		seq++
		return knowledge.ID(fmt.Sprintf("blk-%03d", seq))
	}, nil)
}

func TestExpandOutlineGrowsTheTopicItWasGiven(t *testing.T) {
	repo := testRepo(t)
	svc := expandService(repo, &treeLLM{tree: []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "t1", Content: "Go Contexts"},
		{Kind: knowledge.OpAdd, TempID: "t2", Parent: "t1", Content: "Cancellation propagates."},
		{Kind: knowledge.OpAdd, TempID: "t3", Parent: "t2", Content: "A derived context goes with it."},
	}})

	topicID, err := svc.CreateTopic(t.Context(), "Go Contexts", knowledge.ModeOutline)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if _, err := svc.ExpandTopicByID(t.Context(), topicID, ""); err != nil {
		t.Fatalf("expand: %v", err)
	}

	roots := topicRootsOf(t, repo)
	if len(roots) != 1 || roots[0].ID != topicID {
		t.Fatalf("topic roots = %+v, want only the one that was expanded (%s)", roots, topicID)
	}
	if roots[0].Content != "Go Contexts" {
		t.Errorf("topic content = %q, want it untouched", roots[0].Content)
	}

	sub, err := repo.GetSubtree(t.Context(), topicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 3 {
		t.Fatalf("subtree has %d blocks, want the root and the two generated ones", len(sub))
	}
	if sub[1].ParentID != topicID {
		t.Errorf("%q hangs from %s, want the topic root %s", sub[1].Content, sub[1].ParentID, topicID)
	}
	if sub[2].ParentID != sub[1].ID {
		t.Errorf("%q hangs from %s, want %s", sub[2].Content, sub[2].ParentID, sub[1].ID)
	}
}

func TestExpandOutlineMakesNoSecondTopic(t *testing.T) {
	repo := testRepo(t)
	svc := expandService(repo, &treeLLM{tree: []knowledge.Op{
		{Kind: knowledge.OpAdd, TempID: "t1", Content: "Go contexts in depth"},
		{Kind: knowledge.OpAdd, TempID: "t2", Parent: "t1", Content: "Cancellation propagates."},
	}})

	topicID, err := svc.CreateTopic(t.Context(), "Go Contexts", knowledge.ModeOutline)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if _, err := svc.ExpandTopicByID(t.Context(), topicID, ""); err != nil {
		t.Fatalf("expand: %v", err)
	}

	roots := topicRootsOf(t, repo)
	if len(roots) != 1 || roots[0].ID != topicID {
		t.Fatalf("topic roots = %+v, want only %s", roots, topicID)
	}
	sub, err := repo.GetSubtree(t.Context(), topicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 2 || sub[1].Content != "Cancellation propagates." {
		t.Fatalf("subtree = %+v, want the generated child under the topic", sub)
	}
}
