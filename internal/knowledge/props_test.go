package knowledge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func propsService(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	repo := &fakeRepo{
		topics: []Block{
			{ID: "go", Content: "Go"},
			{ID: "lang", Content: "Languages"},
			{ID: "child", ParentID: "go", Content: "Languages of old"},
		},
		aliases: []TopicAlias{{TopicID: "lang", Alias: "PL"}},
	}
	return NewService(repo, func() ID { return "new-1" }), repo
}

func TestSetTopicPropsKeepsEachAliasOnceAsWritten(t *testing.T) {
	svc, repo := propsService(t)

	got, err := svc.SetTopicProps(t.Context(), "go", []string{" Golang ", "", "golang", "Go lang"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"Golang", "Go lang"}, got.Aliases)
	assert.Equal(t, []string{"Golang", "Go lang"}, repo.props["go"].Aliases)
}

func TestSetTopicPropsRefusesAnAliasNoReferenceCanCarry(t *testing.T) {
	svc, repo := propsService(t)

	_, err := svc.SetTopicProps(t.Context(), "go", []string{"a [b]"}, nil)
	require.ErrorIs(t, err, ErrUnreferenceableTopic)
	assert.Empty(t, repo.props, "refused before any write")
}

func TestSetTopicPropsKeepsEachTagOnceWithoutItsHash(t *testing.T) {
	svc, repo := propsService(t)

	got, err := svc.SetTopicProps(t.Context(), "go", nil, []string{"#Draft", " draft ", "", "#", "to   read"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Draft", "to read"}, got.Tags)
	assert.Zero(t, repo.applyCalls, "a tag is a label: it creates nothing")
}

func TestTagKeyFoldsTheSpellingsOfOneTag(t *testing.T) {
	assert.Equal(t, "to read", TagKey("  #To   Read "))
	assert.Empty(t, TagKey(" # "))
}

func TestSetTopicPropsPassesTheRefusalOfStorageOn(t *testing.T) {
	svc, repo := propsService(t)
	repo.setPropsErr = ErrDuplicateTopic

	_, err := svc.SetTopicProps(t.Context(), "go", []string{"Languages"}, nil)
	require.ErrorIs(t, err, ErrDuplicateTopic)
}
