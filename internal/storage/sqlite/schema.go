package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 4

// This generation intentionally uses a new database. Legacy databases are never imported.
func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version == schemaVersion {
		return tx.Commit()
	}
	if version == 3 {
		if _, err = tx.ExecContext(ctx, schemaV4Migration); err != nil {
			return fmt.Errorf("migrate schema 3 to 4: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (4,unixepoch('subsec')*1000)`); err != nil {
			return err
		}
		return tx.Commit()
	}
	if version != 0 {
		return fmt.Errorf("database schema %d is not supported; select a new storage.database (knowledge.db); legacy data is not migrated", version)
	}
	if _, err = tx.ExecContext(ctx, schemaV1); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (?,unixepoch('subsec')*1000)`, schemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}

const schemaV1 = `
CREATE TABLE app_runs (
    id TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    state TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    stopped_at INTEGER,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE source_sessions (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    state TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    stopped_at INTEGER,
    last_error TEXT NOT NULL DEFAULT '',
    next_sequence INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX source_sessions_state_idx ON source_sessions(state);

CREATE TABLE chunks (
    id TEXT PRIMARY KEY,
    source_session_id TEXT NOT NULL REFERENCES source_sessions(id),
    sequence INTEGER NOT NULL,
    captured_at INTEGER NOT NULL,
    text TEXT NOT NULL,
    status TEXT NOT NULL,
    UNIQUE(source_session_id, sequence)
);
CREATE INDEX chunks_session_sequence_idx ON chunks(source_session_id, sequence);

CREATE TABLE analysis_batches (
 id TEXT PRIMARY KEY,
 source_session_id TEXT NOT NULL REFERENCES source_sessions(id),
 status TEXT NOT NULL CHECK(status IN ('collecting','ready','extracting','extracted','failed')),
 started_at INTEGER NOT NULL, last_text_at INTEGER NOT NULL, closed_at INTEGER,
 close_reason TEXT NOT NULL DEFAULT '',
 raw_model_response TEXT NOT NULL DEFAULT '', normalized_response TEXT NOT NULL DEFAULT '',
 model TEXT NOT NULL DEFAULT '', prompt_version TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX one_collecting_batch ON analysis_batches(source_session_id) WHERE status = 'collecting';
CREATE TABLE analysis_batch_chunks (
 batch_id TEXT NOT NULL REFERENCES analysis_batches(id), chunk_id TEXT NOT NULL REFERENCES chunks(id),
 role TEXT NOT NULL CHECK(role IN ('focus','context')), position INTEGER NOT NULL, text TEXT NOT NULL,
 PRIMARY KEY(batch_id,chunk_id)
);
CREATE UNIQUE INDEX one_focus_per_chunk ON analysis_batch_chunks(chunk_id) WHERE role = 'focus';
CREATE TABLE conspects (
 id TEXT PRIMARY KEY, analysis_batch_id TEXT NOT NULL REFERENCES analysis_batches(id),
 source_session_id TEXT NOT NULL REFERENCES source_sessions(id), position INTEGER NOT NULL,
 topic TEXT NOT NULL, created_at INTEGER NOT NULL, status TEXT NOT NULL, UNIQUE(analysis_batch_id,position)
);
CREATE TABLE conspect_terms (
 id TEXT PRIMARY KEY, conspect_id TEXT NOT NULL REFERENCES conspects(id), position INTEGER NOT NULL,
 ref TEXT NOT NULL, value_json TEXT NOT NULL, UNIQUE(conspect_id,ref)
);
CREATE TABLE conspect_thoughts (
 id TEXT PRIMARY KEY, conspect_id TEXT NOT NULL REFERENCES conspects(id), position INTEGER NOT NULL, value_json TEXT NOT NULL
);
CREATE TABLE conspect_links (
 conspect_id TEXT NOT NULL REFERENCES conspects(id), thought_id TEXT NOT NULL REFERENCES conspect_thoughts(id),
 term_id TEXT NOT NULL REFERENCES conspect_terms(id), PRIMARY KEY(thought_id,term_id)
);
CREATE TABLE candidate_provenance (
 candidate_id TEXT NOT NULL, chunk_id TEXT NOT NULL REFERENCES chunks(id), is_anchor INTEGER NOT NULL,
 PRIMARY KEY(candidate_id,chunk_id)
);
CREATE TABLE review_items (
 id TEXT PRIMARY KEY, conspect_id TEXT NOT NULL REFERENCES conspects(id), kind TEXT NOT NULL CHECK(kind IN ('term','thought')),
 position INTEGER NOT NULL, variants_json TEXT NOT NULL, resolution TEXT NOT NULL DEFAULT '', target_id TEXT NOT NULL DEFAULT '',
 target_version INTEGER NOT NULL DEFAULT 0, final_json TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending'
);
CREATE INDEX review_items_pending ON review_items(status,conspect_id);
CREATE TABLE review_matches (
 item_id TEXT NOT NULL REFERENCES review_items(id) ON DELETE CASCADE, position INTEGER NOT NULL, value_json TEXT NOT NULL,
 PRIMARY KEY(item_id,position)
);
CREATE TABLE review_taxonomy_resolutions (
 id TEXT PRIMARY KEY, conspect_id TEXT NOT NULL REFERENCES conspects(id), kind TEXT NOT NULL,
 value TEXT NOT NULL, resolution TEXT NOT NULL DEFAULT '', target_id TEXT NOT NULL DEFAULT '',
 UNIQUE(conspect_id,kind,value)
);
CREATE TABLE canonical_provenance (
 conspect_id TEXT NOT NULL REFERENCES conspects(id), review_item_id TEXT NOT NULL REFERENCES review_items(id),
 object_kind TEXT NOT NULL, object_id TEXT NOT NULL, candidate_id TEXT NOT NULL, chunk_id TEXT NOT NULL REFERENCES chunks(id),
 is_anchor INTEGER NOT NULL, graph_revision INTEGER NOT NULL,
 PRIMARY KEY(review_item_id,candidate_id,chunk_id)
);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at INTEGER NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX jobs_claim_idx ON jobs(status, available_at, lease_until, created_at);

CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE COLLATE NOCASE,
    system INTEGER NOT NULL,
    enabled INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE taxonomy_aliases (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    alias TEXT NOT NULL COLLATE NOCASE,
    UNIQUE(kind, alias)
);

CREATE TABLE terms (
    id TEXT PRIMARY KEY,
    canonical_name TEXT NOT NULL COLLATE NOCASE,
    description TEXT NOT NULL,
    version INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE thoughts (
    id TEXT PRIMARY KEY,
    thesis TEXT NOT NULL COLLATE NOCASE,
    body TEXT NOT NULL,
    version INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE term_tags (
    term_id TEXT NOT NULL REFERENCES terms(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id),
    PRIMARY KEY(term_id, tag_id)
);
CREATE TABLE thought_tags (
    thought_id TEXT NOT NULL REFERENCES thoughts(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id),
    PRIMARY KEY(thought_id, tag_id)
);
CREATE TABLE thought_terms (
    id TEXT PRIMARY KEY,
    thought_id TEXT NOT NULL REFERENCES thoughts(id) ON DELETE CASCADE,
    term_id TEXT NOT NULL REFERENCES terms(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    UNIQUE(thought_id, term_id)
);

CREATE TABLE graph_meta (id INTEGER PRIMARY KEY CHECK(id = 1), revision INTEGER NOT NULL);
INSERT INTO graph_meta(id, revision) VALUES (1, 0);
CREATE TABLE revisions (
    id TEXT PRIMARY KEY,
    graph_revision INTEGER NOT NULL,
    object_kind TEXT NOT NULL,
    object_id TEXT NOT NULL,
    review_item_id TEXT REFERENCES review_items(id),
    conspect_id TEXT NOT NULL REFERENCES conspects(id),
    snapshot_json TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE user_events (
    id TEXT PRIMARY KEY,
    occurred_at INTEGER NOT NULL,
    severity TEXT NOT NULL,
    type TEXT NOT NULL,
    message TEXT NOT NULL,
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    data_json TEXT NOT NULL DEFAULT '{}',
    is_read INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX user_events_time_idx ON user_events(occurred_at, id);

INSERT INTO tags(id, name, system, enabled, created_at) VALUES
    ('tag-general', 'general', 1, 1, unixepoch('subsec') * 1000),
    ('tag-mathematics', 'mathematics', 1, 1, unixepoch('subsec') * 1000),
    ('tag-natural-sciences', 'natural-sciences', 1, 1, unixepoch('subsec') * 1000),
    ('tag-computer-science', 'computer-science', 1, 1, unixepoch('subsec') * 1000),
    ('tag-software-engineering', 'software-engineering', 1, 1, unixepoch('subsec') * 1000),
    ('tag-artificial-intelligence', 'artificial-intelligence', 1, 1, unixepoch('subsec') * 1000),
    ('tag-data', 'data', 1, 1, unixepoch('subsec') * 1000),
    ('tag-technology', 'technology', 1, 1, unixepoch('subsec') * 1000),
    ('tag-business', 'business', 1, 1, unixepoch('subsec') * 1000),
    ('tag-economics', 'economics', 1, 1, unixepoch('subsec') * 1000),
    ('tag-finance', 'finance', 1, 1, unixepoch('subsec') * 1000),
    ('tag-law', 'law', 1, 1, unixepoch('subsec') * 1000),
    ('tag-medicine', 'medicine', 1, 1, unixepoch('subsec') * 1000),
    ('tag-psychology', 'psychology', 1, 1, unixepoch('subsec') * 1000),
    ('tag-education', 'education', 1, 1, unixepoch('subsec') * 1000),
    ('tag-history', 'history', 1, 1, unixepoch('subsec') * 1000),
    ('tag-philosophy', 'philosophy', 1, 1, unixepoch('subsec') * 1000),
    ('tag-linguistics', 'linguistics', 1, 1, unixepoch('subsec') * 1000),
    ('tag-arts', 'arts', 1, 1, unixepoch('subsec') * 1000),
    ('tag-society', 'society', 1, 1, unixepoch('subsec') * 1000),
    ('tag-personal', 'personal', 1, 1, unixepoch('subsec') * 1000),
    ('tag-learning', 'learning', 1, 1, unixepoch('subsec') * 1000),
    ('tag-work', 'work', 1, 1, unixepoch('subsec') * 1000),
    ('tag-idea', 'idea', 1, 1, unixepoch('subsec') * 1000),
    ('tag-decision', 'decision', 1, 1, unixepoch('subsec') * 1000),
    ('tag-task', 'task', 1, 1, unixepoch('subsec') * 1000),
    ('tag-problem', 'problem', 1, 1, unixepoch('subsec') * 1000),
    ('tag-process', 'process', 1, 1, unixepoch('subsec') * 1000),
    ('tag-comparison', 'comparison', 1, 1, unixepoch('subsec') * 1000),
    ('tag-generated', 'generated', 1, 1, unixepoch('subsec') * 1000);
INSERT INTO taxonomy_aliases(id, kind, entry_id, alias) VALUES
    ('alias-tag-math', 'tag', 'tag-mathematics', 'math'),
    ('alias-tag-science', 'tag', 'tag-natural-sciences', 'science'),
    ('alias-tag-programming', 'tag', 'tag-software-engineering', 'programming'),
    ('alias-tag-ai', 'tag', 'tag-artificial-intelligence', 'ai'),
    ('alias-tag-ml', 'tag', 'tag-artificial-intelligence', 'machine-learning');
`

// schemaV4Migration folds the former areas taxonomy into universal tags. Existing
// canonical term classifications are retained as tags; no user knowledge is dropped.
const schemaV4Migration = `
INSERT OR IGNORE INTO tags(id,name,system,enabled,created_at)
SELECT id,name,system,enabled,created_at FROM areas;
INSERT OR IGNORE INTO term_tags(term_id,tag_id)
SELECT ta.term_id,t.id FROM term_areas ta
JOIN areas a ON a.id=ta.area_id JOIN tags t ON t.name=a.name COLLATE NOCASE;
INSERT OR IGNORE INTO taxonomy_aliases(id,kind,entry_id,alias)
SELECT 'migrated-'||a.id,'tag',t.id,a.alias FROM taxonomy_aliases a
JOIN areas old ON old.id=a.entry_id JOIN tags t ON t.name=old.name COLLATE NOCASE
WHERE a.kind='area';

UPDATE conspect_terms SET value_json=json_remove(
    json_set(value_json,'$.tags',json((
        SELECT json_group_array(value) FROM (
            SELECT value FROM json_each(COALESCE(json_extract(conspect_terms.value_json,'$.tags'),'[]'))
            UNION
            SELECT value FROM json_each(COALESCE(json_extract(conspect_terms.value_json,'$.areas'),'[]'))
        )
    ))),'$.areas')
WHERE json_type(value_json,'$.areas')='array';
UPDATE review_items SET final_json=json_remove(
    json_set(final_json,'$.tags',json((
        SELECT json_group_array(value) FROM (
            SELECT value FROM json_each(COALESCE(json_extract(review_items.final_json,'$.tags'),'[]'))
            UNION
            SELECT value FROM json_each(COALESCE(json_extract(review_items.final_json,'$.areas'),'[]'))
        )
    ))),'$.areas')
WHERE json_type(final_json,'$.areas')='array';
UPDATE review_items SET variants_json=(
    SELECT json_group_array(json(converted)) FROM (
        SELECT json_remove(json_set(v.value,'$.value.tags',json((
            SELECT json_group_array(value) FROM (
                SELECT value FROM json_each(COALESCE(json_extract(v.value,'$.value.tags'),'[]'))
                UNION
                SELECT value FROM json_each(COALESCE(json_extract(v.value,'$.value.areas'),'[]'))
            )
        ))),'$.value.areas') AS converted
        FROM json_each(review_items.variants_json) v
    )
)
WHERE EXISTS (SELECT 1 FROM json_each(variants_json) WHERE json_type(value,'$.value.areas')='array');
UPDATE review_matches SET value_json=json_remove(
    json_set(value_json,'$.value.tags',json((
        SELECT json_group_array(value) FROM (
            SELECT value FROM json_each(COALESCE(json_extract(review_matches.value_json,'$.value.tags'),'[]'))
            UNION
            SELECT value FROM json_each(COALESCE(json_extract(review_matches.value_json,'$.value.areas'),'[]'))
        )
    ))),'$.value.areas')
WHERE json_type(value_json,'$.value.areas')='array';

DELETE FROM taxonomy_aliases WHERE kind='area';
DROP TABLE term_areas;
DROP TABLE areas;

INSERT OR IGNORE INTO tags(id,name,system,enabled,created_at) VALUES
    ('tag-general', 'general', 1, 1, unixepoch('subsec') * 1000),
    ('tag-mathematics', 'mathematics', 1, 1, unixepoch('subsec') * 1000),
    ('tag-natural-sciences', 'natural-sciences', 1, 1, unixepoch('subsec') * 1000),
    ('tag-computer-science', 'computer-science', 1, 1, unixepoch('subsec') * 1000),
    ('tag-software-engineering', 'software-engineering', 1, 1, unixepoch('subsec') * 1000),
    ('tag-artificial-intelligence', 'artificial-intelligence', 1, 1, unixepoch('subsec') * 1000),
    ('tag-data', 'data', 1, 1, unixepoch('subsec') * 1000),
    ('tag-technology', 'technology', 1, 1, unixepoch('subsec') * 1000),
    ('tag-business', 'business', 1, 1, unixepoch('subsec') * 1000),
    ('tag-economics', 'economics', 1, 1, unixepoch('subsec') * 1000),
    ('tag-finance', 'finance', 1, 1, unixepoch('subsec') * 1000),
    ('tag-law', 'law', 1, 1, unixepoch('subsec') * 1000),
    ('tag-medicine', 'medicine', 1, 1, unixepoch('subsec') * 1000),
    ('tag-psychology', 'psychology', 1, 1, unixepoch('subsec') * 1000),
    ('tag-education', 'education', 1, 1, unixepoch('subsec') * 1000),
    ('tag-history', 'history', 1, 1, unixepoch('subsec') * 1000),
    ('tag-philosophy', 'philosophy', 1, 1, unixepoch('subsec') * 1000),
    ('tag-linguistics', 'linguistics', 1, 1, unixepoch('subsec') * 1000),
    ('tag-arts', 'arts', 1, 1, unixepoch('subsec') * 1000),
    ('tag-society', 'society', 1, 1, unixepoch('subsec') * 1000),
    ('tag-personal', 'personal', 1, 1, unixepoch('subsec') * 1000),
    ('tag-learning', 'learning', 1, 1, unixepoch('subsec') * 1000),
    ('tag-work', 'work', 1, 1, unixepoch('subsec') * 1000),
    ('tag-idea', 'idea', 1, 1, unixepoch('subsec') * 1000),
    ('tag-decision', 'decision', 1, 1, unixepoch('subsec') * 1000),
    ('tag-task', 'task', 1, 1, unixepoch('subsec') * 1000),
    ('tag-problem', 'problem', 1, 1, unixepoch('subsec') * 1000),
    ('tag-process', 'process', 1, 1, unixepoch('subsec') * 1000),
    ('tag-comparison', 'comparison', 1, 1, unixepoch('subsec') * 1000),
    ('tag-generated', 'generated', 1, 1, unixepoch('subsec') * 1000);
INSERT OR IGNORE INTO taxonomy_aliases(id,kind,entry_id,alias) VALUES
    ('alias-tag-math', 'tag', (SELECT id FROM tags WHERE name='mathematics'), 'math'),
    ('alias-tag-science', 'tag', (SELECT id FROM tags WHERE name='natural-sciences'), 'science'),
    ('alias-tag-programming', 'tag', (SELECT id FROM tags WHERE name='software-engineering'), 'programming'),
    ('alias-tag-ai', 'tag', (SELECT id FROM tags WHERE name='artificial-intelligence'), 'ai'),
    ('alias-tag-ml', 'tag', (SELECT id FROM tags WHERE name='artificial-intelligence'), 'machine-learning');
INSERT OR IGNORE INTO term_tags(term_id,tag_id)
SELECT terms.id,(SELECT id FROM tags WHERE name='general') FROM terms
WHERE NOT EXISTS (SELECT 1 FROM term_tags WHERE term_tags.term_id=terms.id);
`
