package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
)

func propsBase(t *testing.T, repo *Repo) {
	t.Helper()
	require.NoError(t, ownerOps(t, repo, "01G1",
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01GO", Content: "Go"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01LANG", Content: "Languages"},
		knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01ZIG", Content: "Zig"},
		knowledge.Op{
			Kind: knowledge.OpAdd, BlockID: "01NOTE", Parent: "01ZIG",
			Content: "Intro.\n\nSimpler than [[Golang]] in places.",
		},
	), "setup")
}

func TestTopicPropsRoundTripInTheOrderGiven(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)
	want := knowledge.TopicProps{Aliases: []string{"Golang", "Go lang"}, Tags: []string{"Draft", "to read"}}
	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO", want))

	got, err := repo.GetTopicProps(t.Context(), "01GO")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO", knowledge.TopicProps{Aliases: []string{"Go lang"}}))
	got, err = repo.GetTopicProps(t.Context(), "01GO")
	require.NoError(t, err)
	assert.Equal(t, knowledge.TopicProps{Aliases: []string{"Go lang"}}, got, "a set replaces")
}

func TestTopicPropsBelongToTopicRootsOnly(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)

	_, err := repo.GetTopicProps(t.Context(), "01NOTE")
	require.ErrorIs(t, err, knowledge.ErrNotFound)
	err = repo.SetTopicProps(t.Context(), "01NOTE", knowledge.TopicProps{Aliases: []string{"x"}})
	require.ErrorIs(t, err, knowledge.ErrNotFound)
}

func TestAliasResolvesAReferenceLikeTheName(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)
	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO", knowledge.TopicProps{Aliases: []string{"Golang"}}))

	links, err := repo.GetBacklinks(t.Context(), "01GO")
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, knowledge.ID("01NOTE"), links[0].Block.ID)
	assert.Equal(t, "Simpler than [[Golang]] in places.", links[0].Excerpt, "the paragraph carrying the alias")

	graph, err := repo.GetTopicGraph(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []knowledge.TopicEdge{{From: "01GO", To: "01ZIG"}}, graph.Edges)
}

func TestAliasMustNotBeAnotherNameOrAlias(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)
	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO", knowledge.TopicProps{Aliases: []string{"Golang"}}))

	err := repo.SetTopicProps(t.Context(), "01ZIG", knowledge.TopicProps{Aliases: []string{"languages"}})
	require.ErrorIs(t, err, knowledge.ErrDuplicateTopic, "another topic's name")
	err = repo.SetTopicProps(t.Context(), "01ZIG", knowledge.TopicProps{Aliases: []string{"GOLANG"}})
	require.ErrorIs(t, err, knowledge.ErrDuplicateTopic, "another topic's alias")

	got, err := repo.GetTopicProps(t.Context(), "01ZIG")
	require.NoError(t, err)
	assert.Empty(t, got.Aliases, "a refused set writes nothing")
}

func TestAliasEqualToTheTopicsOwnNameIsDropped(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)
	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO", knowledge.TopicProps{Aliases: []string{"go", "Golang"}}))

	got, err := repo.GetTopicProps(t.Context(), "01GO")
	require.NoError(t, err)
	assert.Equal(t, []string{"Golang"}, got.Aliases)
}

func TestTopicCannotTakeANameThatIsAnAlias(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)
	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO", knowledge.TopicProps{Aliases: []string{"Golang"}}))

	err := ownerOps(t, repo, "01G2", knowledge.Op{Kind: knowledge.OpAdd, BlockID: "01NEW", Content: "golang"})
	require.ErrorIs(t, err, knowledge.ErrDuplicateTopic, "a new topic")
	err = ownerOps(t, repo, "01G3", knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01ZIG", Content: "Golang"})
	require.ErrorIs(t, err, knowledge.ErrDuplicateTopic, "a rename")
	err = ownerOps(t, repo, "01G4", knowledge.Op{Kind: knowledge.OpEdit, BlockID: "01GO", Content: "Golang"})
	require.NoError(t, err, "a topic may take its own alias for a name")
}

func TestTagIsALabelAndNotAReference(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)
	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO", knowledge.TopicProps{Tags: []string{"Languages", "Draft"}}))
	require.NoError(t, repo.SetTopicProps(t.Context(), "01ZIG", knowledge.TopicProps{Tags: []string{"draft"}}))

	tags, err := repo.ListTopicTags(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []knowledge.TopicTag{
		{TopicID: "01GO", Tag: "Languages"},
		{TopicID: "01GO", Tag: "Draft"},
		{TopicID: "01ZIG", Tag: "draft"},
	}, tags)

	links, err := repo.GetBacklinks(t.Context(), "01LANG")
	require.NoError(t, err)
	assert.Empty(t, links, "a tag spelled like a topic's name does not link to it")
	graph, err := repo.GetTopicGraph(t.Context())
	require.NoError(t, err)
	assert.Empty(t, graph.Edges)
}

func TestPropsOfADeletedTopicResolveNothingAndFreeTheAlias(t *testing.T) {
	repo := testRepo(t)
	propsBase(t, repo)
	require.NoError(t, repo.SetTopicProps(t.Context(), "01GO",
		knowledge.TopicProps{Aliases: []string{"Golang"}, Tags: []string{"Draft"}}))
	require.NoError(t, ownerOps(t, repo, "01G2", knowledge.Op{Kind: knowledge.OpDelete, BlockID: "01GO"}))

	aliases, err := repo.ListTopicAliases(t.Context())
	require.NoError(t, err)
	assert.Empty(t, aliases)
	tags, err := repo.ListTopicTags(t.Context())
	require.NoError(t, err)
	assert.Empty(t, tags)

	require.NoError(t, repo.SetTopicProps(t.Context(), "01ZIG", knowledge.TopicProps{Aliases: []string{"Golang"}}))
	aliases, err = repo.ListTopicAliases(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []knowledge.TopicAlias{{TopicID: "01ZIG", Alias: "Golang"}}, aliases)
}
