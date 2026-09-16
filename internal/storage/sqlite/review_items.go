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
	"strings"
)

// PendingCount returns the number of conspects waiting for user review.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conspects WHERE status IN ('preparing_review','review_pending','ready_to_apply','failed')`).Scan(&count)
	return count, err
}

// ListReviewConspects returns oldest pending/reviewable conspects first.
func (s *Store) ListReviewConspects(ctx context.Context, limit int) ([]review.Conspect, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM conspects WHERE status NOT IN ('applied','rejected') ORDER BY rowid LIMIT ?`, limit)
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
	out := []review.Conspect{}
	for _, v := range ids {
		c, err := s.GetReviewConspect(ctx, v)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, nil
}

// GetReviewConspect reconstructs one conspect with items, matches, relationships,
// and taxonomy decisions.
func (s *Store) GetReviewConspect(ctx context.Context, conspectID string) (*review.Conspect, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c, err := getConspect(ctx, tx, conspectID)
	if err != nil || c == nil {
		return nil, err
	}
	out := &review.Conspect{Conspect: *c}
	out.Items, err = readReviewItems(ctx, tx, `conspect_id=?`, conspectID)
	if err != nil {
		return nil, err
	}
	if err = readReviewRelationships(ctx, tx, conspectID, out.Items); err != nil {
		return nil, err
	}
	out.Taxonomy, err = readTaxonomyResolutions(ctx, tx, conspectID)
	if err != nil {
		return nil, err
	}
	required, err := unknownTaxonomy(ctx, tx, out.Items)
	if err != nil {
		return nil, err
	}
	for i := range out.Taxonomy {
		out.Taxonomy[i].Required = required[taxonomyKey(out.Taxonomy[i].Kind, out.Taxonomy[i].Value)]
	}
	return out, tx.Commit()
}

func readReviewRelationships(ctx context.Context, db sqlReader, conspectID string, items []review.Item) error {
	candidateItems := map[string]string{}
	itemIndexes := map[string]int{}
	for index, item := range items {
		itemIndexes[item.ID] = index
		for _, variant := range item.IncomingVariants {
			candidateItems[variant.CandidateID] = item.ID
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT thought_id,term_id FROM conspect_links WHERE conspect_id=? ORDER BY thought_id,term_id`, conspectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]map[string]bool{}
	for rows.Next() {
		var thoughtCandidateID, termCandidateID string
		if err = rows.Scan(&thoughtCandidateID, &termCandidateID); err != nil {
			return err
		}
		thoughtItemID, termItemID := candidateItems[thoughtCandidateID], candidateItems[termCandidateID]
		index, ok := itemIndexes[thoughtItemID]
		if !ok || termItemID == "" {
			continue
		}
		if seen[thoughtItemID] == nil {
			seen[thoughtItemID] = map[string]bool{}
		}
		if !seen[thoughtItemID][termItemID] {
			items[index].RelatedItemIDs = append(items[index].RelatedItemIDs, termItemID)
			seen[thoughtItemID][termItemID] = true
		}
	}
	return rows.Err()
}

// PendingReviewItems supplies not-yet-applied candidates for cross-conspect
// similarity suggestions.
func (s *Store) PendingReviewItems(ctx context.Context) ([]review.Item, error) {
	return readReviewItems(ctx, s.db, `conspect_id IN (SELECT id FROM conspects WHERE status NOT IN ('applied','rejected'))`)
}
func readReviewItems(ctx context.Context, db sqlReader, predicate string, args ...any) ([]review.Item, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,conspect_id,kind,variants_json,resolution,target_id,target_version,final_json,status FROM review_items WHERE `+predicate+` ORDER BY position,id`, args...)
	if err != nil {
		return nil, err
	}
	out := []review.Item{}
	for rows.Next() {
		var item review.Item
		var variants, final string
		if err = rows.Scan(&item.ID, &item.ConspectID, &item.Kind, &variants, &item.Resolution, &item.TargetID, &item.TargetVersion, &final, &item.Status); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(variants), &item.IncomingVariants); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(final), &item.FinalValue); err != nil {
			rows.Close()
			return nil, err
		}
		item.CanonicalMatches = []review.Match{}
		item.PendingMatches = []review.Match{}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		matches, err := db.QueryContext(ctx, `SELECT value_json FROM review_matches WHERE item_id=? ORDER BY position`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for matches.Next() {
			var value string
			var m review.Match
			if err = matches.Scan(&value); err != nil {
				matches.Close()
				return nil, err
			}
			if err = json.Unmarshal([]byte(value), &m); err != nil {
				matches.Close()
				return nil, err
			}
			if m.Source == "canonical" {
				out[i].CanonicalMatches = append(out[i].CanonicalMatches, m)
			} else {
				out[i].PendingMatches = append(out[i].PendingMatches, m)
			}
		}
		if err = matches.Err(); err != nil {
			matches.Close()
			return nil, err
		}
		matches.Close()
	}
	return out, nil
}
func writeReviewItem(ctx context.Context, tx *sql.Tx, item review.Item, position int) error {
	variants, err := json.Marshal(item.IncomingVariants)
	if err != nil {
		return err
	}
	final, err := json.Marshal(item.FinalValue)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO review_items(id,conspect_id,kind,position,variants_json,resolution,target_id,target_version,final_json,status) VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET variants_json=excluded.variants_json,resolution=excluded.resolution,target_id=excluded.target_id,target_version=excluded.target_version,final_json=excluded.final_json,status=excluded.status`, item.ID, item.ConspectID, item.Kind, position, string(variants), item.Resolution, item.TargetID, item.TargetVersion, string(final), item.Status)
	return err
}

// SavePreparedReview atomically replaces deterministic prepared state and marks
// the conspect ready for user review.
func (s *Store) SavePreparedReview(ctx context.Context, conspectID string, items []review.Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state ingest.ConspectStatus
	if err = tx.QueryRowContext(ctx, `SELECT status FROM conspects WHERE id=?`, conspectID).Scan(&state); err != nil {
		return err
	}
	if state != ingest.ConspectPreparingReview {
		return nil
	}
	for index, item := range items {
		if err = writeReviewItem(ctx, tx, item, index); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM review_matches WHERE item_id=?`, item.ID); err != nil {
			return err
		}
		matches := append(append([]review.Match{}, item.CanonicalMatches...), item.PendingMatches...)
		for position, m := range matches {
			b, _ := json.Marshal(m)
			if _, err = tx.ExecContext(ctx, `INSERT INTO review_matches(item_id,position,value_json) VALUES (?,?,?)`, item.ID, position, string(b)); err != nil {
				return err
			}
		}
	}
	if err = refreshReviewState(ctx, tx, conspectID); err != nil {
		return err
	}
	if err = insertDomainEvent(ctx, tx, "conspect.review_ready", "Conspect is ready for review", "conspect", conspectID); err != nil {
		return err
	}
	return tx.Commit()
}

func editableConspect(ctx context.Context, tx *sql.Tx, conspectID string) error {
	var state ingest.ConspectStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM conspects WHERE id=?`, conspectID).Scan(&state); err != nil {
		return err
	}
	if state != ingest.ConspectReviewPending && state != ingest.ConspectReadyToApply {
		return fmt.Errorf("%w: conspect is %s", review.ErrInvalid, state)
	}
	return nil
}

// SaveReviewDecision validates and persists one draft decision without mutating
// canonical tables.
func (s *Store) SaveReviewDecision(ctx context.Context, itemID string, d review.Decision) error {
	err := s.saveReviewDecision(ctx, itemID, d)
	if !errors.Is(err, review.ErrConflict) {
		return err
	}
	var conspectID string
	if queryErr := s.db.QueryRowContext(ctx, `SELECT conspect_id FROM review_items WHERE id=?`, itemID).Scan(&conspectID); queryErr != nil {
		return errors.Join(err, queryErr)
	}
	return s.rematchConflict(ctx, conspectID, err)
}
func (s *Store) saveReviewDecision(ctx context.Context, itemID string, d review.Decision) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	items, err := readReviewItems(ctx, tx, `id=?`, itemID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return sql.ErrNoRows
	}
	item := items[0]
	if err = editableConspect(ctx, tx, item.ConspectID); err != nil {
		return err
	}
	var exact *review.Match
	for index := range item.CanonicalMatches {
		if item.CanonicalMatches[index].Exact {
			exact = &item.CanonicalMatches[index]
			break
		}
	}
	if exact != nil && (d.Resolution != review.Update && d.Resolution != review.KeepExisting || d.TargetID != exact.ID) {
		return review.ErrExactMatch
	}
	switch d.Resolution {
	case review.Create, review.Update, review.KeepExisting, review.Reject:
	default:
		return fmt.Errorf("%w: resolution must be create, update, keep_existing or reject", review.ErrInvalid)
	}
	if d.Resolution == review.Update || d.Resolution == review.KeepExisting {
		target, err := canonicalEntity(ctx, tx, item.Kind, d.TargetID)
		if err != nil {
			return err
		}
		if target == nil {
			return fmt.Errorf("%w: target does not exist", review.ErrInvalid)
		}
		if d.TargetVersion != target.Version {
			return fmt.Errorf("%w: reload selected target", review.ErrConflict)
		}
		item.FinalValue = target.Value
		item.TargetID = target.ID
		item.TargetVersion = target.Version
	} else {
		if d.TargetID != "" || d.TargetVersion != 0 {
			return fmt.Errorf("%w: create/reject cannot select a target", review.ErrInvalid)
		}
		item.TargetID = ""
		item.TargetVersion = 0
	}
	if d.FinalValue != nil && d.Resolution != review.KeepExisting && d.Resolution != review.Reject {
		item.FinalValue = *d.FinalValue
	}
	if d.Resolution != review.Reject {
		if err = validateValue(item.Kind, &item.FinalValue); err != nil {
			return err
		}
	}
	if d.RelatedItemIDs != nil {
		if item.Kind != review.ObjectThought {
			return fmt.Errorf("%w: only thoughts can select related terms", review.ErrInvalid)
		}
		if err = updateReviewRelationships(ctx, tx, item, *d.RelatedItemIDs); err != nil {
			return err
		}
	}
	allItems, err := readReviewItems(ctx, tx, `conspect_id=?`, item.ConspectID)
	if err != nil {
		return err
	}
	if err = readReviewRelationships(ctx, tx, item.ConspectID, allItems); err != nil {
		return err
	}
	for _, candidate := range allItems {
		if candidate.ID == item.ID {
			item.RelatedItemIDs = candidate.RelatedItemIDs
			break
		}
	}
	if item.Kind == review.ObjectThought && d.Resolution != review.Reject {
		related := item.RelatedItemIDs
		if d.RelatedItemIDs != nil {
			related = *d.RelatedItemIDs
		}
		for _, relatedID := range related {
			for _, candidate := range allItems {
				if candidate.ID == relatedID && candidate.Resolution == review.Reject {
					return review.ErrDependency
				}
			}
		}
	}
	item.Resolution = d.Resolution
	item.Status = review.Resolved
	if err = writeReviewItem(ctx, tx, item, 0); err != nil {
		return err
	}
	if item.Kind == review.ObjectTerm && d.Resolution == review.Reject {
		for _, candidate := range allItems {
			if candidate.Kind != review.ObjectThought || candidate.Resolution == "" || candidate.Resolution == review.Reject {
				continue
			}
			for _, relatedID := range candidate.RelatedItemIDs {
				if relatedID == item.ID {
					if _, err = tx.ExecContext(ctx, `UPDATE review_items SET resolution='',target_id='',target_version=0,status='pending' WHERE id=?`, candidate.ID); err != nil {
						return err
					}
					break
				}
			}
		}
	}
	if err = refreshReviewState(ctx, tx, item.ConspectID); err != nil {
		return err
	}
	return tx.Commit()
}

func updateReviewRelationships(ctx context.Context, tx *sql.Tx, thought review.Item, relatedItemIDs []string) error {
	thoughtCandidates := make([]string, 0, len(thought.IncomingVariants))
	for _, variant := range thought.IncomingVariants {
		thoughtCandidates = append(thoughtCandidates, variant.CandidateID)
	}
	termCandidates := []string{}
	seenItems := map[string]bool{}
	for _, itemID := range relatedItemIDs {
		if itemID == "" || seenItems[itemID] {
			continue
		}
		seenItems[itemID] = true
		var conspectID, kind, variantsJSON string
		if err := tx.QueryRowContext(ctx, `SELECT conspect_id,kind,variants_json FROM review_items WHERE id=?`, itemID).Scan(&conspectID, &kind, &variantsJSON); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: related term does not exist", review.ErrInvalid)
			}
			return err
		}
		if conspectID != thought.ConspectID || review.ObjectKind(kind) != review.ObjectTerm {
			return fmt.Errorf("%w: related entity must be a term from this conspect", review.ErrInvalid)
		}
		var variants []review.Variant
		if err := json.Unmarshal([]byte(variantsJSON), &variants); err != nil {
			return err
		}
		for _, variant := range variants {
			termCandidates = append(termCandidates, variant.CandidateID)
		}
	}
	for _, thoughtCandidateID := range thoughtCandidates {
		if _, err := tx.ExecContext(ctx, `DELETE FROM conspect_links WHERE conspect_id=? AND thought_id=?`, thought.ConspectID, thoughtCandidateID); err != nil {
			return err
		}
		for _, termCandidateID := range termCandidates {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO conspect_links(conspect_id,thought_id,term_id) VALUES (?,?,?)`, thought.ConspectID, thoughtCandidateID, termCandidateID); err != nil {
				return err
			}
		}
	}
	return nil
}
func validateValue(kind review.ObjectKind, v *review.Value) error {
	v.Name = knowledge.Clean(v.Name)
	v.Thesis = knowledge.Clean(v.Thesis)
	v.Description = strings.TrimSpace(v.Description)
	v.Body = strings.TrimSpace(v.Body)
	v.Tags = ingest.NormalizedValues(v.Tags)
	if kind == review.ObjectTerm {
		if v.Name == "" || v.Thesis != "" || v.Body != "" {
			return fmt.Errorf("%w: term requires name and term fields only", review.ErrInvalid)
		}
		if len(v.Tags) == 0 {
			return fmt.Errorf("%w: term requires at least one tag", review.ErrInvalid)
		}
	} else if kind == review.ObjectThought {
		if v.Thesis == "" || v.Name != "" || v.Description != "" {
			return fmt.Errorf("%w: thought requires thesis and thought fields only", review.ErrInvalid)
		}
	} else {
		return review.ErrInvalid
	}
	return nil
}

func exactCanonicalTitle(ctx context.Context, db sqlReader, kind review.ObjectKind, title string) (bool, error) {
	table, column := "terms", "canonical_name"
	if kind == review.ObjectThought {
		table, column = "thoughts", "thesis"
	} else if kind != review.ObjectTerm {
		return false, fmt.Errorf("%w: unknown entity kind", review.ErrInvalid)
	}
	rows, err := db.QueryContext(ctx, `SELECT `+column+` FROM `+table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	want := knowledge.Normalize(title)
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return false, err
		}
		if knowledge.Normalize(value) == want {
			return true, nil
		}
	}
	return false, rows.Err()
}

func canonicalEntity(ctx context.Context, db sqlReader, kind review.ObjectKind, entityID string) (*review.Match, error) {
	m := &review.Match{ID: entityID, Kind: kind, Source: "canonical"}
	var err error
	if kind == review.ObjectTerm {
		err = db.QueryRowContext(ctx, `SELECT canonical_name,description,version FROM terms WHERE id=?`, entityID).Scan(&m.Value.Name, &m.Value.Description, &m.Version)
	} else if kind == review.ObjectThought {
		err = db.QueryRowContext(ctx, `SELECT thesis,body,version FROM thoughts WHERE id=?`, entityID).Scan(&m.Value.Thesis, &m.Value.Body, &m.Version)
	} else {
		return nil, review.ErrInvalid
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	table, owner := "term_tags", "term_id"
	if kind == review.ObjectThought {
		table, owner = "thought_tags", "thought_id"
	}
	read := func(join, owner, table, target string) ([]string, error) {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT t.name FROM %s j JOIN %s t ON t.id=j.%s WHERE j.%s=? ORDER BY t.name`, join, table, target, owner), entityID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, rows.Err()
	}
	m.Value.Tags, err = read(table, owner, "tags", "tag_id")
	if err != nil {
		return nil, err
	}
	return m, err
}

// SplitReviewItem moves selected incoming variants into a new pending item while
// preserving provenance and relationships.
func (s *Store) SplitReviewItem(ctx context.Context, itemID string, variantIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	items, err := readReviewItems(ctx, tx, `id=?`, itemID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return sql.ErrNoRows
	}
	item := items[0]
	if err = editableConspect(ctx, tx, item.ConspectID); err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, v := range variantIDs {
		selected[v] = true
	}
	split, remaining := []review.Variant{}, []review.Variant{}
	for _, v := range item.IncomingVariants {
		if selected[v.CandidateID] {
			split = append(split, v)
		} else {
			remaining = append(remaining, v)
		}
	}
	if len(split) == 0 || len(remaining) == 0 || len(split) != len(selected) {
		return fmt.Errorf("%w: select a proper subset of variants", review.ErrInvalid)
	}
	item.IncomingVariants = remaining
	item.FinalValue = remaining[0].Value
	item.Resolution = ""
	item.TargetID = ""
	item.TargetVersion = 0
	item.Status = review.Pending
	if err = writeReviewItem(ctx, tx, item, 0); err != nil {
		return err
	}
	item.ID = id.New()
	item.IncomingVariants = split
	item.FinalValue = split[0].Value
	if err = writeReviewItem(ctx, tx, item, 1); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE conspects SET status='preparing_review' WHERE id=?`, item.ConspectID); err != nil {
		return err
	}
	if err = insertJobValue(ctx, tx, ingest.JobPrepareConspectReview, ingest.ConspectPayload{ConspectID: item.ConspectID}); err != nil {
		return err
	}
	return tx.Commit()
}

func taxonomyKey(kind knowledge.TaxonomyKind, value string) string {
	return string(kind) + ":" + knowledge.Normalize(value)
}
func taxonomyEntry(ctx context.Context, db sqlReader, kind knowledge.TaxonomyKind, value string) (*knowledge.TaxonomyEntry, error) {
	// SQLite NOCASE is ASCII-only; normalize names and aliases in Go for Unicode equivalence.
	table, err := taxonomyTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT e.id,e.name,COALESCE(a.alias,'') FROM `+table+` e LEFT JOIN taxonomy_aliases a ON a.entry_id=e.id AND a.kind=? WHERE e.enabled=1 ORDER BY e.id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	value = knowledge.Normalize(value)
	for rows.Next() {
		var entry knowledge.TaxonomyEntry
		var alias string
		if err = rows.Scan(&entry.ID, &entry.Name, &alias); err != nil {
			return nil, err
		}
		if knowledge.Normalize(entry.Name) == value || (alias != "" && knowledge.Normalize(alias) == value) {
			entry.Kind = kind
			entry.Enabled = true
			return &entry, nil
		}
	}
	return nil, rows.Err()
}
func unknownTaxonomy(ctx context.Context, db sqlReader, items []review.Item) (map[string]bool, error) {
	out := map[string]bool{}
	for _, item := range items {
		if item.Resolution == review.Reject || item.Resolution == review.KeepExisting {
			continue
		}
		for _, value := range item.FinalValue.Tags {
			entry, err := taxonomyEntry(ctx, db, knowledge.TaxonomyTag, value)
			if err != nil {
				return nil, err
			}
			if entry == nil {
				out[taxonomyKey(knowledge.TaxonomyTag, value)] = true
			}
		}
	}
	return out, nil
}
func readTaxonomyResolutions(ctx context.Context, db sqlReader, conspectID string) ([]review.TaxonomyResolution, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,conspect_id,kind,value,resolution,target_id FROM review_taxonomy_resolutions WHERE conspect_id=? ORDER BY kind,value`, conspectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []review.TaxonomyResolution{}
	for rows.Next() {
		var v review.TaxonomyResolution
		if err = rows.Scan(&v.ID, &v.ConspectID, &v.Kind, &v.Value, &v.Resolution, &v.TargetID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func refreshReviewState(ctx context.Context, tx *sql.Tx, conspectID string) error {
	items, err := readReviewItems(ctx, tx, `conspect_id=?`, conspectID)
	if err != nil {
		return err
	}
	unknown, err := unknownTaxonomy(ctx, tx, items)
	if err != nil {
		return err
	}
	for key := range unknown {
		kind, value, _ := strings.Cut(key, ":")
		if _, err = tx.ExecContext(ctx, `INSERT INTO review_taxonomy_resolutions(id,conspect_id,kind,value) VALUES (?,?,?,?) ON CONFLICT(conspect_id,kind,value) DO NOTHING`, id.New(), conspectID, kind, value); err != nil {
			return err
		}
	}
	resolutions, err := readTaxonomyResolutions(ctx, tx, conspectID)
	if err != nil {
		return err
	}
	ready := true
	for _, item := range items {
		if item.Status != review.Resolved {
			ready = false
		}
	}
	for _, r := range resolutions {
		if unknown[taxonomyKey(r.Kind, r.Value)] && r.Resolution == "" {
			ready = false
		}
	}
	state := ingest.ConspectReviewPending
	if ready {
		state = ingest.ConspectReadyToApply
	}
	_, err = tx.ExecContext(ctx, `UPDATE conspects SET status=? WHERE id=?`, state, conspectID)
	return err
}

// SaveTaxonomyDecision validates map/create/remove for one unknown tag and
// refreshes whether the parent conspect is ready to apply.
func (s *Store) SaveTaxonomyDecision(ctx context.Context, resolutionID string, d review.TaxonomyDecision) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var conspectID, previousValue string
	var kind knowledge.TaxonomyKind
	if err = tx.QueryRowContext(ctx, `SELECT conspect_id,kind,value FROM review_taxonomy_resolutions WHERE id=?`, resolutionID).Scan(&conspectID, &kind, &previousValue); err != nil {
		return err
	}
	if err = editableConspect(ctx, tx, conspectID); err != nil {
		return err
	}

	if d.Value != nil && d.Resolution != "create" {
		return fmt.Errorf("%w: only create accepts value", review.ErrInvalid)
	}
	if d.Value != nil {
		value := knowledge.Normalize(*d.Value)
		if value == "" {
			return fmt.Errorf("%w: tag name is required", review.ErrInvalid)
		}
		entry, err := taxonomyEntry(ctx, tx, kind, value)
		if err != nil {
			return err
		}
		// An edited label matching a name or alias uses the existing canonical tag.
		if entry != nil {
			value = entry.Name
			d.Resolution = "map"
			d.TargetID = entry.ID
		}
		if taxonomyKey(kind, value) != taxonomyKey(kind, previousValue) {
			items, err := readReviewItems(ctx, tx, `conspect_id=?`, conspectID)
			if err != nil {
				return err
			}
			for _, item := range items {
				if item.Resolution == review.KeepExisting || item.Resolution == review.Reject {
					continue
				}
				changed := false
				for i, tag := range item.FinalValue.Tags {
					if taxonomyKey(kind, tag) == taxonomyKey(kind, previousValue) {
						item.FinalValue.Tags[i] = value
						changed = true
					}
				}
				if changed {
					item.FinalValue.Tags = ingest.NormalizedValues(item.FinalValue.Tags)
					if err = writeReviewItem(ctx, tx, item, 0); err != nil {
						return err
					}
				}
			}
			// Merge duplicate draft labels and preserve the edited resolution's identity.
			if _, err = tx.ExecContext(ctx, `DELETE FROM review_taxonomy_resolutions WHERE conspect_id=? AND kind=? AND value=? AND id<>?`, conspectID, kind, value, resolutionID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE review_taxonomy_resolutions SET value=? WHERE id=?`, value, resolutionID); err != nil {
				return err
			}
		}
	}
	switch d.Resolution {
	case "map":
		table, _ := taxonomyTable(kind)
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id=? AND enabled=1`, d.TargetID).Scan(&exists); err != nil {
			return fmt.Errorf("%w: target taxonomy entry does not exist", review.ErrInvalid)
		}
	case "create", "remove":
		if d.TargetID != "" {
			return fmt.Errorf("%w: only map accepts target_id", review.ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: taxonomy resolution must be map, create or remove", review.ErrInvalid)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE review_taxonomy_resolutions SET resolution=?,target_id=? WHERE id=?`, d.Resolution, d.TargetID, resolutionID); err != nil {
		return err
	}
	if err = refreshReviewState(ctx, tx, conspectID); err != nil {
		return err
	}
	return tx.Commit()
}
