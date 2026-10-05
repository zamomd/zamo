package vaultimport_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/modules/vaultimport"
	"github.com/zamomd/zamo/internal/modules/vaultimport/logseq"
	"github.com/zamomd/zamo/internal/modules/vaultimport/obsidian"
	"github.com/zamomd/zamo/internal/sqlite"
	"github.com/zamomd/zamo/internal/ulid"
)

// openService assembles the domain over a fresh sqlite base, as main.go
// does, so the module is exercised against the real write path. Tests may
// import storage; the module itself may not.
func openService(t *testing.T) *knowledge.Service {
	t.Helper()
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "zamo.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }
	return knowledge.NewService(sqlite.NewRepo(db, time.Now, newID), newID)
}

func TestImportLogseqGraphEndToEnd(t *testing.T) {
	svc := openService(t)
	m := vaultimport.New(svc, svc, obsidian.Adapter{}, logseq.Adapter{})
	vaultimport.SetPicker(m, fakePicker{answer: "testdata/logseq"})
	ctx := context.Background()

	plan, err := m.Preview(ctx)
	require.NoError(t, err)
	require.Equal(t, 8, plan.Pages)
	require.Empty(t, plan.Conflicts)
	res, err := m.Apply(ctx, plan.Token)
	require.NoError(t, err)
	assert.Equal(t, 8, res.Pages)

	topics, err := svc.TopicMap(ctx)
	require.NoError(t, err)
	byName := map[string]knowledge.Block{}
	for _, b := range topics {
		if b.IsTopic() {
			byName[knowledge.TopicName(b.Content)] = b
		}
	}
	require.Len(t, byName, 8)

	sub, err := svc.GetSubtree(ctx, byName["Go"].ID)
	require.NoError(t, err)
	var contexts knowledge.Block
	for _, b := range sub {
		if strings.HasPrefix(b.Content, "Contexts") {
			contexts = b
		}
		assert.False(t, b.Unreviewed, "block %q awaits review; an import is born reviewed", b.Content)
	}
	require.NotEmpty(t, contexts.ID, "want a Contexts block")
	require.NotEmpty(t, contexts.ParentID, "want a grandchild of the Go root")
	require.NotEqual(t, byName["Go"].ID, contexts.ParentID, "want a grandchild of the Go root")

	back, err := svc.Backlinks(ctx, contexts.ID)
	require.NoError(t, err)
	require.Len(t, back, 1)
	require.Equal(t, byName["Refs"].ID, back[0].TopicID, "want the backlink from the Refs page")
	assert.Contains(t, back[0].Block.Content, "(("+string(contexts.ID)+"|Contexts))", "want a resolved block reference")

	_, err = m.Apply(ctx, plan.Token)
	assert.Error(t, err, "a second apply of a spent preview must fail")
}

func TestImportObsidianVaultSkipsWhatTheBaseAlreadyHas(t *testing.T) {
	svc := openService(t)
	ctx := context.Background()
	_, err := svc.CreateTopic(ctx, "Notes", "")
	require.NoError(t, err)
	m := vaultimport.New(svc, svc, obsidian.Adapter{}, logseq.Adapter{})
	vaultimport.SetPicker(m, fakePicker{answer: "testdata/obsidian"})

	plan, err := m.Preview(ctx)
	require.NoError(t, err)
	kinds := map[string]string{}
	for _, c := range plan.Conflicts {
		kinds[c.Page] = c.Kind
	}
	want := map[string]string{
		"Notes":        vaultimport.ConflictExists,
		"Go":           vaultimport.ConflictDuplicate, // archive/go.md comes first by path
		"Weird [name]": vaultimport.ConflictUnnamed,
	}
	require.Len(t, kinds, len(want), "conflicts = %v, want %v", kinds, want)
	for page, kind := range want {
		assert.Equal(t, kind, kinds[page], "conflict for %q", page)
	}
	assert.Equal(t, 2, plan.Pages, "want Home and go")
	_, err = m.Apply(ctx, plan.Token)
	require.NoError(t, err)
	topics, err := svc.TopicMap(ctx)
	require.NoError(t, err)
	n := 0
	for _, b := range topics {
		if b.IsTopic() {
			n++
		}
	}
	assert.Equal(t, 3, n, "want Notes plus the two imported")
}
