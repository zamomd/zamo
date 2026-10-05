package knowledge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOpsReturnsEmptyAppliedWhenNothingChanges(t *testing.T) {
	repo := &fakeRepo{applyNothing: true}
	svc := NewService(repo, func() ID { return "id" })

	applied, err := svc.ApplyOps(t.Context(), ownerMeta(), []Op{
		{Kind: OpEdit, BlockID: "b1", Content: "same"},
	})
	require.NoError(t, err)
	assert.Empty(t, applied.GroupID)
	assert.Empty(t, applied.IDs)
}
