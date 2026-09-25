package ulid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	a := New()
	require.Len(t, a, 26)

	seen := make(map[string]bool)
	for range 1000 {
		id := New()
		require.False(t, seen[id], "duplicate ULID: %q", id)
		seen[id] = true
	}
}

func TestNewSortsByTime(t *testing.T) {
	early := New()
	time.Sleep(2 * time.Millisecond)
	late := New()
	require.Less(t, early, late, "ULIDs do not sort by time")
}
