package settings

import "sync/atomic"

// Policy stores the process-wide AI permission.
type Policy struct {
	on atomic.Bool
}

// NewPolicy returns a policy initialized to the supplied value.
func NewPolicy(on bool) *Policy {
	p := new(Policy)
	p.on.Store(on)
	return p
}

// Set updates the policy.
func (p *Policy) Set(on bool) { p.on.Store(on) }

// AIEnabled reports whether AI operations are allowed.
func (p *Policy) AIEnabled() bool { return p.on.Load() }
