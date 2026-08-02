package settings

import "sync/atomic"

// Switch is the live AI policy: the value the running process obeys, kept
// apart from the files it was resolved from. Save flips it in the same step
// that swaps the live client, so a GUI change takes effect without a restart,
// and knowledge.Service reads it through Switch.AIEnabled on every write.
type Switch struct {
	on atomic.Bool
}

// NewSwitch starts the switch at the resolved policy.
func NewSwitch(on bool) *Switch {
	s := new(Switch)
	s.on.Store(on)
	return s
}

// Set records a new policy.
func (s *Switch) Set(on bool) { s.on.Store(on) }

// AIEnabled reports the current policy. Its shape is knowledge.AIPolicy, so it
// can be handed over as a method value.
func (s *Switch) AIEnabled() bool { return s.on.Load() }
