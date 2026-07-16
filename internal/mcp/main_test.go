package mcp

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's tests under a goroutine-leak check. The server and
// its in-memory client transport (NewInMemoryTransports) run background
// goroutines per connection; a session that is not closed leaks them. Every test
// connects through the connect helper, which closes both sessions via t.Cleanup,
// so a leak reported here points at a real missing shutdown, not a fixture.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
