package sqlite

import (
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func titleKeyOf(t *testing.T, repo *Repo, id knowledge.ID) string {
	t.Helper()
	var key any
	if err := repo.db.QueryRowContext(t.Context(),
		`SELECT title_key FROM blocks WHERE id = ?`, string(id)).Scan(&key); err != nil {
		t.Fatalf("read title_key of %s: %v", id, err)
	}
	if key == nil {
		return ""
	}
	s, ok := key.(string)
	if !ok {
		t.Fatalf("title_key of %s is %T, want string or NULL", id, key)
	}
	return s
}

func ownerOps(t *testing.T, repo *Repo, group knowledge.ID, ops ...knowledge.Op) error {
	t.Helper()
	return repo.ApplyOps(t.Context(), group, ownerMeta("test"), ops)
}

func TestTitleKeyOnlyForRoots(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "## Local-First  Architecture"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01CHILD", Parent: "01ROOT", Content: "A child"},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, want := titleKeyOf(t, repo, "01ROOT"), "local-first architecture"; got != want {
		t.Errorf("root title_key = %q, want %q", got, want)
	}
	if got := titleKeyOf(t, repo, "01CHILD"); got != "" {
		t.Errorf("child title_key = %q, want empty", got)
	}
}

func TestTitleKeyFollowsEdits(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Old name"},
	); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01ROOT", Content: "New name"},
	); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got, want := titleKeyOf(t, repo, "01ROOT"), "new name"; got != want {
		t.Errorf("title_key after edit = %q, want %q", got, want)
	}
}

func TestTitleKeyFollowsMoves(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "Host"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "Guest"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01KID", Parent: "01R1", Content: "Promoted"},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpMove, BlockID: "01R2", Parent: "01R1"},
	); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if got := titleKeyOf(t, repo, "01R2"); got != "" {
		t.Errorf("demoted root title_key = %q, want empty", got)
	}
	if err := ownerOps(t, repo, "01G3",
		knowledge.Op{Kind: knowledge.OpMove, BlockID: "01KID"},
	); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if got, want := titleKeyOf(t, repo, "01KID"), "promoted"; got != want {
		t.Errorf("promoted child title_key = %q, want %q", got, want)
	}
}

func TestDuplicateTopicNameRejected(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "Local-first"},
	); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "  LOCAL-FIRST  "},
	)
	if !errors.Is(err, knowledge.ErrDuplicateTopic) {
		t.Fatalf("second add: got %v, want ErrDuplicateTopic", err)
	}
	var n int
	if err := repo.db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM blocks WHERE id = '01R2'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("rejected add left %d rows, want 0", n)
	}
}

func TestUnnamedRootsCoexist(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R1", Content: "###"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01R2", Content: "   "},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := titleKeyOf(t, repo, "01R1"); got != "" {
		t.Errorf("marker-only root title_key = %q, want empty", got)
	}
}

func TestTitleKeyRestoredWithTopic(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Local-first"},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := repo.ApplyOps(t.Context(), "01G2", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "01ROOT"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}
	if err := repo.RejectBlock(t.Context(), "01ROOT"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got, want := titleKeyOf(t, repo, "01ROOT"), "local-first"; got != want {
		t.Errorf("title_key after restore = %q, want %q", got, want)
	}
}
