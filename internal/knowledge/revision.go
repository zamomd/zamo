package knowledge

import "time"

// Author says who wrote a revision: the human owner or an AI (any source).
type Author string

// The two authors of the MVP.
const (
	AuthorOwner Author = "owner"
	AuthorAI    Author = "ai"
)

// Revision is a full snapshot of a block's state at one change: content,
// placement, and a tombstone flag, plus provenance and the review mark.
// The blocks table is a projection of each block's highest-Seq revision;
// deleting revisions IS the rollback mechanism, so a revision must carry
// everything needed to rebuild the block without consulting neighbours.
type Revision struct {
	ID        ID
	BlockID   ID
	Seq       int // monotonic per block, 1 = creation
	Content   string
	ParentID  ID // empty means topic root
	Position  int
	Mode      Mode // the shape the block had at this change (see Block.Mode)
	Deleted   bool // tombstone: the block is absent from the projection
	Author    Author
	Source    Source
	Origin    string
	Rationale string
	Reviewed  bool
	GroupID   ID // one ApplyOps batch = one group
	CreatedAt time.Time

	// Atomic reports that the revision's group is only true as a whole (see
	// WriteMeta.Atomic), so while it is unreviewed it can be neither decided on
	// nor deleted alone. Derived by storage on read; never written by callers.
	Atomic bool
}

// WriteMeta is the provenance of one ApplyOps batch. Author is a fact about who
// made the change and nothing else is allowed to bend it; whether the batch has
// to be reviewed is BornReviewed's answer.
type WriteMeta struct {
	Author Author
	Source Source
	Title  string // shown in the Changes screen as the batch description
	// Atomic marks a batch whose blocks are only true together — a topic
	// rename and the reference rewrites it implies. Review then takes the
	// whole group or refuses with ErrAtomicGroup: accepting a rewritten
	// [[New name]] while rejecting the rename would leave it resolving to
	// nothing.
	Atomic bool
	// AwaitsReview sends an owner-authored batch to the review queue anyway.
	// A restructuring the owner asked for is still one the owner may want back
	// — and every mechanism that gives a batch back whole (the Changes screen's
	// Reject batch, the atomic-group guards) reads the review state, so a batch
	// born reviewed has no whole-batch revert at all. AI batches always await
	// review and ignore this.
	AwaitsReview bool
}

// BornReviewed reports whether the batch's revisions need no review decision.
// The owner's own writes do not — typing must never queue up work for the
// person who typed it — unless the batch asks for review explicitly.
func (m WriteMeta) BornReviewed() bool {
	return m.Author == AuthorOwner && !m.AwaitsReview
}

// Applied reports what an ApplyOps batch did: the group that ties its
// revisions together and the real ids assigned to temp ids.
type Applied struct {
	GroupID ID
	IDs     map[string]ID
}

// UnreviewedBlock is one entry of the Changes screen: the block's head
// revision (Deleted=true for a pending deletion — the ghost), the topic root
// it lives under, and the id/description of the batch awaiting review.
// GroupID and GroupTitle both come from the block's newest *unreviewed*
// revision, which is not always Head: an owner edit applied straight on top
// of a still-pending AI edit becomes the new (already-reviewed) head while
// the AI revision underneath it is what is actually awaiting review, so
// GroupID can differ from Head.GroupID. "Accept batch" must key off GroupID,
// not Head.GroupID, or it accepts the wrong — already-reviewed — batch.
// Atomic says that group has to be reviewed whole (see WriteMeta.Atomic), so
// the screen offers the batch's controls for this block and not its own.
type UnreviewedBlock struct {
	Head       Revision
	TopicID    ID
	GroupID    ID
	GroupTitle string
	Atomic     bool

	// WrittenOver reports that the batch named by GroupID can no longer be
	// given back: a later change of another batch sits on top of one of the
	// blocks it touched — not necessarily this one, which is why no comparison
	// of this entry's own fields can stand in for it. Undoing the batch would
	// silently change what that later change was made against, so it is
	// refused with ErrGroupWrittenOver: accepting is the only decision left,
	// and a screen offering to reject is offering something that can only
	// fail. Derived by storage on read; never written by callers.
	WrittenOver bool
}

// UnreviewedChanges is everything awaiting review.
type UnreviewedChanges struct {
	Blocks []UnreviewedBlock
}
