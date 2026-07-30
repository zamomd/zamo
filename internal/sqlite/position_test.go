package sqlite

import (
	"errors"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// seedChildren applies one batch adding n children under parent and returns
// their ids in the order they were added.
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
	// Group id distinct from seedChildren's "g-root" (its "g-"+parent scheme),
	// since revision_groups.id is a primary key and the two batches collide
	// under the same connection otherwise.
	if err := repo.ApplyOps(t.Context(), "g-init", aiMeta("root"),
		[]knowledge.Op{{Kind: knowledge.OpAdd, BlockID: "root", Content: "Root"}}); err != nil {
		t.Fatal(err)
	}
	seedChildren(t, repo, "root", "a", "b", "c")

	// The first child starts at 0 and every later one is a full stride past
	// the previous, so a block can later be inserted between any two.
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
	// Neither neighbour moved: their revisions still describe where they are.
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

	// "other" is a topic root, not a child of "root": anchoring against it
	// would silently append instead of inserting, so the batch is refused.
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

	// Enter at the very start of the first child: the new block must sort
	// ahead of it, and there is no predecessor to take a midpoint against.
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
	// Force the pathological state a long run of midpoint inserts would
	// reach: adjacent positions with no integer between them. The anchor
	// ("b") is not first, so its position changes across the renumber (1 ->
	// 65536): anchoring on the first child instead would map to position 0
	// both before and after respacing, and a stale-anchor bug would slip
	// through unnoticed.
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

	// The children were respaced first (a=0, b=65536, c=131072), so the
	// midpoint after b exists again and the order is a, b, mid, c.
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

// TestApplyOpsMovesChildBeforeDeletingItsOldParent pins MergeBlocks' shape —
// edit target, move a child of source onto target, delete source — across
// the domain/storage seam. A domain test can only assert op kinds against a
// fake repo; it cannot see applyDelete's real cascade. This one runs the
// batch through actual SQLite so a future change hoisting the subtree read
// above the move, or a schema gaining ON DELETE CASCADE, would surface here:
// the child must survive under target, not vanish along with source.
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

// TestApplyMoveWithAfterLandsOnTheMidpoint drives an anchored OpMove through
// applyMove with real SQLite — the shape OutdentBlock ships (reparent plus
// land directly after a named sibling), which until now no test exercised
// below the domain layer.
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
	// Force the pathological state a long run of midpoint inserts would
	// reach: adjacent positions with no integer between them. The anchor
	// ("c") is not first, so its position changes across the renumber (2 ->
	// 131072): this is what lets the test tell positionBefore's retry path
	// apart from a bug that reuses the anchor/prev it read before triggering
	// the renumber instead of re-reading them.
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

	// The children were respaced first (a=0, b=65536, c=131072), so the
	// midpoint before c exists again and the order is a, b, mid, c.
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
