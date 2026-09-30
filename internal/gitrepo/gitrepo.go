// Package gitrepo exports the base into a managed directory inside a local
// git repository and commits the change there when it actually changed
// something. It knows nothing about the knowledge base itself — the render
// step is supplied by the caller — and nothing about the Wails application:
// see internal/modules/gitsnapshot for the module that binds this to both.
package gitrepo

// dirNotSetError reports that ExportAndCommit was asked to run before any
// directory was chosen. It carries its own wire code (host.Coded) rather
// than going through wailsbind's central sentinel table, which is reserved
// for domain errors.
type dirNotSetError struct{}

func (dirNotSetError) Error() string { return "no git snapshot directory is set" }
func (dirNotSetError) Code() string  { return "git_snapshot_dir_not_set" }

// ErrDirNotSet reports that ExportAndCommit was asked to run before a
// managed directory was chosen.
var ErrDirNotSet error = dirNotSetError{}
