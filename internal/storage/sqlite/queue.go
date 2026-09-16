package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"crawler/internal/ingest"
)

// ListJobs returns recent jobs, optionally filtered by status.
func (s *Store) ListJobs(ctx context.Context, status ingest.JobStatus, limit int) ([]ingest.Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, kind, payload, status, attempts, available_at, lease_owner, lease_until, last_error FROM jobs`
	arguments := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		arguments = append(arguments, status)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []ingest.Job
	for rows.Next() {
		var job ingest.Job
		var payload string
		var available int64
		var leaseUntil sql.NullInt64
		if err := rows.Scan(&job.ID, &job.Kind, &payload, &job.Status, &job.Attempts, &available,
			&job.LeaseOwner, &leaseUntil, &job.LastError); err != nil {
			return nil, err
		}
		job.Payload = []byte(payload)
		job.AvailableAt = time.UnixMilli(available).UTC()
		if leaseUntil.Valid {
			job.LeaseUntil = time.UnixMilli(leaseUntil.Int64).UTC()
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// RetryFailedJob moves a terminal failed job back to immediately queued state.
func (s *Store) RetryFailedJob(ctx context.Context, jobID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().UnixMilli()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, attempts = 0, available_at = ?, lease_owner = '',
			lease_until = NULL, last_error = '', updated_at = ?
		WHERE id = ? AND status = ?
	`, ingest.JobQueued, now, now, jobID, ingest.JobFailed)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("failed job %q does not exist", jobID)
	}
	var kind ingest.JobKind
	var payload string
	if err = tx.QueryRowContext(ctx, `SELECT kind,payload FROM jobs WHERE id=?`, jobID).Scan(&kind, &payload); err != nil {
		return err
	}
	if kind == ingest.JobPrepareConspectReview {
		var p ingest.ConspectPayload
		if err = json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE conspects SET status='preparing_review' WHERE id=? AND status='failed'`, p.ConspectID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Claim atomically selects one eligible job, increments its attempt count, and
// grants owner an expiring lease.
func (s *Store) Claim(ctx context.Context, owner string, lease time.Duration) (*ingest.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, lease_owner = '', lease_until = NULL, updated_at = ?
		WHERE status = ? AND lease_until IS NOT NULL AND lease_until <= ?
	`, ingest.JobRetry, now.UnixMilli(), ingest.JobRunning, now.UnixMilli()); err != nil {
		return nil, err
	}
	var job ingest.Job
	var payload string
	var available int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, kind, payload, status, attempts, available_at, last_error
		FROM jobs
		WHERE status IN (?, ?) AND available_at <= ?
		ORDER BY available_at, created_at LIMIT 1
	`, ingest.JobQueued, ingest.JobRetry, now.UnixMilli()).Scan(
		&job.ID, &job.Kind, &payload, &job.Status, &job.Attempts, &available, &job.LastError)
	if err == sql.ErrNoRows {
		return nil, ingest.ErrNoJob
	}
	if err != nil {
		return nil, err
	}
	job.Attempts++
	job.Status = ingest.JobRunning
	job.Payload = []byte(payload)
	job.AvailableAt = time.UnixMilli(available).UTC()
	job.LeaseOwner = owner
	job.LeaseUntil = now.Add(lease)
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, attempts = ?, lease_owner = ?, lease_until = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)
	`, job.Status, job.Attempts, owner, job.LeaseUntil.UnixMilli(), now.UnixMilli(), job.ID,
		ingest.JobQueued, ingest.JobRetry)
	if err != nil {
		return nil, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return nil, ingest.ErrNoJob
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &job, nil
}

// Renew extends a running job lease only for its current owner.
func (s *Store) Renew(ctx context.Context, jobID, owner string, lease time.Duration) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET lease_until = ?, updated_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?
	`, now.Add(lease).UnixMilli(), now.UnixMilli(), jobID, ingest.JobRunning, owner)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("job %q lease is no longer owned by %q", jobID, owner)
	}
	return nil
}

// Complete marks a successfully committed job done.
func (s *Store) Complete(ctx context.Context, jobID string) error {
	return s.setJobState(ctx, jobID, ingest.JobDone, time.Time{}, nil)
}

// Retry releases a lease and schedules the job at availableAt with its error.
func (s *Store) Retry(ctx context.Context, jobID string, availableAt time.Time, cause error) error {
	return s.setJobState(ctx, jobID, ingest.JobRetry, availableAt, cause)
}

// Fail moves a job to terminal failed state for manual inspection/retry.
func (s *Store) Fail(ctx context.Context, jobID string, cause error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind ingest.JobKind
	var payload string
	if err = tx.QueryRowContext(ctx, `SELECT kind,payload FROM jobs WHERE id=?`, jobID).Scan(&kind, &payload); err != nil {
		return err
	}
	if kind == ingest.JobPrepareConspectReview {
		var p ingest.ConspectPayload
		if err = json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE conspects SET status='failed' WHERE id=? AND status='preparing_review'`, p.ConspectID); err != nil {
			return err
		}
	}
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status='failed',lease_owner='',lease_until=NULL,last_error=?,updated_at=? WHERE id=?`, message, time.Now().UTC().UnixMilli(), jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) setJobState(ctx context.Context, jobID string, status ingest.JobStatus, availableAt time.Time, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	available := time.Now().UTC().UnixMilli()
	if !availableAt.IsZero() {
		available = availableAt.UnixMilli()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = ?, available_at = ?, lease_owner = '', lease_until = NULL,
		last_error = ?, updated_at = ? WHERE id = ?
	`, status, available, message, time.Now().UTC().UnixMilli(), jobID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Stats counts jobs by active queue state.
func (s *Store) Stats(ctx context.Context) (ingest.QueueStats, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return ingest.QueueStats{}, err
	}
	defer rows.Close()
	var stats ingest.QueueStats
	for rows.Next() {
		var status ingest.JobStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return ingest.QueueStats{}, err
		}
		switch status {
		case ingest.JobQueued:
			stats.Queued = count
		case ingest.JobRunning:
			stats.Running = count
		case ingest.JobRetry:
			stats.Retry = count
		case ingest.JobFailed:
			stats.Failed = count
		}
	}
	return stats, rows.Err()
}
