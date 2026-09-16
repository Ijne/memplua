package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"crawler/internal/app"
	"crawler/internal/ingest"
	"crawler/internal/observability"
)

// QueueStats implements app.RuntimeRepository using durable job counts.
func (s *Store) QueueStats(ctx context.Context) (ingest.QueueStats, error) {
	return s.Stats(ctx)
}

// StartAppRun recovers interrupted sessions/batches/leases, then records the new
// application run in one transaction.
func (s *Store) StartAppRun(ctx context.Context, run app.AppRun) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().UnixMilli()
	_, err = tx.ExecContext(ctx, `
		UPDATE app_runs SET state = ?, stopped_at = ?, last_error = ?
		WHERE state IN (?, ?, ?, ?)
	`, app.StateStopped, now, "application exited without a clean shutdown",
		app.StateStarting, app.StateRunning, app.StatePaused, app.StateDegraded)
	if err != nil {
		return fmt.Errorf("close stale app runs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE source_sessions SET state = ?, stopped_at = ?, last_error = ?
		WHERE state IN (?, ?)
	`, app.SourceFailed, now, "application exited without a clean shutdown", app.SourceStarting, app.SourceRunning); err != nil {
		return fmt.Errorf("close stale source sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, available_at = ?, lease_owner = '', lease_until = NULL,
			last_error = 'worker exited before completing the job', updated_at = ?
		WHERE status = ?
	`, ingest.JobRetry, now, now, ingest.JobRunning); err != nil {
		return fmt.Errorf("release stale jobs: %w", err)
	}
	// A local model can be healthy again after an application restart. Resume
	// extraction jobs that failed for transient transport or timeout reasons;
	// structural/model-output failures remain failed for explicit intervention.
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, attempts = 0, available_at = ?, lease_owner = '',
			lease_until = NULL, last_error = '', updated_at = ?
		WHERE status = ? AND kind = ? AND (
			last_error LIKE '%context deadline exceeded%' OR
			last_error LIKE '%timed out%' OR
			last_error LIKE '%connection was forcibly closed%' OR
			last_error LIKE '%connection refused%' OR
			last_error LIKE '%actively refused%'
		)
	`, ingest.JobQueued, now, now, ingest.JobFailed, ingest.JobExtractAnalysisBatch); err != nil {
		return fmt.Errorf("recover transient extraction jobs: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO app_runs(id, version, state, started_at) VALUES (?, ?, ?, ?)`,
		run.ID, run.Version, run.State, run.StartedAt.UnixMilli())
	if err != nil {
		return err
	}
	if err = s.sweepBatches(ctx, tx, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishAppRun records final state, stop time, and a diagnostic error string.
func (s *Store) FinishAppRun(ctx context.Context, id string, state app.State, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE app_runs SET state = ?, stopped_at = ?, last_error = ? WHERE id = ?`,
		state, time.Now().UTC().UnixMilli(), message, id)
	return err
}

// SaveSourceSession persists a newly created source session.
func (s *Store) SaveSourceSession(ctx context.Context, session app.SourceSession) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO source_sessions(id, source_id, kind, state, started_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?)
	`, session.ID, session.SourceID, session.Kind, session.State, session.StartedAt.UnixMilli(), session.LastError)
	return err
}

// UpdateSourceSession persists a lifecycle transition and optional failure.
func (s *Store) UpdateSourceSession(ctx context.Context, id string, state app.SourceState, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	var stoppedAt any
	if state == app.SourceStopped || state == app.SourceFailed || state == app.SourcePaused {
		stoppedAt = time.Now().UTC().UnixMilli()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE source_sessions SET state = ?, stopped_at = ?, last_error = ? WHERE id = ?`, state, stoppedAt, message, id); err != nil {
		return err
	}
	if stoppedAt != nil {
		if err = closeSessionBatch(ctx, tx, id, time.Now().UTC(), "source_stopped"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetSourceSession loads one persisted source session.
func (s *Store) GetSourceSession(ctx context.Context, id string) (*app.SourceSession, error) {
	var session app.SourceSession
	var started int64
	var stopped sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source_id, kind, state, started_at, stopped_at, last_error
		FROM source_sessions WHERE id = ?
	`, id).Scan(&session.ID, &session.SourceID, &session.Kind, &session.State, &started, &stopped, &session.LastError)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session.StartedAt = time.UnixMilli(started).UTC()
	session.StoppedAt = nullableTime(stopped)
	return &session, nil
}

// ListSourceSessions returns persisted source sessions in chronological order.
func (s *Store) ListSourceSessions(ctx context.Context) ([]app.SourceSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, kind, state, started_at, stopped_at, last_error
		FROM source_sessions ORDER BY started_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []app.SourceSession
	for rows.Next() {
		var session app.SourceSession
		var started int64
		var stopped sql.NullInt64
		if err := rows.Scan(&session.ID, &session.SourceID, &session.Kind, &session.State, &started, &stopped, &session.LastError); err != nil {
			return nil, err
		}
		session.StartedAt = time.UnixMilli(started).UTC()
		session.StoppedAt = nullableTime(stopped)
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// SaveEvent durably inserts one user event before live delivery.
func (s *Store) SaveEvent(ctx context.Context, event observability.Event) error {
	data, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_events(id, occurred_at, severity, type, message, resource_kind, resource_id, data_json, is_read)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.Time.UnixMilli(), event.Severity, event.Type, event.Message,
		event.ResourceKind, event.ResourceID, string(data), event.Read)
	return err
}

// ListEvents replays events after a stable SSE cursor.
func (s *Store) ListEvents(ctx context.Context, afterID string, limit int) ([]observability.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, occurred_at, severity, type, message, resource_kind, resource_id, data_json, is_read FROM user_events`
	args := []any{}
	if afterID != "" {
		query += ` WHERE rowid > COALESCE((SELECT rowid FROM user_events WHERE id = ?), 0)`
		args = append(args, afterID)
	}
	query += ` ORDER BY rowid LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []observability.Event
	for rows.Next() {
		var event observability.Event
		var occurred int64
		var data string
		if err := rows.Scan(&event.ID, &occurred, &event.Severity, &event.Type, &event.Message,
			&event.ResourceKind, &event.ResourceID, &data, &event.Read); err != nil {
			return nil, err
		}
		event.Time = time.UnixMilli(occurred).UTC()
		if err := json.Unmarshal([]byte(data), &event.Data); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// MarkEventRead updates the durable notification read flag.
func (s *Store) MarkEventRead(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE user_events SET is_read = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecentWarnings returns newest unread warning/error events for compact UI state.
func (s *Store) RecentWarnings(ctx context.Context, limit int) ([]observability.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,occurred_at,severity,type,is_read FROM user_events WHERE severity IN ('warning','error') AND is_read=0 ORDER BY rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []observability.Event{}
	for rows.Next() {
		var event observability.Event
		var occurred int64
		if err = rows.Scan(&event.ID, &occurred, &event.Severity, &event.Type, &event.Read); err != nil {
			return nil, err
		}
		event.Time = time.UnixMilli(occurred).UTC()
		out = append(out, event)
	}
	// Keep chronological ordering consistent with History for consumers.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
