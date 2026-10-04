package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/adhocore/gronx"
)

// schedule answers when a cron expression fires next. It is the only place
// that knows the cron library, so the dialect Preview shows is by construction
// the dialect the loop runs.
type schedule struct {
	expr string
}

// parse accepts a standard five-field expression or a descriptor such as
// @daily. The library's wider forms, with a seconds or a year field, are
// refused: a schedule here is an LLM call, and one per second is never meant.
// An expression that is well formed but never fires, such as the 30th of
// February, is refused too: it would sit in the list looking alive.
func parse(spec string) (schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return schedule{}, fmt.Errorf("empty cron expression")
	}
	if !strings.HasPrefix(spec, "@") && len(strings.Fields(spec)) != 5 {
		return schedule{}, fmt.Errorf("cron expression %q must have five fields", spec)
	}
	if !gronx.IsValid(spec) {
		return schedule{}, fmt.Errorf("cron expression %q is not valid", spec)
	}
	s := schedule{expr: spec}
	if s.next(time.Now()).IsZero() {
		return schedule{}, fmt.Errorf("cron expression %q never fires", spec)
	}
	return s, nil
}

// next returns the first firing time strictly after t, in t's zone, or the
// zero time when the expression never fires.
func (s schedule) next(t time.Time) time.Time {
	next, err := gronx.NextTickAfter(s.expr, t, false)
	if err != nil {
		return time.Time{}
	}
	return next
}
