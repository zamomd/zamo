package knowledge

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guiEdit is the command the owner types in the app.
func guiEdit(scope ID, instruction string) AIEdit {
	return AIEdit{Scope: scope, Instruction: instruction, Source: SourceGUIEdit}
}

func editFixture() (*fakeLLM, *fakeRepo, *Service) {
	llm := &fakeLLM{editOps: []Op{{Kind: OpEdit, BlockID: "01A", Content: "revised"}}}
	repo := &fakeRepo{subtrees: map[ID][]Block{"01A": {{ID: "01A", Content: "old text"}}}}
	return llm, repo, NewService(llm, repo, seqIDs())
}

func TestUnattendedEditCarriesItsSourceAndRefusesOverUnreviewed(t *testing.T) {
	_, repo, svc := editFixture()

	_, err := svc.EditWithAI(t.Context(), AIEdit{
		Scope: "01A", Instruction: "refresh", Source: "smart_cron", Unattended: true,
	})
	require.NoError(t, err)
	assert.Equal(t, Source("smart_cron"), repo.applyMeta.Source)
	assert.Equal(t, AuthorAI, repo.applyMeta.Author)
	assert.Equal(t, ID("01A"), repo.applyMeta.RefuseOverUnreviewedIn,
		"the write must recheck in its transaction")
}

func TestUnattendedEditStopsBeforeTheModelWhenScopeAwaitsReview(t *testing.T) {
	llm, repo, svc := editFixture()
	repo.unreviewedUnder = 3

	_, err := svc.EditWithAI(t.Context(), AIEdit{
		Scope: "01A", Instruction: "refresh", Source: "smart_cron", Unattended: true,
	})

	scoped, ok := errors.AsType[*UnreviewedInScopeError](err)
	require.True(t, ok, "EditWithAI = %v, want UnreviewedInScopeError", err)
	require.Equal(t, 3, scoped.Count)
	assert.Zero(t, llm.editOpsCalls, "model calls: the call would be wasted")
	assert.Zero(t, repo.applyCalls, "ApplyOps calls")
}

func TestAttendedEditStacksOnUnreviewedAsBefore(t *testing.T) {
	_, repo, svc := editFixture()
	repo.unreviewedUnder = 3

	_, err := svc.EditWithAI(t.Context(), AIEdit{Scope: "01A", Instruction: "revise", Source: SourceGUIEdit})
	require.NoError(t, err, "the owner's own command must apply")
	assert.Empty(t, repo.applyMeta.RefuseOverUnreviewedIn)
}

func TestEditWithAIRequiresASource(t *testing.T) {
	_, _, svc := editFixture()

	_, err := svc.EditWithAI(t.Context(), AIEdit{Scope: "01A", Instruction: "revise"})

	assert.ErrorIs(t, err, ErrInvalidOps)
}
