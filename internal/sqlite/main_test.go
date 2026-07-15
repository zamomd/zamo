package sqlite

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's tests under a goroutine-leak check. The storage
// layer owns the sql.Open connection pool for the modernc.org/sqlite driver;
// a Repo or *sql.DB that is not closed leaves the pool's goroutines running.
// Live-process pprof cannot gate that (no running program under load, and the
// numbers flap by machine); goleak turns the same signal into a deterministic
// pass/fail at the end of the package's tests. Every test here closes its DB
// via t.Cleanup, so a leak reported by goleak points at real code, not fixtures.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
