package sqlite

import (
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

func seedChildren(t *testing.T, repo *Repo, parent string, contents ...string) []string {
	t.Helper()
	ops := make([]knowledge.Op, 0, len(contents))
	ids := make([]string, 0, len(contents))
	for _, c := range contents {
		id := knowledge.ID(c + "-id")
		ids = append(ids, string(id))
		ops = append(ops, knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: id, Parent: parent, Content: c,
		})
	}
	if err := repo.ApplyOps(t.Context(), knowledge.ID("g-"+parent), aiMeta("seed"), ops); err != nil {
		t.Fatalf("seed children of %q: %v", parent, err)
	}
	return ids
}

func TestAppendPositionsUseTheStride(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b", "c")

	for i, id := range []string{"a-id", "b-id", "c-id"} {
		_, pos, _, ok := blockRow(t, repo, id)
		if !ok {
			t.Fatalf("block %s missing", id)
		}
		if want := i * positionStride; pos != want {
			t.Errorf("position of %s = %d, want %d", id, pos, want)
		}
	}
}

func TestInsertAfterMiddleSiblingTakesTheMidpoint(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b", "c") // 0, 65536, 131072

	if err := repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root",
			After: "b-id", Content: "between b and c",
		}}); err != nil {
		t.Fatalf("insert after: %v", err)
	}

	_, pos, _, ok := blockRow(t, repo, "mid")
	if !ok {
		t.Fatal("inserted block missing")
	}
	if want := positionStride + positionStride/2; pos != want {
		t.Errorf("position = %d, want %d (midpoint of 65536 and 131072)", pos, want)
	}
	for id, want := range map[string]int{"b-id": positionStride, "c-id": 2 * positionStride} {
		if _, p, _, _ := blockRow(t, repo, id); p != want {
			t.Errorf("neighbour %s position = %d, want %d (untouched)", id, p, want)
		}
	}
}

func TestInsertAfterLastSiblingAppends(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b") // 0, 65536

	if err := repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "tail", Parent: "root",
			After: "b-id", Content: "after b",
		}}); err != nil {
		t.Fatalf("insert after last: %v", err)
	}
	if _, pos, _, _ := blockRow(t, repo, "tail"); pos != 2*positionStride {
		t.Errorf("position = %d, want %d", pos, 2*positionStride)
	}
}

func TestInsertAfterForeignSiblingIsRefused(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{
			{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
			{Kind: knowledge.OpAdd, BlockID: "other", Content: "Other root"},
		}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a")

	err := repo.ApplyOps(t.Context(), "g-bad", aiMeta("bad"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "x", Parent: "root",
			After: "other", Content: "nope",
		}})
	if !errors.Is(err, knowledge.ErrInvalidOps) {
		t.Errorf("error = %v, want ErrInvalidOps", err)
	}
	if _, _, _, ok := blockRow(t, repo, "x"); ok {
		t.Error("refused batch still wrote the block")
	}
}

func TestInsertBeforeFirstSiblingGoesAhead(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b") // 0, 65536

	if err := repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "head", Parent: "root",
			Before: "a-id", Content: "before a",
		}}); err != nil {
		t.Fatalf("insert before first: %v", err)
	}
	if _, pos, _, _ := blockRow(t, repo, "head"); pos != -positionStride {
		t.Errorf("position = %d, want %d", pos, -positionStride)
	}
	if _, pos, _, _ := blockRow(t, repo, "a-id"); pos != 0 {
		t.Errorf("first child moved to %d, want 0 (untouched)", pos)
	}
}

func TestInsertIntoExhaustedGapRespaces(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b", "c") // 0, 65536, 131072
	for id, pos := range map[string]int{"a-id": 0, "b-id": 1, "c-id": 2} {
		if _, err := repo.db.ExecContext(t.Context(),
			`UPDATE blocks SET position = ? WHERE id = ?`, pos, id); err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root",
			After: "b-id", Content: "squeezed",
		}}); err != nil {
		t.Fatalf("insert into exhausted gap: %v", err)
	}

	want := []struct {
		id  string
		pos int
	}{
		{"a-id", 0},
		{"b-id", positionStride},
		{"mid", positionStride + positionStride/2},
		{"c-id", 2 * positionStride},
	}
	for _, w := range want {
		if _, pos, _, ok := blockRow(t, repo, w.id); !ok || pos != w.pos {
			t.Errorf("position of %s = %d (present %v), want %d", w.id, pos, ok, w.pos)
		}
	}
}

func TestApplyOpsMovesChildBeforeDeletingItsOldParent(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "target", Parent: "root", Content: "Target"},
		{Kind: knowledge.OpAdd, BlockID: "source", Parent: "root", Content: "Source"},
		{Kind: knowledge.OpAdd, BlockID: "kid", Parent: "source", Content: "Kid"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.ApplyOps(t.Context(), "g-merge", aiMeta("merge"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "target", Content: "Target+Source"},
		{Kind: knowledge.OpMove, BlockID: "kid", Parent: "target"},
		{Kind: knowledge.OpDelete, BlockID: "source"},
	}); err != nil {
		t.Fatalf("apply merge-shaped batch: %v", err)
	}

	parent, _, content, ok := blockRow(t, repo, "kid")
	if !ok {
		t.Fatal("kid did not survive the batch: applyDelete's cascade removed it")
	}
	if parent != "target" || content != "Kid" {
		t.Errorf("kid = parent=%q content=%q, want target/Kid", parent, content)
	}
	if _, _, _, ok := blockRow(t, repo, "source"); ok {
		t.Error("source still present after its own delete op")
	}
}

func TestApplyMoveWithAfterLandsOnTheMidpoint(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("setup"), []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"},
		{Kind: knowledge.OpAdd, BlockID: "mover", Content: "Mover"}, // a lone topic root to reparent in
	}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b") // 0, 65536

	if err := repo.ApplyOps(t.Context(), "g-move", aiMeta("move"), []knowledge.Op{
		{Kind: knowledge.OpMove, BlockID: "mover", Parent: "root", After: "a-id"},
	}); err != nil {
		t.Fatalf("anchored move: %v", err)
	}

	_, pos, _, ok := blockRow(t, repo, "mover")
	if !ok {
		t.Fatal("moved block missing")
	}
	if want := positionStride / 2; pos != want {
		t.Errorf("position = %d, want %d (midpoint of 0 and %d)", pos, want, positionStride)
	}
}

func TestInsertBeforeIntoExhaustedGapRespaces(t *testing.T) {
	repo := testRepo(t)
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b", "c") // 0, 65536, 131072
	for id, pos := range map[string]int{"a-id": 0, "b-id": 1, "c-id": 2} {
		if _, err := repo.db.ExecContext(t.Context(),
			`UPDATE blocks SET position = ? WHERE id = ?`, pos, id); err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.ApplyOps(t.Context(), "g-ins", aiMeta("insert"),
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, BlockID: "mid", Parent: "root",
			Before: "c-id", Content: "squeezed",
		}}); err != nil {
		t.Fatalf("insert before into exhausted gap: %v", err)
	}

	want := []struct {
		id  string
		pos int
	}{
		{"a-id", 0},
		{"b-id", positionStride},
		{"mid", positionStride + positionStride/2},
		{"c-id", 2 * positionStride},
	}
	for _, w := range want {
		if _, pos, _, ok := blockRow(t, repo, w.id); !ok || pos != w.pos {
			t.Errorf("position of %s = %d (present %v), want %d", w.id, pos, ok, w.pos)
		}
	}
}

func TestRenumberKeepsEveryRevisionAlignedForRollback(t *testing.T) {
	repo := testRepo(t)
	ctx := t.Context()
	owner := knowledge.WriteMeta{
		Author: knowledge.AuthorOwner,
		Source: knowledge.SourceGUIManual,
		Title:  "setup",
	}
	if err := repo.ApplyOps(ctx, "g-rollback-setup", owner, []knowledge.Op{
		{Kind: knowledge.OpAdd, BlockID: "rollback-root", Content: "root"},
		{Kind: knowledge.OpAdd, BlockID: "rollback-first", Parent: "rollback-root", Content: "first"},
		{Kind: knowledge.OpAdd, BlockID: "rollback-bystander", Parent: "rollback-root", Content: "before"},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := repo.ApplyOps(ctx, "g-rollback-edit", aiMeta("edit bystander"), []knowledge.Op{
		{Kind: knowledge.OpEdit, BlockID: "rollback-bystander", Content: "pending"},
	}); err != nil {
		t.Fatalf("edit bystander: %v", err)
	}

	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin renumber: %v", err)
	}
	if err := renumber(ctx, tx, "rollback-root"); err != nil {
		tx.Rollback()
		t.Fatalf("renumber: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit renumber: %v", err)
	}

	wantPosition := positionStride
	for _, rev := range revisionsOf(t, repo, "rollback-bystander") {
		if rev.position != wantPosition {
			t.Errorf("revision %d position = %d, want %d", rev.seq, rev.position, wantPosition)
		}
	}
	if err := repo.RejectBlock(ctx, "rollback-bystander"); err != nil {
		t.Fatalf("reject bystander: %v", err)
	}
	parent, position, content, ok := blockRow(t, repo, "rollback-bystander")
	if !ok || parent != "rollback-root" || position != wantPosition || content != "before" {
		t.Errorf("bystander after rollback = parent=%q position=%d content=%q ok=%v, want rollback-root/%d/before/true",
			parent, position, content, ok, wantPosition)
	}
}
