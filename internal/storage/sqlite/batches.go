package sqlite

import (
	"context"
	"crawler/internal/app"
	"crawler/internal/id"
	"crawler/internal/ingest"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SubmitText creates a synthetic source session and durable chunk, then
// finalizes its batch so manual input follows the normal extraction pipeline.
func (s *Store) SubmitText(ctx context.Context, text string) (ingest.TextSubmission, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ingest.TextSubmission{}, errors.New("text is required")
	}
	now := time.Now().UTC()
	submission := ingest.TextSubmission{SourceSessionID: id.New(), ChunkID: id.New(), CreatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return submission, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO source_sessions(id,source_id,kind,state,started_at,stopped_at) VALUES (?,'manual-text','text/manual',?,?,?)`, submission.SourceSessionID, app.SourceStopped, now.UnixMilli(), now.UnixMilli()); err != nil {
		return submission, err
	}
	chunk := ingest.Chunk{ID: submission.ChunkID, SourceSessionID: submission.SourceSessionID, CapturedAt: now, Text: text, Status: ingest.ChunkReady}
	if err = s.saveChunk(ctx, tx, chunk, now); err != nil {
		return submission, err
	}
	if err = closeSessionBatch(ctx, tx, submission.SourceSessionID, now, "finalize"); err != nil {
		return submission, err
	}
	return submission, tx.Commit()
}

// SaveChunk atomically persists a source-ordered chunk and attaches it to the
// current collecting batch, closing that batch when policy requires.
func (s *Store) SaveChunk(ctx context.Context, chunk ingest.Chunk) error {
	if strings.TrimSpace(chunk.Text) == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.saveChunk(ctx, tx, chunk, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) saveChunk(ctx context.Context, tx *sql.Tx, chunk ingest.Chunk, now time.Time) error {
	if chunk.ID == "" {
		chunk.ID = id.New()
	}
	if chunk.CapturedAt.IsZero() {
		chunk.CapturedAt = now
	}
	if chunk.Status == "" {
		chunk.Status = ingest.ChunkReady
	}
	var existingSource, existingText string
	err := tx.QueryRowContext(ctx, `SELECT source_session_id,text FROM chunks WHERE id=?`, chunk.ID).Scan(&existingSource, &existingText)
	if err == nil {
		if existingSource != chunk.SourceSessionID || existingText != chunk.Text {
			return errors.New("chunk ID already belongs to different content")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if chunk.Sequence == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT next_sequence FROM source_sessions WHERE id=?`, chunk.SourceSessionID).Scan(&chunk.Sequence); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chunks(id,source_session_id,sequence,captured_at,text,status) VALUES (?,?,?,?,?,?)`, chunk.ID, chunk.SourceSessionID, chunk.Sequence, chunk.CapturedAt.UnixMilli(), chunk.Text, chunk.Status); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE source_sessions SET next_sequence=MAX(next_sequence,?) WHERE id=?`, chunk.Sequence+1, chunk.SourceSessionID); err != nil {
		return err
	}
	var batchID string
	var started, last int64
	err = tx.QueryRowContext(ctx, `SELECT id,started_at,last_text_at FROM analysis_batches WHERE source_session_id=? AND status='collecting'`, chunk.SourceSessionID).Scan(&batchID, &started, &last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if batchID != "" {
		reason := batchCloseReason(now, started, last, s.batchPolicy)
		if reason != "" {
			if err = closeBatch(ctx, tx, batchID, now, reason); err != nil {
				return err
			}
			batchID = ""
		}
	}
	if batchID == "" {
		batchID = id.New()
		if _, err = tx.ExecContext(ctx, `INSERT INTO analysis_batches(id,source_session_id,status,started_at,last_text_at) VALUES (?,?,'collecting',?,?)`, batchID, chunk.SourceSessionID, now.UnixMilli(), now.UnixMilli()); err != nil {
			return err
		}
		if err = copyContextTail(ctx, tx, batchID, chunk.SourceSessionID, s.batchPolicy.ContextTailChars); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO analysis_batch_chunks(batch_id,chunk_id,role,position,text) VALUES (?,?,'focus',?,?)`, batchID, chunk.ID, chunk.Sequence, chunk.Text); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE analysis_batches SET last_text_at=? WHERE id=?`, now.UnixMilli(), batchID)
	return err
}

func batchCloseReason(now time.Time, started, last int64, policy ingest.BatchPolicy) string {
	if now.UnixMilli()-started >= policy.MaxDuration.Milliseconds() {
		return "deadline"
	}
	if now.UnixMilli()-last >= policy.IdleTimeout.Milliseconds() {
		return "idle"
	}
	return ""
}

func copyContextTail(ctx context.Context, tx *sql.Tx, batchID, sessionID string, limit int) error {
	if limit <= 0 {
		return nil
	}
	// Only the immediately preceding batch supplies context, even if extraction is still queued.
	rows, err := tx.QueryContext(ctx, `SELECT bc.chunk_id,bc.position,bc.text FROM analysis_batch_chunks bc WHERE bc.role='focus' AND bc.batch_id=(SELECT id FROM analysis_batches WHERE source_session_id=? AND id<>? ORDER BY rowid DESC LIMIT 1) ORDER BY bc.position DESC`, sessionID, batchID)
	if err != nil {
		return err
	}
	type fragment struct {
		id       string
		position int64
		text     string
	}
	var tail []fragment
	for limit > 0 && rows.Next() {
		var f fragment
		if err = rows.Scan(&f.id, &f.position, &f.text); err != nil {
			rows.Close()
			return err
		}
		chars := []rune(f.text)
		if len(chars) > limit {
			chars = chars[len(chars)-limit:]
		}
		f.text = string(chars)
		limit -= len(chars)
		tail = append(tail, f)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, f := range tail {
		if _, err = tx.ExecContext(ctx, `INSERT INTO analysis_batch_chunks(batch_id,chunk_id,role,position,text) VALUES (?,?,'context',?,?)`, batchID, f.id, f.position, f.text); err != nil {
			return err
		}
	}
	return nil
}

func closeBatch(ctx context.Context, tx *sql.Tx, batchID string, now time.Time, reason string) error {
	result, err := tx.ExecContext(ctx, `UPDATE analysis_batches SET status='ready',closed_at=?,close_reason=? WHERE id=? AND status='collecting'`, now.UnixMilli(), reason, batchID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	if err = insertJobValue(ctx, tx, ingest.JobExtractAnalysisBatch, ingest.BatchPayload{AnalysisBatchID: batchID}); err != nil {
		return err
	}
	return insertDomainEvent(ctx, tx, "analysis_batch.closed", "Analysis batch closed", "analysis_batch", batchID)
}

func closeSessionBatch(ctx context.Context, tx *sql.Tx, sessionID string, now time.Time, reason string) error {
	var batchID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM analysis_batches WHERE source_session_id=? AND status='collecting'`, sessionID).Scan(&batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return closeBatch(ctx, tx, batchID, now, reason)
}

// FinalizeSource idempotently closes a session's collecting batch and queues
// extraction when the batch contains focus text.
func (s *Store) FinalizeSource(ctx context.Context, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM source_sessions WHERE id=?`, sessionID).Scan(&exists); err != nil {
		return err
	}
	if err = closeSessionBatch(ctx, tx, sessionID, time.Now().UTC(), "finalize"); err != nil {
		return err
	}
	return tx.Commit()
}

// SweepBatches closes collecting batches past their duration or idle deadline.
func (s *Store) SweepBatches(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.sweepBatches(ctx, tx, now); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) sweepBatches(ctx context.Context, tx *sql.Tx, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT b.id,b.started_at,b.last_text_at,s.state FROM analysis_batches b JOIN source_sessions s ON s.id=b.source_session_id WHERE b.status='collecting'`)
	if err != nil {
		return err
	}
	type closing struct{ id, reason string }
	var batches []closing
	for rows.Next() {
		var batchID, state string
		var started, last int64
		if err = rows.Scan(&batchID, &started, &last, &state); err != nil {
			rows.Close()
			return err
		}
		reason := batchCloseReason(now, started, last, s.batchPolicy)
		if state == string(app.SourceStopped) || state == string(app.SourceFailed) || state == string(app.SourcePaused) {
			reason = "source_stopped"
		}
		if reason != "" {
			batches = append(batches, closing{batchID, reason})
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, b := range batches {
		if err = closeBatch(ctx, tx, b.id, now, b.reason); err != nil {
			return err
		}
	}
	return nil
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertJob(ctx context.Context, executor sqlExecutor, kind ingest.JobKind, payload []byte) error {
	now := time.Now().UTC().UnixMilli()
	_, err := executor.ExecContext(ctx, `INSERT INTO jobs(id,kind,payload,status,available_at,created_at,updated_at) VALUES (?,?,?,'queued',?,?,?)`, id.New(), kind, string(payload), now, now, now)
	return err
}
func insertJobValue(ctx context.Context, executor sqlExecutor, kind ingest.JobKind, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return insertJob(ctx, executor, kind, encoded)
}

// Enqueue serializes a small payload and creates a queued durable job.
func (s *Store) Enqueue(ctx context.Context, kind ingest.JobKind, payload any) error {
	return insertJobValue(ctx, s.db, kind, payload)
}
func insertDomainEvent(ctx context.Context, tx *sql.Tx, eventType, message, kind, resourceID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO user_events(id,occurred_at,severity,type,message,resource_kind,resource_id) VALUES (?,?,'info',?,?,?,?)`, id.New(), time.Now().UTC().UnixMilli(), eventType, message, kind, resourceID)
	return err
}

// GetAnalysisBatch reconstructs one batch, including ordered context/focus and
// stored extraction diagnostics.
func (s *Store) GetAnalysisBatch(ctx context.Context, batchID string) (*ingest.AnalysisBatch, error) {
	var b ingest.AnalysisBatch
	var started, last int64
	var closed sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,source_session_id,status,started_at,last_text_at,closed_at,close_reason,raw_model_response,normalized_response,model,prompt_version,last_error FROM analysis_batches WHERE id=?`, batchID).Scan(&b.ID, &b.SourceSessionID, &b.Status, &started, &last, &closed, &b.CloseReason, &b.RawModelResponse, &b.NormalizedResponse, &b.Model, &b.PromptVersion, &b.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.StartedAt = time.UnixMilli(started).UTC()
	b.LastTextAt = time.UnixMilli(last).UTC()
	b.ClosedAt = nullableTime(closed)
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.source_session_id,c.sequence,c.captured_at,bc.text,c.status,bc.role FROM analysis_batch_chunks bc JOIN chunks c ON c.id=bc.chunk_id WHERE bc.batch_id=? ORDER BY bc.position`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	b.Input.Context = []ingest.Chunk{}
	b.Input.Focus = []ingest.Chunk{}
	for rows.Next() {
		var c ingest.Chunk
		var captured int64
		var role string
		if err = rows.Scan(&c.ID, &c.SourceSessionID, &c.Sequence, &captured, &c.Text, &c.Status, &role); err != nil {
			return nil, err
		}
		c.CapturedAt = time.UnixMilli(captured).UTC()
		if role == "focus" {
			b.Input.Focus = append(b.Input.Focus, c)
		} else {
			b.Input.Context = append(b.Input.Context, c)
		}
	}
	return &b, rows.Err()
}

// ListAnalysisBatches returns recent batches for contributor diagnostics.
func (s *Store) ListAnalysisBatches(ctx context.Context, limit int) ([]ingest.AnalysisBatch, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM analysis_batches ORDER BY rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	result := []ingest.AnalysisBatch{}
	for _, v := range ids {
		b, err := s.GetAnalysisBatch(ctx, v)
		if err != nil {
			return nil, err
		}
		result = append(result, *b)
	}
	return result, nil
}

// BeginExtraction marks a ready/retryable batch as extracting.
func (s *Store) BeginExtraction(ctx context.Context, batchID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE analysis_batches SET status='extracting' WHERE id=? AND status IN ('ready','failed','extracting')`, batchID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("batch is not ready for extraction")
	}
	return nil
}

// SaveBatchFailure records raw diagnostic output and the extraction error
// without losing the batch input needed for retry.
func (s *Store) SaveBatchFailure(ctx context.Context, batchID, raw, model, promptVersion string, cause error) error {
	_, err := s.db.ExecContext(ctx, `UPDATE analysis_batches SET status='failed',raw_model_response=?,model=?,prompt_version=?,last_error=? WHERE id=? AND status<>'extracted'`, raw, model, promptVersion, cause.Error(), batchID)
	return err
}
