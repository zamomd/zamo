package vaultimport_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
	"github.com/cominddev/comind/internal/modules/vaultimport"
	"github.com/cominddev/comind/internal/modules/vaultimport/logseq"
	"github.com/cominddev/comind/internal/modules/vaultimport/obsidian"
	"github.com/cominddev/comind/internal/sqlite"
	"github.com/cominddev/comind/internal/ulid"
)

// openService assembles the domain over a fresh sqlite base, as main.go
// does, so the module is exercised against the real write path. Tests may
// import storage; the module itself may not.
func openService(t *testing.T) *knowledge.Service {
	t.Helper()
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "comind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }
	return knowledge.NewService(llm.NewHolder(nil), sqlite.NewRepo(db, time.Now, newID), newID)
}

func TestImportLogseqGraphEndToEnd(t *testing.T) {
	svc := openService(t)
	m := vaultimport.New(svc, svc, obsidian.Adapter{}, logseq.Adapter{})
	vaultimport.SetPicker(m, fakePicker{answer: "testdata/logseq"})
	ctx := context.Background()

	plan, err := m.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Pages != 8 || len(plan.Conflicts) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	res, err := m.Apply(ctx, plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	if res.Pages != 8 {
		t.Errorf("result = %+v", res)
	}

	topics, err := svc.TopicMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]knowledge.Block{}
	for _, b := range topics {
		if b.IsTopic() {
			byName[knowledge.TopicName(b.Content)] = b
		}
	}
	if len(byName) != 8 {
		t.Fatalf("topics = %v, want 8", byName)
	}

	sub, err := svc.GetSubtree(ctx, byName["Go"].ID)
	if err != nil {
		t.Fatal(err)
	}
	var contexts knowledge.Block
	for _, b := range sub {
		if strings.HasPrefix(b.Content, "Contexts") {
			contexts = b
		}
		if b.Unreviewed {
			t.Errorf("block %q awaits review; an import is born reviewed", b.Content)
		}
	}
	if contexts.ID == "" || contexts.ParentID == byName["Go"].ID || contexts.ParentID == "" {
		t.Fatalf("Contexts block = %+v, want a grandchild of the Go root", contexts)
	}

	back, err := svc.Backlinks(ctx, contexts.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].TopicID != byName["Refs"].ID {
		t.Fatalf("backlinks to Contexts = %+v, want one from the Refs page", back)
	}
	if !strings.Contains(back[0].Block.Content, "(("+string(contexts.ID)+"|Contexts))") {
		t.Errorf("Refs content = %q, want a resolved block reference", back[0].Block.Content)
	}

	if _, err := m.Apply(ctx, plan.Token); err == nil {
		t.Error("a second apply of a spent preview must fail")
	}
}

func TestImportObsidianVaultSkipsWhatTheBaseAlreadyHas(t *testing.T) {
	svc := openService(t)
	ctx := context.Background()
	if _, err := svc.CreateTopic(ctx, "Notes", ""); err != nil {
		t.Fatal(err)
	}
	m := vaultimport.New(svc, svc, obsidian.Adapter{}, logseq.Adapter{})
	vaultimport.SetPicker(m, fakePicker{answer: "testdata/obsidian"})

	plan, err := m.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, c := range plan.Conflicts {
		kinds[c.Page] = c.Kind
	}
	want := map[string]string{
		"Notes":        vaultimport.ConflictExists,
		"Go":           vaultimport.ConflictDuplicate, // archive/go.md comes first by path
		"Weird [name]": vaultimport.ConflictUnnamed,
	}
	if len(kinds) != len(want) {
		t.Fatalf("conflicts = %v, want %v", kinds, want)
	}
	for page, kind := range want {
		if kinds[page] != kind {
			t.Errorf("conflict for %q = %q, want %q", page, kinds[page], kind)
		}
	}
	if plan.Pages != 2 {
		t.Errorf("Pages = %d, want Home and go", plan.Pages)
	}
	if _, err := m.Apply(ctx, plan.Token); err != nil {
		t.Fatal(err)
	}
	topics, err := svc.TopicMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, b := range topics {
		if b.IsTopic() {
			n++
		}
	}
	if n != 3 {
		t.Errorf("topics = %d, want Notes plus the two imported", n)
	}
}
