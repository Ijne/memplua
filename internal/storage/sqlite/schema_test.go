package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSchemaV4MigratesAreasToUniversalTags(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "v3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);
INSERT INTO schema_migrations VALUES (3, 0);
CREATE TABLE terms(id TEXT PRIMARY KEY);
INSERT INTO terms VALUES ('term-1');
INSERT INTO terms VALUES ('term-without-classification');
CREATE TABLE areas(id TEXT PRIMARY KEY, name TEXT UNIQUE COLLATE NOCASE, system INTEGER, enabled INTEGER, created_at INTEGER);
CREATE TABLE tags(id TEXT PRIMARY KEY, name TEXT UNIQUE COLLATE NOCASE, system INTEGER, enabled INTEGER, created_at INTEGER);
CREATE TABLE taxonomy_aliases(id TEXT PRIMARY KEY, kind TEXT, entry_id TEXT, alias TEXT, UNIQUE(kind,alias));
CREATE TABLE term_areas(term_id TEXT, area_id TEXT, PRIMARY KEY(term_id,area_id));
CREATE TABLE term_tags(term_id TEXT, tag_id TEXT, PRIMARY KEY(term_id,tag_id));
CREATE TABLE conspect_terms(value_json TEXT);
CREATE TABLE review_items(final_json TEXT, variants_json TEXT);
CREATE TABLE review_matches(value_json TEXT);
INSERT INTO areas VALUES ('area-mathematics','mathematics',1,1,1);
INSERT INTO taxonomy_aliases VALUES ('alias-area-math','area','area-mathematics','math');
INSERT INTO term_areas VALUES ('term-1','area-mathematics');
INSERT INTO conspect_terms VALUES ('{"name":"Topology","areas":["math"],"tags":[]}');
INSERT INTO review_items VALUES ('{"name":"Topology","areas":["math"],"tags":[]}', '[{"value":{"name":"Topology","areas":["math"],"tags":[]}}]');
INSERT INTO review_matches VALUES ('{"value":{"name":"Topology","areas":["math"],"tags":[]}}');
`)
	if err != nil {
		t.Fatal(err)
	}
	if err = migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 4 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	var tag string
	if err = db.QueryRow(`SELECT t.name FROM term_tags tt JOIN tags t ON t.id=tt.tag_id WHERE tt.term_id='term-1'`).Scan(&tag); err != nil || tag != "mathematics" {
		t.Fatalf("migrated tag=%q err=%v", tag, err)
	}
	if err = db.QueryRow(`SELECT t.name FROM taxonomy_aliases a JOIN tags t ON t.id=a.entry_id WHERE a.kind='tag' AND a.alias='math'`).Scan(&tag); err != nil || tag != "mathematics" {
		t.Fatalf("migrated alias target=%q err=%v", tag, err)
	}
	if err = db.QueryRow(`SELECT name FROM tags WHERE name='artificial-intelligence'`).Scan(&tag); err != nil {
		t.Fatalf("default domain tags were not seeded: %v", err)
	}
	if err = db.QueryRow(`SELECT t.name FROM term_tags tt JOIN tags t ON t.id=tt.tag_id WHERE tt.term_id='term-without-classification'`).Scan(&tag); err != nil || tag != "general" {
		t.Fatalf("unclassified term fallback=%q err=%v", tag, err)
	}
	if _, err = db.Exec(`SELECT 1 FROM areas`); err == nil {
		t.Fatal("obsolete areas table still exists")
	}
	for _, item := range []struct{ table, column, prefix string }{
		{"conspect_terms", "value_json", "$"},
		{"review_items", "final_json", "$"},
		{"review_matches", "value_json", "$.value"},
	} {
		var areas any
		if err = db.QueryRow(`SELECT json_extract(` + item.column + `,'` + item.prefix + `.areas') FROM ` + item.table).Scan(&areas); err != nil || areas != nil {
			t.Errorf("%s still contains areas: value=%v err=%v", item.table, areas, err)
		}
		var tags string
		if err = db.QueryRow(`SELECT json_extract(` + item.column + `,'` + item.prefix + `.tags') FROM ` + item.table).Scan(&tags); err != nil || tags != `["math"]` {
			t.Errorf("%s migrated tags=%q err=%v", item.table, tags, err)
		}
	}
	var variants string
	if err = db.QueryRow(`SELECT variants_json FROM review_items`).Scan(&variants); err != nil || variants != `[{"value":{"name":"Topology","tags":["math"]}}]` {
		t.Errorf("migrated variants=%q err=%v", variants, err)
	}
}

func TestFreshSchemaSeedsDomainTags(t *testing.T) {
	store := openTestStore(t)
	for _, name := range []string{"mathematics", "natural-sciences", "computer-science", "software-engineering", "artificial-intelligence", "business", "medicine", "history"} {
		if countRows(t, store, `SELECT COUNT(*) FROM tags WHERE name=? AND system=1 AND enabled=1`, name) != 1 {
			t.Errorf("missing default domain tag %q", name)
		}
	}
}
