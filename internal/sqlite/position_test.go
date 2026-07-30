package sqlite

import (
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
