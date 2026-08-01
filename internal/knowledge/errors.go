package knowledge

import "errors"

// The error vocabulary of the domain.
var (
	// ErrNotFound reports that a referenced block does not exist.
	ErrNotFound = errors.New("not found")

	// ErrCycle reports a move operation that would place a block inside
	// its own subtree, collapsing the tree into a ring.
	ErrCycle = errors.New("move would create a cycle")

	// ErrInvalidOps reports a batch whose shape is broken: an unknown op
	// kind, an op missing a required field, or duplicate temp ids.
	// Malformed input from an untrusted source, refused before any write.
	ErrInvalidOps = errors.New("operations are malformed")

	// ErrEmptyOps reports a write batch with no operations.
	ErrEmptyOps = errors.New("no operations")

	// ErrInvalidRollback reports a revision deletion that would leave the
	// tree inconsistent: a block whose parent no longer exists, or a
	// removal that would orphan live children.
	ErrInvalidRollback = errors.New("rollback would leave an inconsistent tree")

	// ErrDuplicateTopic reports a write that would give two topic roots the
	// same name. Names are how [[Topic]] references resolve, so they must
	// stay unique.
	ErrDuplicateTopic = errors.New("topic name already taken")

	// ErrUnreferenceableTopic reports a write that would leave a topic root
	// with a name no [[Topic]] reference can reach: one carrying a bracket,
	// which cannot be written inside [[...]] at all, or none at all while
	// other blocks still reference the name being dropped. Either way the
	// references that pointed at it would resolve to nothing, silently.
	ErrUnreferenceableTopic = errors.New("topic name cannot be referenced")

	// ErrBusy reports that the database is locked by another process.
	// The caller retries shortly.
	ErrBusy = errors.New("database is busy")

	// ErrLLMUnavailable reports that the model could not be reached:
	// network, API key, timeout.
	ErrLLMUnavailable = errors.New("LLM is unavailable")

	// ErrLLMBadResponse reports that the model answered, but the answer
	// could not be parsed into operations even after a retry.
	ErrLLMBadResponse = errors.New("LLM returned an unparsable response")
)
