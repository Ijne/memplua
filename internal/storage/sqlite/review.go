package sqlite

import (
	"context"
	"crawler/internal/knowledge"
	"database/sql"
	"fmt"
	"time"
)

func upsertTerm(ctx context.Context, tx *sql.Tx, term knowledge.Term, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO terms(id, canonical_name, description, version, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET canonical_name = excluded.canonical_name,
			description = excluded.description, version = terms.version + 1, updated_at = excluded.updated_at
	`, term.ID, term.Name, term.Description, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM term_tags WHERE term_id = ?`, term.ID); err != nil {
		return err
	}
	return attachTaxonomy(ctx, tx, "term_tags", "term_id", term.ID, knowledge.TaxonomyTag, term.Tags)
}

func upsertThought(ctx context.Context, tx *sql.Tx, thought knowledge.Thought, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO thoughts(id, thesis, body, version, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET thesis = excluded.thesis,
			body = excluded.body, version = thoughts.version + 1, updated_at = excluded.updated_at
	`, thought.ID, thought.Thesis, thought.Body, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thought_tags WHERE thought_id = ?`, thought.ID); err != nil {
		return err
	}
	return attachTaxonomy(ctx, tx, "thought_tags", "thought_id", thought.ID, knowledge.TaxonomyTag, thought.Tags)
}

func attachTaxonomy(ctx context.Context, tx *sql.Tx, joinTable, ownerColumn, ownerID string, kind knowledge.TaxonomyKind, values []string) error {
	targetColumn := "tag_id"
	for _, value := range values {
		entry, err := taxonomyEntry(ctx, tx, kind, value)
		if err != nil {
			return err
		}
		if entry == nil {
			return fmt.Errorf("unknown %s %q", kind, value)
		}
		query := fmt.Sprintf(`INSERT OR IGNORE INTO %s(%s,%s) VALUES (?,?)`, joinTable, ownerColumn, targetColumn)
		if _, err = tx.ExecContext(ctx, query, ownerID, entry.ID); err != nil {
			return err
		}
	}
	return nil
}
