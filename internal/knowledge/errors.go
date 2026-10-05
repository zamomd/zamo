package knowledge

import (
	"errors"
	"fmt"
)

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

	// ErrGroupWrittenOver reports a batch that cannot be rolled back because
	// one of its blocks has been written again since. Removing the batch's
	// revisions from under a later one would silently change what that later
	// change was made against, so the batch can only be accepted from here and
	// the blocks edited afterwards. Distinct from ErrInvalidRollback, which is
	// about the tree a rollback would leave, so the user can be told which of
	// the two stopped them.
	ErrGroupWrittenOver = errors.New("batch was written over by a later change")

	// ErrDuplicateTopic reports a write that would give two topic roots the
	// same name. Names are how [[Topic]] references resolve, so they must
	// stay unique.
	ErrDuplicateTopic = errors.New("topic name already taken")

	// ErrUnreferenceableTopic reports a write that would take a topic root to a
	// name no [[Topic]] reference can reach — an empty one, or one carrying a
	// bracket, which cannot be written inside [[...]] at all — while other
	// blocks still reference the name it is dropping. Those references would
	// resolve to nothing, silently. The name by itself is fine: a root nothing
	// points at may carry either and is simply not addressable by name.
	ErrUnreferenceableTopic = errors.New("topic name cannot be referenced")

	// ErrAtomicGroup reports a review decision covering only part of a batch
	// whose blocks are only true together (see WriteMeta.Atomic). Accepting
	// or rejecting it has to cover the whole group.
	ErrAtomicGroup = errors.New("change must be reviewed as one group")

	// ErrBusy reports that the database is locked by another process.
	// The caller retries shortly.
	ErrBusy = errors.New("database is busy")

	// ErrConcurrentWrite reports that a rename was prepared from a block which
	// changed before the rename transaction began.
	ErrConcurrentWrite = errors.New("block changed while preparing the rename")

	// ErrAIDisabled reports an operation rejected by the global AI policy.
	ErrAIDisabled = errors.New("AI features are switched off")

	// ErrLLMUnavailable reports that the model could not be reached:
	// network, API key, timeout.
	ErrLLMUnavailable = errors.New("LLM is unavailable")

	// ErrLLMBadResponse reports that the model answered, but the answer
	// could not be parsed into operations even after a retry.
	ErrLLMBadResponse = errors.New("LLM returned an unparsable response")

	// ErrLLMNoToolSupport reports that the configured model was reached but
	// cannot call tools, which every AI feature depends on.
	ErrLLMNoToolSupport = errors.New("model cannot call tools")

	// ErrDocumentHasChildren reports a write that would leave a block under a
	// topic root in ModeDoc. A document is the whole topic: its text is the
	// page, and a child would be content the reader never sees.
	ErrDocumentHasChildren = errors.New("a document topic cannot have children")

	// ErrUnreviewedInScope reports a batch refused because its scope still
	// holds changes awaiting review (see WriteMeta.RefuseOverUnreviewedIn). It
	// arrives wrapped in an UnreviewedInScopeError carrying how many.
	ErrUnreviewedInScope = errors.New("scope has changes awaiting review")
)

// UnreviewedInScopeError is ErrUnreviewedInScope with the number of blocks
// awaiting review, so a caller can tell the owner how much stands in the way.
type UnreviewedInScopeError struct {
	Scope ID
	Count int
}

func (e *UnreviewedInScopeError) Error() string {
	return fmt.Sprintf("%d changes under %s await review: %v", e.Count, e.Scope, ErrUnreviewedInScope)
}

func (e *UnreviewedInScopeError) Unwrap() error { return ErrUnreviewedInScope }
