package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crawler/internal/knowledge"
)

// ListTerms returns canonical terms with tags in stable display order.
func (s *Store) ListTerms(ctx context.Context) ([]knowledge.Term, error) { return listTerms(ctx, s.db) }
func listTerms(ctx context.Context, db sqlReader) ([]knowledge.Term, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, canonical_name, description, version, created_at, updated_at
		FROM terms ORDER BY canonical_name COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	terms := []knowledge.Term{}
	for rows.Next() {
		term, err := scanTerm(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		terms = append(terms, term)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range terms {
		var err error
		terms[index].Tags, err = taxonomyFor(ctx, db, "term_tags", "term_id", terms[index].ID, "tags", "tag_id")
		if err != nil {
			return nil, err
		}
	}
	return terms, nil
}

// ListThoughts returns canonical thoughts with tags in stable display order.
func (s *Store) ListThoughts(ctx context.Context) ([]knowledge.Thought, error) {
	return listThoughts(ctx, s.db)
}
func listThoughts(ctx context.Context, db sqlReader) ([]knowledge.Thought, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, thesis, body, version, created_at, updated_at
		FROM thoughts ORDER BY updated_at DESC, thesis COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	thoughts := []knowledge.Thought{}
	for rows.Next() {
		thought, err := scanThought(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		thoughts = append(thoughts, thought)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range thoughts {
		var err error
		thoughts[index].Tags, err = taxonomyFor(ctx, db, "thought_tags", "thought_id", thoughts[index].ID, "tags", "tag_id")
		if err != nil {
			return nil, err
		}
	}
	return thoughts, nil
}

// FindTerm performs normalized exact canonical-name lookup.
func (s *Store) FindTerm(ctx context.Context, name string) (*knowledge.Term, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, canonical_name, description, version, created_at, updated_at
		FROM terms WHERE canonical_name = ? COLLATE NOCASE
	`, knowledge.Normalize(name))
	term, err := scanTerm(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	term.Tags, err = s.taxonomyFor(ctx, "term_tags", "term_id", term.ID, "tags", "tag_id")
	return &term, err
}

// FindThought performs normalized exact canonical-thesis lookup.
func (s *Store) FindThought(ctx context.Context, thesis string) (*knowledge.Thought, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, thesis, body, version, created_at, updated_at
		FROM thoughts WHERE thesis = ? COLLATE NOCASE
	`, knowledge.Normalize(thesis))
	thought, err := scanThought(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	thought.Tags, err = s.taxonomyFor(ctx, "thought_tags", "thought_id", thought.ID, "tags", "tag_id")
	return &thought, err
}

// ResolveTaxonomy resolves a normalized canonical name or alias.
func (s *Store) ResolveTaxonomy(ctx context.Context, kind knowledge.TaxonomyKind, value string) (*knowledge.TaxonomyEntry, error) {
	table, err := taxonomyTable(kind)
	if err != nil {
		return nil, err
	}
	normalized := knowledge.Normalize(value)
	query := fmt.Sprintf(`
		SELECT e.id, e.name, e.system, e.enabled, e.created_at
		FROM %s e
		LEFT JOIN taxonomy_aliases a ON a.entry_id = e.id AND a.kind = ?
		WHERE e.enabled = 1 AND (e.name = ? COLLATE NOCASE OR a.alias = ? COLLATE NOCASE)
		LIMIT 1
	`, table)
	var entry knowledge.TaxonomyEntry
	var system, enabled bool
	var created int64
	err = s.db.QueryRowContext(ctx, query, kind, normalized, normalized).Scan(
		&entry.ID, &entry.Name, &system, &enabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry.Kind = kind
	entry.System = system
	entry.Enabled = enabled
	entry.CreatedAt = time.UnixMilli(created).UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT alias FROM taxonomy_aliases WHERE kind = ? AND entry_id = ? ORDER BY alias`, kind, entry.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, err
		}
		entry.Aliases = append(entry.Aliases, alias)
	}
	return &entry, rows.Err()
}

// ListTaxonomy returns controlled entries and aliases for kind.
func (s *Store) ListTaxonomy(ctx context.Context, kind knowledge.TaxonomyKind) ([]knowledge.TaxonomyEntry, error) {
	table, err := taxonomyTable(kind)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`SELECT id, name, system, enabled, created_at FROM %s ORDER BY name COLLATE NOCASE`, table)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var entries []knowledge.TaxonomyEntry
	for rows.Next() {
		var entry knowledge.TaxonomyEntry
		var created int64
		if err := rows.Scan(&entry.ID, &entry.Name, &entry.System, &entry.Enabled, &created); err != nil {
			rows.Close()
			return nil, err
		}
		entry.Kind = kind
		entry.CreatedAt = time.UnixMilli(created).UTC()
		entries = append(entries, entry)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range entries {
		aliasRows, err := s.db.QueryContext(ctx, `SELECT alias FROM taxonomy_aliases WHERE kind = ? AND entry_id = ? ORDER BY alias`, kind, entries[index].ID)
		if err != nil {
			return nil, err
		}
		for aliasRows.Next() {
			var alias string
			if err := aliasRows.Scan(&alias); err != nil {
				aliasRows.Close()
				return nil, err
			}
			entries[index].Aliases = append(entries[index].Aliases, alias)
		}
		if err := aliasRows.Close(); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// Snapshot reads a consistent canonical graph and current graph revision.
func (s *Store) Snapshot(ctx context.Context) (knowledge.Snapshot, error) {
	snapshot := knowledge.Snapshot{Links: []knowledge.ThoughtTerm{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM graph_meta WHERE id = 1`).Scan(&snapshot.Revision); err != nil {
		return snapshot, err
	}
	if snapshot.Terms, err = listTerms(ctx, tx); err != nil {
		return snapshot, err
	}
	if snapshot.Thoughts, err = listThoughts(ctx, tx); err != nil {
		return snapshot, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, thought_id, term_id, created_at FROM thought_terms ORDER BY id`)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var link knowledge.ThoughtTerm
		var created int64
		if err := rows.Scan(&link.ID, &link.ThoughtID, &link.TermID, &created); err != nil {
			return snapshot, err
		}
		link.CreatedAt = time.UnixMilli(created).UTC()
		snapshot.Links = append(snapshot.Links, link)
	}
	if err = rows.Err(); err != nil {
		return snapshot, err
	}
	rows.Close()
	return snapshot, tx.Commit()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTerm(row scanner) (knowledge.Term, error) {
	var term knowledge.Term
	var created, updated int64
	err := row.Scan(&term.ID, &term.Name, &term.Description, &term.Version, &created, &updated)
	term.CreatedAt = time.UnixMilli(created).UTC()
	term.UpdatedAt = time.UnixMilli(updated).UTC()
	return term, err
}

func scanThought(row scanner) (knowledge.Thought, error) {
	var thought knowledge.Thought
	var created, updated int64
	err := row.Scan(&thought.ID, &thought.Thesis, &thought.Body, &thought.Version, &created, &updated)
	thought.CreatedAt = time.UnixMilli(created).UTC()
	thought.UpdatedAt = time.UnixMilli(updated).UTC()
	return thought, err
}

func (s *Store) taxonomyFor(ctx context.Context, joinTable, ownerColumn, ownerID, table, targetColumn string) ([]string, error) {
	return taxonomyFor(ctx, s.db, joinTable, ownerColumn, ownerID, table, targetColumn)
}
func taxonomyFor(ctx context.Context, db sqlReader, joinTable, ownerColumn, ownerID, table, targetColumn string) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT t.name FROM %s j JOIN %s t ON t.id = j.%s
		WHERE j.%s = ? ORDER BY t.name COLLATE NOCASE
	`, joinTable, table, targetColumn, ownerColumn)
	rows, err := db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func taxonomyTable(kind knowledge.TaxonomyKind) (string, error) {
	switch kind {
	case knowledge.TaxonomyTag:
		return "tags", nil
	default:
		return "", fmt.Errorf("unknown taxonomy kind %q", kind)
	}
}
