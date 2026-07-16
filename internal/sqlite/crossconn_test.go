package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// TestPendingVisibleAcrossConnections pins the invariant the live "new
// proposals" badge (#15) depends on: a proposal committed by one process (the
// comind mcp server) is visible to a plain SELECT from another process (the
// GUI) that opened the same database file, with no reopen. Two independent
// *sql.DB handles stand in for the two processes.
//
// This covers visibility after commit, which is plain SQLite behaviour and not
// a property of the journal mode: the test stays green with journal_mode(WAL)
// dropped from Open's DSN. It is not a WAL guard, and the concurrency WAL
// actually buys (a reader never blocking the writer) is not pinned anywhere.
func TestPendingVisibleAcrossConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")

	writer, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open writer: %v", err)
	}
	t.Cleanup(func() { writer.Close() })

	reader, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open reader: %v", err)
	}
	t.Cleanup(func() { reader.Close() })

	before, err := r(reader).ListProposals(t.Context(), knowledge.StatusPending)
	if err != nil {
		t.Fatalf("ListProposals before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("reader saw %d pending before any write, want 0", len(before))
	}

	// A separate connection commits a pending proposal, as the MCP server would.
	savePending(t, r(writer), "p-mcp", knowledge.SourceMCP)

	// The reader's next query — exactly what the badge poll issues — must surface
	// it without reopening the database.
	after, err := r(reader).ListProposals(t.Context(), knowledge.StatusPending)
	if err != nil {
		t.Fatalf("ListProposals after: %v", err)
	}
	if len(after) != 1 || after[0].ID != "p-mcp" {
		t.Fatalf("reader saw %v after cross-connection write, want one [p-mcp]", after)
	}
}
