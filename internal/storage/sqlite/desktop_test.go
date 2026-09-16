package sqlite

import (
	"context"
	"crawler/internal/ingest"
	"crawler/internal/review"
	"testing"
)

func TestRenamedTaxonomyDraftNormalizesMergesAndDoesNotMutateGraph(t *testing.T) {
	for _, name := range []string{"  New   Label  ", " MATH "} {
		t.Run(name, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			c := prepareTestConspect(t, s, func(e *ingest.Extraction) {
				e.Conspects[0].Terms[0].Tags = []string{"unknown"}
				e.Conspects[0].Thoughts[0].Tags = []string{"unknown"}
			})
			resolveCreate(t, s, c)
			if len(c.Taxonomy) != 1 {
				t.Fatalf("taxonomy=%+v", c.Taxonomy)
			}
			must(t, s.SaveTaxonomyDecision(ctx, c.Taxonomy[0].ID, review.TaxonomyDecision{Resolution: "create", Value: &name}))
			updated, err := s.GetReviewConspect(ctx, c.ID)
			must(t, err)
			expected := "new label"
			if name == " MATH " {
				expected = "mathematics"
			}
			for _, item := range updated.Items {
				if len(item.FinalValue.Tags) != 1 || item.FinalValue.Tags[0] != expected {
					t.Fatalf("tags=%v", item.FinalValue.Tags)
				}
			}
			before, err := s.Snapshot(ctx)
			must(t, err)
			if before.Revision != 0 || len(before.Terms) > 0 {
				t.Fatal("taxonomy autosave mutated canonical graph")
			}
			must(t, s.ApplyConspect(ctx, c.ID))
			after, err := s.Snapshot(ctx)
			must(t, err)
			if len(after.Terms) != 1 || after.Terms[0].Tags[0] != expected {
				t.Fatalf("applied=%+v", after.Terms)
			}
			if countRows(t, s, `SELECT COUNT(*) FROM tags WHERE name=?`, expected) != 1 {
				t.Fatal("created duplicate tag")
			}
		})
	}
}

func TestTaxonomyRenameRejectsEmptyAndMapsUnicodeAliases(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c := prepareTestConspect(t, s, func(e *ingest.Extraction) { e.Conspects[0].Terms[0].Tags = []string{"unknown"} })
	var target string
	must(t, s.db.QueryRow(`SELECT id FROM tags WHERE name='mathematics'`).Scan(&target))
	_, err := s.db.Exec(`INSERT INTO taxonomy_aliases(id,kind,entry_id,alias) VALUES ('alias-test','tag',?,'Математика')`, target)
	must(t, err)
	empty := " \t "
	if err = s.SaveTaxonomyDecision(ctx, c.Taxonomy[0].ID, review.TaxonomyDecision{Resolution: "create", Value: &empty}); err == nil {
		t.Fatal("accepted empty tag")
	}
	alias := " МАТЕМАТИКА "
	must(t, s.SaveTaxonomyDecision(ctx, c.Taxonomy[0].ID, review.TaxonomyDecision{Resolution: "create", Value: &alias}))
	updated, err := s.GetReviewConspect(ctx, c.ID)
	must(t, err)
	if updated.Taxonomy[0].Resolution != "map" || updated.Taxonomy[0].TargetID != target {
		t.Fatalf("alias mapping=%+v", updated.Taxonomy)
	}
}
