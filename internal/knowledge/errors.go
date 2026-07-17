package knowledge

import "errors"

// The error vocabulary of the domain.
var (
	// ErrNotFound reports that a referenced block or proposal
	// does not exist.
	ErrNotFound = errors.New("not found")

	// ErrStale reports that a proposal references blocks that changed or
	// disappeared after the review screen was rendered. The GUI reacts by
	// re-reading the proposal and re-rendering the review.
	ErrStale = errors.New("proposal is stale")

	// ErrCycle reports a move operation that would place a block inside
	// its own subtree, collapsing the tree into a ring.
	ErrCycle = errors.New("move would create a cycle")

	// ErrNotPending reports an attempt to apply or reject a proposal
	// that has already been decided.
	ErrNotPending = errors.New("proposal already decided")

	// ErrInvalidProposal reports a proposal whose shape is broken: an empty
	// title, an unknown op kind, or an op missing a required field. Malformed
	// input from an untrusted source, refused before it reaches the queue.
	ErrInvalidProposal = errors.New("proposal is malformed")

	// ErrEmptyProposal reports an attempt to create a proposal that carries
	// no operations: a proposal that changes nothing is not a proposal.
	ErrEmptyProposal = errors.New("proposal has no operations")

	// ErrBusy reports that the database is locked by another process.
	// The caller retries shortly.
	ErrBusy = errors.New("database is busy")

	// ErrReferenced reports that an operation would leave a dangling reference:
	// a block cannot be removed while an edge still points at it. A foreign-key
	// violation surfaced as a domain error rather than a raw driver one.
	ErrReferenced = errors.New("block is still referenced")

	// ErrLLMUnavailable reports that the model could not be reached:
	// network, API key, timeout.
	ErrLLMUnavailable = errors.New("LLM is unavailable")

	// ErrLLMBadResponse reports that the model answered, but the answer
	// could not be parsed into operations even after a retry.
	ErrLLMBadResponse = errors.New("LLM returned an unparsable response")
)
