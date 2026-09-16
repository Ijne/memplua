package sqlite

import (
	"context"
	"crawler/internal/id"
	"crawler/internal/ingest"
	"crawler/internal/knowledge"
	"crawler/internal/review"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ApplyConspect is the only canonical mutation boundary. The complete review,
// taxonomy, links, provenance, graph revision and export outbox commit together.
func (s *Store) ApplyConspect(ctx context.Context, conspectID string) error {
	err := s.applyConspect(ctx, conspectID)
	if !errors.Is(err, review.ErrConflict) {
		return err
	}
	return s.rematchConflict(ctx, conspectID, err)
}

func (s *Store) rematchConflict(ctx context.Context, conspectID string, err error) error {
	// The failed transaction has already rolled back. Rematching is a separate durable transition.
	tx, resetErr := s.db.BeginTx(ctx, nil)
	if resetErr != nil {
		return errors.Join(err, resetErr)
	}
	defer tx.Rollback()
	result, resetErr := tx.ExecContext(ctx, `UPDATE conspects SET status='preparing_review' WHERE id=? AND status IN ('review_pending','ready_to_apply')`, conspectID)
	if resetErr != nil {
		return errors.Join(err, resetErr)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return err
	}
	if _, resetErr = tx.ExecContext(ctx, `UPDATE review_items SET status='pending',resolution='',target_id='',target_version=0 WHERE conspect_id=? AND resolution IN ('update','keep_existing')`, conspectID); resetErr != nil {
		return errors.Join(err, resetErr)
	}
	if resetErr = insertJobValue(ctx, tx, ingest.JobPrepareConspectReview, ingest.ConspectPayload{ConspectID: conspectID}); resetErr != nil {
		return errors.Join(err, resetErr)
	}
	if resetErr = insertDomainEvent(ctx, tx, "conspect.apply_conflict", "Canonical knowledge changed; review refreshed matches", "conspect", conspectID); resetErr != nil {
		return errors.Join(err, resetErr)
	}
	return errors.Join(err, tx.Commit())
}

func (s *Store) applyConspect(ctx context.Context, conspectID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state ingest.ConspectStatus
	if err = tx.QueryRowContext(ctx, `SELECT status FROM conspects WHERE id=?`, conspectID).Scan(&state); err != nil {
		return err
	}
	if state == ingest.ConspectApplied || state == ingest.ConspectRejected {
		return nil
	}
	if state != ingest.ConspectReadyToApply {
		return review.ErrNotReady
	}
	items, err := readReviewItems(ctx, tx, `conspect_id=?`, conspectID)
	if err != nil {
		return err
	}
	if err = readReviewRelationships(ctx, tx, conspectID, items); err != nil {
		return err
	}
	accepted := 0
	updates := map[string]bool{}
	// Check every version before writing anything, including keep_existing endpoints.
	for _, item := range items {
		if item.Status != review.Resolved {
			return review.ErrNotReady
		}
		if item.Resolution == review.Reject {
			continue
		}
		if item.Kind == review.ObjectThought {
			for _, relatedID := range item.RelatedItemIDs {
				valid := false
				for _, related := range items {
					if related.ID == relatedID && related.Kind == review.ObjectTerm && related.Resolution != review.Reject {
						valid = true
						break
					}
				}
				if !valid {
					return review.ErrDependency
				}
			}
		}
		accepted++
		if item.Resolution == review.Update || item.Resolution == review.KeepExisting {
			target, err := canonicalEntity(ctx, tx, item.Kind, item.TargetID)
			if err != nil {
				return err
			}
			if target == nil || target.Version != item.TargetVersion {
				return review.ErrConflict
			}
			if item.Resolution == review.Update {
				key := string(item.Kind) + ":" + item.TargetID
				if updates[key] {
					return fmt.Errorf("%w: multiple updates to the same target; resolve the extra item as keep_existing", review.ErrInvalid)
				}
				updates[key] = true
			}
		} else if item.Resolution == review.Create {
			exists, exactErr := exactCanonicalTitle(ctx, tx, item.Kind, item.FinalValue.Title(item.Kind))
			if exactErr != nil {
				return exactErr
			}
			if exists {
				return review.ErrConflict
			}
		} else {
			return review.ErrNotReady
		}
	}
	if accepted == 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE review_items SET status='applied' WHERE conspect_id=?`, conspectID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE conspects SET status='rejected' WHERE id=?`, conspectID); err != nil {
			return err
		}
		return tx.Commit()
	}
	resolutions, err := readTaxonomyResolutions(ctx, tx, conspectID)
	if err != nil {
		return err
	}
	byKey := map[string]review.TaxonomyResolution{}
	for _, r := range resolutions {
		byKey[taxonomyKey(r.Kind, r.Value)] = r
	}
	now := time.Now().UTC()
	var revision int64
	if err = tx.QueryRowContext(ctx, `UPDATE graph_meta SET revision=revision+1 WHERE id=1 RETURNING revision`).Scan(&revision); err != nil {
		return err
	}
	// Resolve only labels used by accepted create/update values. Rejected-only labels are ignored.
	for i := range items {
		item := &items[i]
		if item.Resolution == review.Reject || item.Resolution == review.KeepExisting {
			continue
		}
		if err = validateValue(item.Kind, &item.FinalValue); err != nil {
			return err
		}
		item.FinalValue.Tags, err = applyTaxonomy(ctx, tx, conspectID, revision, knowledge.TaxonomyTag, item.FinalValue.Tags, byKey, now)
		if err != nil {
			return err
		}
	}
	candidateTargets := map[string]string{}
	for i := range items {
		item := &items[i]
		if item.Resolution == review.Reject {
			continue
		}
		if item.Resolution == review.Create {
			item.TargetID = id.New()
		}
		if item.Resolution != review.KeepExisting {
			v := item.FinalValue
			if item.Kind == review.ObjectTerm {
				err = upsertTerm(ctx, tx, knowledge.Term{ID: item.TargetID, Name: v.Name, Description: v.Description, Tags: v.Tags}, now)
			} else {
				err = upsertThought(ctx, tx, knowledge.Thought{ID: item.TargetID, Thesis: v.Thesis, Body: v.Body, Tags: v.Tags}, now)
			}
			if err != nil {
				return err
			}
			current, err := canonicalEntity(ctx, tx, item.Kind, item.TargetID)
			if err != nil {
				return err
			}
			snapshot, _ := json.Marshal(current)
			if err = insertRevision(ctx, tx, revision, string(item.Kind), item.TargetID, item.ID, conspectID, string(snapshot), now); err != nil {
				return err
			}
		}
		for _, variant := range item.IncomingVariants {
			candidateTargets[variant.CandidateID] = item.TargetID
			if _, err = tx.ExecContext(ctx, `INSERT INTO canonical_provenance(conspect_id,review_item_id,object_kind,object_id,candidate_id,chunk_id,is_anchor,graph_revision) SELECT ?,?,?,?,?,chunk_id,is_anchor,? FROM candidate_provenance WHERE candidate_id=?`, conspectID, item.ID, item.Kind, item.TargetID, variant.CandidateID, revision, variant.CandidateID); err != nil {
				return err
			}
		}
		// Store the resolved canonical ID for create decisions as well as update/keep.
		if err = writeReviewItem(ctx, tx, *item, i); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT thought_id,term_id FROM conspect_links WHERE conspect_id=? ORDER BY thought_id,term_id`, conspectID)
	if err != nil {
		return err
	}
	type pair struct{ thought, term string }
	links := []pair{}
	for rows.Next() {
		var p pair
		if err = rows.Scan(&p.thought, &p.term); err != nil {
			rows.Close()
			return err
		}
		links = append(links, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, p := range links {
		thoughtID, termID := candidateTargets[p.thought], candidateTargets[p.term]
		if thoughtID == "" || termID == "" {
			continue
		}
		link := knowledge.ThoughtTerm{ID: id.New(), ThoughtID: thoughtID, TermID: termID, CreatedAt: now}
		result, err := tx.ExecContext(ctx, `INSERT INTO thought_terms(id,thought_id,term_id,created_at) VALUES (?,?,?,?) ON CONFLICT(thought_id,term_id) DO NOTHING`, link.ID, thoughtID, termID, now.UnixMilli())
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n > 0 {
			snapshot, _ := json.Marshal(link)
			if err = insertRevision(ctx, tx, revision, "thought_term", link.ID, "", conspectID, string(snapshot), now); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE review_items SET status='applied' WHERE conspect_id=?`, conspectID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE conspects SET status='applied' WHERE id=?`, conspectID); err != nil {
		return err
	}
	if err = insertJobValue(ctx, tx, ingest.JobExport, ingest.ExportPayload{Automatic: true}); err != nil {
		return err
	}
	if err = insertDomainEvent(ctx, tx, "conspect.applied", "Conspect applied to the knowledge graph", "conspect", conspectID); err != nil {
		return err
	}
	return tx.Commit()
}

func insertRevision(ctx context.Context, tx *sql.Tx, revision int64, kind, objectID, itemID, conspectID, snapshot string, now time.Time) error {
	var item any
	if itemID != "" {
		item = itemID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO revisions(id,graph_revision,object_kind,object_id,review_item_id,conspect_id,snapshot_json,created_at) VALUES (?,?,?,?,?,?,?,?)`, id.New(), revision, kind, objectID, item, conspectID, snapshot, now.UnixMilli())
	return err
}

func applyTaxonomy(ctx context.Context, tx *sql.Tx, conspectID string, revision int64, kind knowledge.TaxonomyKind, values []string, resolutions map[string]review.TaxonomyResolution, now time.Time) ([]string, error) {
	out := []string{}
	for _, value := range values {
		entry, err := taxonomyEntry(ctx, tx, kind, value)
		if err != nil {
			return nil, err
		}
		r, hasResolution := resolutions[taxonomyKey(kind, value)]
		// Respect explicit map/remove even if another conspect subsequently created this label.
		if hasResolution && r.Resolution == "remove" {
			continue
		}
		if hasResolution && r.Resolution == "map" {
			table, _ := taxonomyTable(kind)
			entry = &knowledge.TaxonomyEntry{Kind: kind, ID: r.TargetID, Enabled: true}
			if err = tx.QueryRowContext(ctx, `SELECT name FROM `+table+` WHERE id=? AND enabled=1`, r.TargetID).Scan(&entry.Name); err != nil {
				return nil, review.ErrNotReady
			}
		}
		if entry == nil {
			if !hasResolution || r.Resolution != "create" {
				return nil, review.ErrNotReady
			}
			table, _ := taxonomyTable(kind)
			entry = &knowledge.TaxonomyEntry{ID: id.New(), Kind: kind, Name: knowledge.Normalize(value), Enabled: true, CreatedAt: now}
			if _, err = tx.ExecContext(ctx, `INSERT INTO `+table+`(id,name,system,enabled,created_at) VALUES (?,?,0,1,?)`, entry.ID, entry.Name, now.UnixMilli()); err != nil {
				return nil, err
			}
			snapshot, _ := json.Marshal(entry)
			if err = insertRevision(ctx, tx, revision, "taxonomy", entry.ID, "", conspectID, string(snapshot), now); err != nil {
				return nil, err
			}
		}
		out = append(out, entry.Name)
	}
	return ingest.NormalizedValues(out), nil
}
