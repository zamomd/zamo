package sqlite

import (
	"slices"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func refsOf(t *testing.T, repo *Repo, source knowledge.ID) []string {
	t.Helper()
	rows, err := repo.db.QueryContext(t.Context(),
		`SELECT kind || ':' || target_key FROM refs WHERE source_id = ? ORDER BY kind, target_key`,
		string(source))
	if err != nil {
		t.Fatalf("read refs of %s: %v", source, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan refs of %s: %v", source, err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read refs of %s: %v", source, err)
	}
	return out
}

func TestRefsIndexedOnAdd(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT",
			Content: "Compare [[Rust]] with ((01JT4X9K2M8P0QRSTVWXYZ2345|a claim)).",
		},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	want := []string{"block:01JT4X9K2M8P0QRSTVWXYZ2345", "topic:rust"}
	if got := refsOf(t, repo, "01NOTE"); !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func TestRefsRebuiltOnEdit(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01NOTE", Content: "See [[Go]] instead."},
	); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got, want := refsOf(t, repo, "01NOTE"), []string{"topic:go"}; !slices.Equal(got, want) {
		t.Errorf("refs after edit = %v, want %v", got, want)
	}
}

func TestRefsClearedOnDelete(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpDelete, BlockID: "01NOTE"},
	); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := refsOf(t, repo, "01NOTE"); len(got) != 0 {
		t.Errorf("refs after delete = %v, want none", got)
	}
}

func TestRefsClearedForDeletedSubtree(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01DEEP", Parent: "01NOTE", Content: "And [[Go]]."},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpDelete, BlockID: "01NOTE"},
	); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := refsOf(t, repo, "01DEEP"); len(got) != 0 {
		t.Errorf("refs of deleted descendant = %v, want none", got)
	}
}

func TestRefsRestoredWithBlock(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := repo.ApplyOps(t.Context(), "01G2", aiMeta("ai delete"), []knowledge.Op{
		{Kind: knowledge.OpDelete, BlockID: "01NOTE"},
	}); err != nil {
		t.Fatalf("ai delete: %v", err)
	}
	if err := repo.RejectBlock(t.Context(), "01NOTE"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got, want := refsOf(t, repo, "01NOTE"), []string{"topic:rust"}; !slices.Equal(got, want) {
		t.Errorf("refs after restore = %v, want %v", got, want)
	}
}

func TestRefsClearedWhenRejectedAddVanishes(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := repo.ApplyOps(t.Context(), "01G2", aiMeta("ai add"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	}); err != nil {
		t.Fatalf("ai add: %v", err)
	}
	if err := repo.RejectBlock(t.Context(), "01NOTE"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, _, _, ok := blockRow(t, repo, "01NOTE"); ok {
		t.Fatal("rejected add left a block row")
	}
	if got := refsOf(t, repo, "01NOTE"); len(got) != 0 {
		t.Errorf("refs of vanished block = %v, want none", got)
	}
}

func TestRefsRevertWithUndoneGroup(t *testing.T) {
	repo := testRepo(t)
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ROOT", Content: "Zig"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ROOT", Content: "See [[Rust]]."},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01NOTE", Content: "See [[Go]] instead."},
	); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := repo.UndoGroup(t.Context(), "01G2"); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got, want := refsOf(t, repo, "01NOTE"), []string{"topic:rust"}; !slices.Equal(got, want) {
		t.Errorf("refs after undo = %v, want %v", got, want)
	}
}
