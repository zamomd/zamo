package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zamomd/zamo/internal/scheduler"
)

// Schedules implements scheduler.Store on the application database.
type Schedules struct {
	db  *sql.DB
	now func() time.Time
}

var _ scheduler.Store = (*Schedules)(nil)

// NewSchedules returns the schedule store over db. now stamps created_at and
// updated_at; run times always come from the caller.
func NewSchedules(db *sql.DB, now func() time.Time) *Schedules {
	return &Schedules{db: db, now: now}
}

const scheduleColumns = `id, module, key, spec, enabled, payload,
	next_run, last_run, last_outcome, last_detail, claimed_at, claimed_by`

// unixOrNull stores the zero time as NULL, the column's "no such moment".
func unixOrNull(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func timeOf(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.Unix(v.Int64, 0)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSchedule(row rowScanner) (scheduler.Record, error) {
	var r scheduler.Record
	var payload string
	var next, last, claimed sql.NullInt64
	if err := row.Scan(&r.ID, &r.Module, &r.Key, &r.Spec, &r.Enabled, &payload,
		&next, &last, &r.Outcome, &r.Detail, &claimed, &r.ClaimedBy); err != nil {
		return scheduler.Record{}, err
	}
	r.Payload = []byte(payload)
	r.NextRun, r.LastRun, r.ClaimedAt = timeOf(next), timeOf(last), timeOf(claimed)
	return r, nil
}

// Put implements scheduler.Store.
func (s *Schedules) Put(ctx context.Context, rec scheduler.Record) (scheduler.Record, error) {
	now := s.now().Unix()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO schedules (id, module, key, spec, enabled, payload, next_run, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (module, key) DO UPDATE SET
    spec = excluded.spec, enabled = excluded.enabled, payload = excluded.payload,
    next_run = excluded.next_run, updated_at = excluded.updated_at
RETURNING `+scheduleColumns,
		rec.ID, rec.Module, rec.Key, rec.Spec, rec.Enabled, string(rec.Payload),
		unixOrNull(rec.NextRun), now, now)
	got, err := scanSchedule(row)
	if err != nil {
		return scheduler.Record{}, fmt.Errorf("put schedule %s/%s: %w", rec.Module, rec.Key, translate(err))
	}
	return got, nil
}

// Get implements scheduler.Store.
func (s *Schedules) Get(ctx context.Context, module, key string) (scheduler.Record, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+scheduleColumns+` FROM schedules WHERE module = ? AND key = ?`, module, key)
	got, err := scanSchedule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return scheduler.Record{}, fmt.Errorf("schedule %s/%s: %w", module, key, scheduler.ErrNotFound)
	}
	if err != nil {
		return scheduler.Record{}, fmt.Errorf("get schedule %s/%s: %w", module, key, translate(err))
	}
	return got, nil
}

// Delete implements scheduler.Store. Deleting what is not there is not an error.
func (s *Schedules) Delete(ctx context.Context, module, key string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM schedules WHERE module = ? AND key = ?`, module, key); err != nil {
		return fmt.Errorf("delete schedule %s/%s: %w", module, key, translate(err))
	}
	return nil
}

// List implements scheduler.Store.
func (s *Schedules) List(ctx context.Context, module string) ([]scheduler.Record, error) {
	return s.query(ctx, "list schedules", `
SELECT `+scheduleColumns+` FROM schedules
WHERE ? = '' OR module = ? ORDER BY module, created_at, id`, module, module)
}

// Due implements scheduler.Store.
func (s *Schedules) Due(ctx context.Context, now time.Time, lease time.Duration) ([]scheduler.Record, error) {
	return s.query(ctx, "due schedules", `
SELECT `+scheduleColumns+` FROM schedules
WHERE enabled = 1 AND next_run IS NOT NULL AND next_run <= ?
  AND (claimed_at IS NULL OR claimed_at <= ?)
ORDER BY next_run, id`, now.Unix(), now.Add(-lease).Unix())
}

func (s *Schedules) query(ctx context.Context, what, q string, args ...any) ([]scheduler.Record, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, translate(err))
	}
	defer rows.Close()
	var out []scheduler.Record
	for rows.Next() {
		rec, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, translate(err))
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, translate(err))
	}
	return out, nil
}

// Claim implements scheduler.Store. The single UPDATE is the whole protocol:
// SQLite serialises writers, so of two processes racing for one record
// exactly one sees a row affected.
func (s *Schedules) Claim(
	ctx context.Context, id, instance string, now time.Time, lease time.Duration,
) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE schedules SET claimed_at = ?, claimed_by = ?
WHERE id = ? AND (claimed_at IS NULL OR claimed_at <= ?)`,
		now.Unix(), instance, id, now.Add(-lease).Unix())
	if err != nil {
		return false, fmt.Errorf("claim schedule %s: %w", id, translate(err))
	}
	return affected(res)
}

// Finish implements scheduler.Store.
func (s *Schedules) Finish(ctx context.Context, id, instance string, done scheduler.Completion) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE schedules SET last_run = ?, last_outcome = ?, last_detail = ?,
    next_run = CASE WHEN enabled = 1 THEN ? ELSE NULL END,
    claimed_at = NULL, claimed_by = '', updated_at = ?
WHERE id = ? AND claimed_by = ?`,
		done.At.Unix(), done.Outcome, done.Detail, unixOrNull(done.NextRun), s.now().Unix(), id, instance)
	if err != nil {
		return false, fmt.Errorf("finish schedule %s: %w", id, translate(err))
	}
	return affected(res)
}

// Release implements scheduler.Store.
func (s *Schedules) Release(ctx context.Context, id, instance string) error {
	if _, err := s.db.ExecContext(ctx, `
UPDATE schedules SET claimed_at = NULL, claimed_by = '' WHERE id = ? AND claimed_by = ?`,
		id, instance); err != nil {
		return fmt.Errorf("release schedule %s: %w", id, translate(err))
	}
	return nil
}

// Reschedule implements scheduler.Store.
func (s *Schedules) Reschedule(ctx context.Context, id string, next time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
UPDATE schedules SET next_run = ?, updated_at = ? WHERE id = ? AND enabled = 1`,
		unixOrNull(next), s.now().Unix(), id); err != nil {
		return fmt.Errorf("reschedule %s: %w", id, translate(err))
	}
	return nil
}

func affected(res sql.Result) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return false, translate(err)
	}
	return n == 1, nil
}
