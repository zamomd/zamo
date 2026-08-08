package knowledge

// Mode is the shape a topic takes: a tree of blocks, or a single block holding
// a whole markdown document. It is a property of a topic root and of nothing
// else; no other state is derived from it.
type Mode string

// The two shapes a topic can have.
const (
	ModeOutline Mode = "outline"
	ModeDoc     Mode = "doc"
)

// valid reports whether m is a mode that can be stored. The empty mode is not:
// it means "unchanged" on the way in and is resolved before any write.
func (m Mode) valid() bool { return m == ModeOutline || m == ModeDoc }
