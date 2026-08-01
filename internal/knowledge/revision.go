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
	Deleted   bool // tombstone: the block is absent from the projection
	Author    Author
	Source    Source
	Origin    string
	Rationale string
	Reviewed  bool
	GroupID   ID // one ApplyOps batch = one group
	CreatedAt time.Time
}

// WriteMeta is the provenance of one ApplyOps batch. Author decides the
// review state: owner revisions are born reviewed, AI revisions are not.
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
}

// UnreviewedChanges is everything awaiting review.
type UnreviewedChanges struct {
	Blocks []UnreviewedBlock
}
