package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err, "load Europe/Berlin")
	return loc
}

func TestParseAcceptsStandardExpressionsAndDescriptors(t *testing.T) {
	for _, spec := range []string{"0 9 * * 1", "*/15 * * * *", "30 2 1 * *", "@daily", "@hourly", "@monthly"} {
		_, err := parse(spec)
		assert.NoError(t, err, "parse(%q)", spec)
	}
}

func TestParseRejectsWhatCannotRun(t *testing.T) {
	for _, spec := range []string{
		"", "   ", "every monday", "61 * * * *", "* * *", "0 0 30 2 *", "* * * * * *", "0 0 9 * * * 2030",
	} {
		_, err := parse(spec)
		assert.Error(t, err, "parse(%q)", spec)
	}
}

func TestNextIsStrictlyAfter(t *testing.T) {
	s, err := parse("0 9 * * *")
	require.NoError(t, err)
	at := time.Date(2026, 9, 21, 9, 0, 0, 0, berlin(t))

	got := s.next(at)

	want := time.Date(2026, 9, 22, 9, 0, 0, 0, berlin(t))
	assert.WithinDuration(t, want, got, 0, "next(%v): a slot that is now is not the next one", at)
}

func TestNextFollowsTheZoneOfItsArgument(t *testing.T) {
	s, err := parse("0 9 * * *")
	require.NoError(t, err)
	utcEvening := time.Date(2026, 9, 21, 20, 0, 0, 0, time.UTC)

	inBerlin := s.next(utcEvening.In(berlin(t)))

	assert.Equal(t, 9, inBerlin.Hour(), "next = %v", inBerlin)
	assert.Equal(t, "Europe/Berlin", inBerlin.Location().String())
}

// Spring forward in Berlin: 29 Mar 2026, 02:00 becomes 03:00, so 02:30 does
// not exist that night.
func TestNextAcrossTheSpringGapIsARealLaterInstant(t *testing.T) {
	s, err := parse("30 2 * * *")
	require.NoError(t, err)
	before := time.Date(2026, 3, 28, 12, 0, 0, 0, berlin(t))

	first := s.next(before)
	second := s.next(first)

	require.True(t, first.After(before) && second.After(first),
		"next chain %v -> %v -> %v is not strictly increasing", before, first, second)
	assert.LessOrEqual(t, second.Sub(before), 72*time.Hour, "the gap night must cost at most one day")
}

// Fall back in Berlin: 25 Oct 2026, 03:00 becomes 02:00, so 02:30 happens
// twice that night.
func TestNextAcrossTheAutumnRepeatNeverGoesBackwards(t *testing.T) {
	s, err := parse("30 2 * * *")
	require.NoError(t, err)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, berlin(t))

	for range 4 {
		next := s.next(at)
		require.True(t, next.After(at), "next(%v) = %v, want strictly later", at, next)
		at = next
	}
	assert.False(t, at.Before(time.Date(2026, 10, 27, 0, 0, 0, 0, berlin(t))),
		"four slots ended at %v, want at most one extra run on the repeat night", at)
}
