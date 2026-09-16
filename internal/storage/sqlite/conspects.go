package sqlite

import (
	"context"
	"crawler/internal/id"
	"crawler/internal/ingest"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SaveBatchExtraction atomically commits normalized extraction diagnostics,
// stable conspects/candidates/provenance, and review-preparation jobs.
func (s *Store) SaveBatchExtraction(ctx context.Context, batchID string, response ingest.Extraction, raw, model, promptVersion string) error {
	batch, err := s.GetAnalysisBatch(ctx, batchID)
	if err != nil {
		return err
	}
	if batch == nil {
		return sql.ErrNoRows
	}
	if batch.Status == ingest.BatchExtracted {
		return nil
	}
	response, err = ingest.NormalizeExtraction(batch.Input, response)
	if err != nil {
		return err
	}
	normalized, err := json.Marshal(response)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state ingest.BatchStatus
	if err = tx.QueryRowContext(ctx, `SELECT status FROM analysis_batches WHERE id=?`, batchID).Scan(&state); err != nil {
		return err
	}
	if state == ingest.BatchExtracted {
		return nil
	}
	if state == ingest.BatchCollecting {
		return errors.New("cannot extract collecting batch")
	}
	for position, group := range response.Conspects {
		conspectID := id.New()
		if _, err = tx.ExecContext(ctx, `INSERT INTO conspects(id,analysis_batch_id,source_session_id,position,topic,created_at,status) VALUES (?,?,?,?,?,?,'preparing_review')`, conspectID, batchID, batch.SourceSessionID, position, group.Topic, time.Now().UTC().UnixMilli()); err != nil {
			return err
		}
		refs := map[string]string{}
		for index, t := range group.Terms {
			t.ID = id.New()
			refs[t.Ref] = t.ID
			b, _ := json.Marshal(t)
			if _, err = tx.ExecContext(ctx, `INSERT INTO conspect_terms(id,conspect_id,position,ref,value_json) VALUES (?,?,?,?,?)`, t.ID, conspectID, index, t.Ref, string(b)); err != nil {
				return err
			}
			if err = saveProvenance(ctx, tx, t.ID, t.Provenance); err != nil {
				return err
			}
		}
		for index, t := range group.Thoughts {
			t.ID = id.New()
			b, _ := json.Marshal(t)
			if _, err = tx.ExecContext(ctx, `INSERT INTO conspect_thoughts(id,conspect_id,position,value_json) VALUES (?,?,?,?)`, t.ID, conspectID, index, string(b)); err != nil {
				return err
			}
			if err = saveProvenance(ctx, tx, t.ID, t.Provenance); err != nil {
				return err
			}
			for _, ref := range t.RelatedTermRefs {
				if _, err = tx.ExecContext(ctx, `INSERT INTO conspect_links(conspect_id,thought_id,term_id) VALUES (?,?,?)`, conspectID, t.ID, refs[ref]); err != nil {
					return err
				}
			}
		}
		if err = insertJobValue(ctx, tx, ingest.JobPrepareConspectReview, ingest.ConspectPayload{ConspectID: conspectID}); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE analysis_batches SET status='extracted',raw_model_response=?,normalized_response=?,model=?,prompt_version=?,last_error='' WHERE id=?`, raw, string(normalized), model, promptVersion, batchID); err != nil {
		return err
	}
	if err = insertDomainEvent(ctx, tx, "analysis_batch.extracted", "Analysis batch extracted", "analysis_batch", batchID); err != nil {
		return err
	}
	return tx.Commit()
}
func saveProvenance(ctx context.Context, tx *sql.Tx, candidateID string, p ingest.Provenance) error {
	for _, chunkID := range p.EvidenceChunkIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO candidate_provenance(candidate_id,chunk_id,is_anchor) VALUES (?,?,?)`, candidateID, chunkID, chunkID == p.AnchorChunkID); err != nil {
			return err
		}
	}
	return nil
}

type sqlReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// GetConspect reconstructs one extracted conspect and its candidate relations.
func (s *Store) GetConspect(ctx context.Context, conspectID string) (*ingest.Conspect, error) {
	return getConspect(ctx, s.db, conspectID)
}
func getConspect(ctx context.Context, db sqlReader, conspectID string) (*ingest.Conspect, error) {
	var c ingest.Conspect
	var created int64
	err := db.QueryRowContext(ctx, `SELECT id,analysis_batch_id,source_session_id,topic,created_at,status FROM conspects WHERE id=?`, conspectID).Scan(&c.ID, &c.AnalysisBatchID, &c.SourceSessionID, &c.Topic, &created, &c.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt = time.UnixMilli(created).UTC()
	c.Terms = []ingest.TermCandidate{}
	c.Thoughts = []ingest.ThoughtCandidate{}
	for _, kind := range []string{"terms", "thoughts"} {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT value_json FROM conspect_%s WHERE conspect_id=? ORDER BY position`, kind), conspectID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				rows.Close()
				return nil, err
			}
			if kind == "terms" {
				var t ingest.TermCandidate
				err = json.Unmarshal([]byte(value), &t)
				c.Terms = append(c.Terms, t)
			} else {
				var t ingest.ThoughtCandidate
				err = json.Unmarshal([]byte(value), &t)
				c.Thoughts = append(c.Thoughts, t)
			}
			if err != nil {
				rows.Close()
				return nil, err
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return &c, nil
}

// ConspectChunks returns the ordered focus chunks that can serve as evidence for
// a conspect; continuity-only context is excluded.
func (s *Store) ConspectChunks(ctx context.Context, conspectID string) ([]ingest.Chunk, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT c.id,c.source_session_id,c.sequence,c.captured_at,c.text,c.status FROM chunks c JOIN candidate_provenance p ON p.chunk_id=c.id WHERE p.candidate_id IN (SELECT id FROM conspect_terms WHERE conspect_id=? UNION ALL SELECT id FROM conspect_thoughts WHERE conspect_id=?) ORDER BY c.sequence`, conspectID, conspectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ingest.Chunk{}
	for rows.Next() {
		var c ingest.Chunk
		var captured int64
		if err = rows.Scan(&c.ID, &c.SourceSessionID, &c.Sequence, &captured, &c.Text, &c.Status); err != nil {
			return nil, err
		}
		c.CapturedAt = time.UnixMilli(captured).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}
