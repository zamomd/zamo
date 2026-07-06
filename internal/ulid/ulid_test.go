package ulid

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	a := New()
	if len(a) != 26 {
		t.Fatalf("ULID length = %d, want 26: %q", len(a), a)
	}

	seen := make(map[string]bool)
	for range 1000 {
		id := New()
		if seen[id] {
			t.Fatalf("duplicate ULID: %q", id)
		}
		seen[id] = true
	}
}

func TestNewSortsByTime(t *testing.T) {
	early := New()
	time.Sleep(2 * time.Millisecond)
	late := New()
	if early >= late {
		t.Fatalf("ULIDs do not sort by time: %q >= %q", early, late)
	}
}
