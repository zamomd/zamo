package sqlite

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// renameService wires the domain to the real repository, so a rename runs the
// way the app runs it: the expansion in the domain, one transaction below it.
func renameService(repo *Repo) *knowledge.Service {
	seq := 0
	newID := func() knowledge.ID { seq++; return knowledge.ID(fmt.Sprintf("grp-%03d", seq)) }
	return knowledge.NewService(nil, repo, newID, nil)
}

// headGroup returns the group id of a block's most recent revision.
func headGroup(t *testing.T, repo *Repo, id string) string {
	t.Helper()
	var group string
	if err := repo.db.QueryRowContext(t.Context(),
		`SELECT group_id FROM block_revisions WHERE block_id = ? ORDER BY seq DESC LIMIT 1`, id).Scan(&group); err != nil {
		t.Fatalf("read head revision of %s: %v", id, err)
	}
	return group
}

func renameBase(t *testing.T, repo *Repo) {
	t.Helper()
	if err := ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01LOCAL", Content: "Local-first"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "We use [[Local-first]] and [[local-first]] daily.",
		},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01OTHER", Parent: "01ZIG",
			Content: "Nothing to see here.",
		},
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func TestBlocksReferencingTopic(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)

	got, err := repo.BlocksReferencingTopic(t.Context(), "local-first")
	if err != nil {
		t.Fatalf("BlocksReferencingTopic: %v", err)
	}
	if len(got) != 1 || got[0].ID != "01NOTE" {
		t.Fatalf("got %v, want exactly the one referencing block 01NOTE", got)
	}
	if got[0].Content != "We use [[Local-first]] and [[local-first]] daily." {
		t.Errorf("content = %q, want the block's full text to rewrite", got[0].Content)
	}
	if got[0].ParentID != "01ZIG" {
		t.Errorf("parent = %q, want the block fully hydrated", got[0].ParentID)
	}

	none, err := repo.BlocksReferencingTopic(t.Context(), "nothing here")
	if err != nil {
		t.Fatalf("BlocksReferencingTopic: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("got %d blocks for an unknown topic, want 0", len(none))
	}
}

func TestRenameTopicRewritesReferencesInOneGroup(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	svc := renameService(repo)

	if err := svc.EditBlockContent(t.Context(), "01LOCAL", "Offline-first"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	want := "We use [[Offline-first]] and [[Offline-first]] daily."
	if _, _, content, ok := blockRow(t, repo, "01NOTE"); !ok || content != want {
		t.Errorf("referencing block = %q (present %v), want %q", content, ok, want)
	}
	if got, want := titleKeyOf(t, repo, "01LOCAL"), "offline-first"; got != want {
		t.Errorf("title_key = %q, want %q", got, want)
	}
	if got := headGroup(t, repo, "01LOCAL"); got != headGroup(t, repo, "01NOTE") {
		t.Errorf("root and rewritten block landed in different groups (%q), want one reviewable group", got)
	}
	links, err := repo.GetBacklinks(t.Context(), "01LOCAL")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(links) != 1 || links[0].Block.ID != "01NOTE" {
		t.Errorf("backlinks = %v, want the rewritten reference still resolving to the topic", links)
	}
}

// An AI rename arrives as a plain edit op through ApplyOps, the path
// write_blocks and EditWithAI share. Without expansion there the refs rows keep
// the old key, so the topic loses every backlink the moment it is renamed.
func TestAIRenameKeepsReferencesResolving(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	svc := renameService(repo)

	meta := knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: "rename the topic"}
	if _, err := svc.ApplyOps(t.Context(), meta,
		[]knowledge.Op{{Kind: knowledge.OpEdit, BlockID: "01LOCAL", Content: "Offline-first"}}); err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}

	want := "We use [[Offline-first]] and [[Offline-first]] daily."
	if _, _, content, ok := blockRow(t, repo, "01NOTE"); !ok || content != want {
		t.Errorf("referencing block = %q (present %v), want %q", content, ok, want)
	}
	links, err := repo.GetBacklinks(t.Context(), "01LOCAL")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(links) != 1 || links[0].Block.ID != "01NOTE" {
		t.Errorf("backlinks = %v, want the reference still resolving after an AI rename", links)
	}
	if got := headGroup(t, repo, "01LOCAL"); got != headGroup(t, repo, "01NOTE") {
		t.Errorf("root and rewritten block landed in different groups (%q), want one reviewable group", got)
	}
	if reviewed := reviewedHead(t, repo, "01NOTE"); reviewed {
		t.Error("the rewrite is already reviewed, want an AI batch awaiting the owner's decision")
	}
}

// The point of CreateTopic: the reference the user clicked must resolve
// afterwards, which only holds because the name is written verbatim and
// title_key is derived from exactly that text.
func TestCreateTopicResolvesTheReferenceThatAskedForIt(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	svc := renameService(repo)

	id, err := svc.CreateTopic(t.Context(), "Nothing to see here")
	if err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}
	if got, want := titleKeyOf(t, repo, id), "nothing to see here"; got != want {
		t.Fatalf("title_key = %q, want %q", got, want)
	}

	// 01OTHER's text is a plain sentence, so a reference to it resolves only
	// once a topic of that exact name exists.
	if err := svc.EditBlockContent(t.Context(), "01OTHER", "See [[Nothing to see here]]."); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	links, err := repo.GetBacklinks(t.Context(), id)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(links) != 1 || links[0].Block.ID != "01OTHER" {
		t.Errorf("backlinks = %v, want the reference resolving to the created topic", links)
	}

	if _, err := svc.CreateTopic(t.Context(), "nothing to SEE here"); !errors.Is(err, knowledge.ErrDuplicateTopic) {
		t.Errorf("second CreateTopic = %v, want ErrDuplicateTopic", err)
	}
}

// reviewedHead reports the review state of a block's most recent revision.
func reviewedHead(t *testing.T, repo *Repo, id string) bool {
	t.Helper()
	var reviewed bool
	if err := repo.db.QueryRowContext(t.Context(),
		`SELECT reviewed FROM block_revisions WHERE block_id = ? ORDER BY seq DESC LIMIT 1`, id).Scan(&reviewed); err != nil {
		t.Fatalf("read head revision of %s: %v", id, err)
	}
	return reviewed
}

func TestRenameOntoATakenNameWritesNothing(t *testing.T) {
	repo := testRepo(t)
	renameBase(t, repo)
	if err := ownerOps(t, repo, "01G2",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01OFFLINE", Content: "Offline-first"},
	); err != nil {
		t.Fatalf("setup rival topic: %v", err)
	}
	revisions, groups := countRows(t, repo, "block_revisions"), countRows(t, repo, "revision_groups")
	svc := renameService(repo)

	err := svc.EditBlockContent(t.Context(), "01LOCAL", "Offline-first")
	if !errors.Is(err, knowledge.ErrDuplicateTopic) {
		t.Fatalf("rename onto a taken name: got %v, want ErrDuplicateTopic", err)
	}

	if _, _, content, _ := blockRow(t, repo, "01LOCAL"); content != "Local-first" {
		t.Errorf("root content = %q, want the rejected rename rolled back", content)
	}
	if got, want := titleKeyOf(t, repo, "01LOCAL"), "local-first"; got != want {
		t.Errorf("title_key = %q, want %q", got, want)
	}
	want := "We use [[Local-first]] and [[local-first]] daily."
	if _, _, content, _ := blockRow(t, repo, "01NOTE"); content != want {
		t.Errorf("referencing block = %q, want the rewrite rolled back too", content)
	}
	if got := countRows(t, repo, "block_revisions"); got != revisions {
		t.Errorf("block_revisions = %d, want %d: the failed batch left revisions behind", got, revisions)
	}
	if got := countRows(t, repo, "revision_groups"); got != groups {
		t.Errorf("revision_groups = %d, want %d: the failed batch left its group behind", got, groups)
	}
	refs, err := repo.BlocksReferencingTopic(t.Context(), "local-first")
	if err != nil {
		t.Fatalf("BlocksReferencingTopic: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "01NOTE" {
		t.Errorf("refs index = %v, want the old reference untouched", refs)
	}
}
