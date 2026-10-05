package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/tools"
)

// ErrOutOfScope reports a write batch reaching past what its run may change.
var ErrOutOfScope = errors.New("write outside the allowed scope")

// WritePolicy is what one run may change. It is the caller's to set: the
// model is never asked where it may write. The zero policy permits nothing.
type WritePolicy struct {
	// Scope is the block the run may change, with everything under it.
	Scope knowledge.ID
	// AllowNewTopic lets the run create topics of its own.
	AllowNewTopic bool
}

// Permits reports whether any write at all can pass the policy.
func (p WritePolicy) Permits() bool { return p.Scope != "" || p.AllowNewTopic }

// Check refuses a batch with ErrOutOfScope unless every operation stays
// inside the scope subtree or builds a topic the batch itself creates. The
// scope root may be edited, never moved or deleted.
func (p WritePolicy) Check(ctx context.Context, base tools.Reader, ops []knowledge.Op) error {
	allowed := map[string]bool{}
	if p.Scope != "" {
		blocks, err := base.GetSubtree(ctx, p.Scope)
		if err != nil {
			return err
		}
		for _, b := range blocks {
			allowed[string(b.ID)] = true
		}
	}
	for _, op := range ops {
		switch op.Kind {
		case knowledge.OpAdd:
			if op.Parent == "" {
				if !p.AllowNewTopic {
					return fmt.Errorf("%w: this run may not create a new topic", ErrOutOfScope)
				}
			} else if !allowed[op.Parent] {
				return refuse(op.Parent)
			}
			if op.TempID != "" {
				allowed[op.TempID] = true
			}
		case knowledge.OpMove:
			if !allowed[string(op.BlockID)] || op.BlockID == p.Scope || op.Parent == "" {
				return refuse(string(op.BlockID))
			}
			if !allowed[op.Parent] {
				return refuse(op.Parent)
			}
		case knowledge.OpDelete:
			if !allowed[string(op.BlockID)] || op.BlockID == p.Scope {
				return refuse(string(op.BlockID))
			}
		default:
			if !allowed[string(op.BlockID)] {
				return refuse(string(op.BlockID))
			}
		}
	}
	return nil
}

func refuse(block string) error {
	return fmt.Errorf("%w: %s is not yours to change in this run", ErrOutOfScope, block)
}
