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
