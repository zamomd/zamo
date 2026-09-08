package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// markerSuffix names the file that records a restore waiting for the next
// startup. It sits beside the database so a base and its pending restore
// travel together.
const markerSuffix = ".restore-from"

// Stage accepts an archive now and leaves the base for the next startup.
//
// Replacing the database underneath a process that holds it open corrupts
// it, and the MCP server is a separate process this one cannot stop. Startup
// is the one moment nothing holds the base, so that is where the swap
// happens. The archive is verified here as well, so a file that was never a
// backup is refused while the user is still looking at the dialog rather
// than at the next launch.
func Stage(ctx context.Context, archive, dbPath string) error {
	if _, err := verified(ctx, archive); err != nil {
		return err
	}
	if err := os.WriteFile(dbPath+markerSuffix, []byte(archive), 0o600); err != nil {
		return fmt.Errorf("mark the restore for the next start: %w", err)
	}
	return nil
}

// StagedRestore reports the archive a restore is waiting on, or "" when
// there is none.
func StagedRestore(dbPath string) string {
	b, err := os.ReadFile(dbPath + markerSuffix)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// CancelStaged drops a pending restore, leaving the live base as it is.
func CancelStaged(dbPath string) error {
	if err := os.Remove(dbPath + markerSuffix); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("drop the pending restore: %w", err)
	}
	return nil
}

// ApplyStaged carries out a restore left by Stage and reports where the
// replaced base was kept. It belongs at startup, before anything opens the
// database. An ordinary startup finds no marker and does nothing.
//
// The archive is checked again rather than trusted: it can be edited,
// replaced or deleted between staging and the restart. A failure clears the
// marker, so a bad archive costs one refused startup instead of every
// startup from then on.
func ApplyStaged(ctx context.Context, dbPath string) (safety string, err error) {
	archive := StagedRestore(dbPath)
	if archive == "" {
		return "", nil
	}
	if cerr := CancelStaged(dbPath); cerr != nil {
		return "", cerr
	}
	return Restore(ctx, archive, dbPath)
}
