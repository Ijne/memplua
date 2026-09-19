package sqlite

import (
	"context"
	"crawler/internal/app"
	"crawler/internal/id"
	"crawler/internal/ingest"
	"crawler/internal/review"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func testBatch(t *testing.T, s *Store) *ingest.AnalysisBatch {
	t.Helper()
	ctx := context.Background()
	submission, err := s.SubmitText(ctx, "source evidence")
	must(t, err)
	batches, err := s.ListAnalysisBatches(ctx, 200)
	must(t, err)
	for i := range batches {
		if batches[i].SourceSessionID == submission.SourceSessionID {
			return &batches[i]
		}
	}
	t.Fatal("missing batch")
	return nil
}
func testExtraction(chunkID string) ingest.Extraction {
	p := ingest.Provenance{EvidenceChunkIDs: []string{chunkID}, AnchorChunkID: chunkID}
	return ingest.Extraction{Conspects: []ingest.TopicExtraction{{Topic: "Topology", Terms: []ingest.TermCandidate{{Ref: "term-1", Name: "Topology", Description: "Shapes", Tags: []string{"math"}, Provenance: p}}, Thoughts: []ingest.ThoughtCandidate{{Thesis: "Continuity matters", Body: "draft", RelatedTermRefs: []string{"term-1"}, Tags: []string{"idea"}, Provenance: p}}}}}
}
func prepareTestConspect(t *testing.T, s *Store, modify func(*ingest.Extraction)) *review.Conspect {
	t.Helper()
	ctx := context.Background()
	batch := testBatch(t, s)
	extraction := testExtraction(batch.Input.Focus[0].ID)
	if modify != nil {
		modify(&extraction)
	}
	must(t, s.SaveBatchExtraction(ctx, batch.ID, extraction, "raw", "fake", "v1"))
	conspects, err := s.ListReviewConspects(ctx, 200)
	must(t, err)
	for _, c := range conspects {
		if c.AnalysisBatchID == batch.ID {
			must(t, (review.Preparer{Knowledge: s, Repository: s, Threshold: 0.3, Limit: 5}).Prepare(ctx, c.ID))
			got, err := s.GetReviewConspect(ctx, c.ID)
			must(t, err)
			return got
		}
	}
	t.Fatal("missing conspect")
	return nil
}
func resolveCreate(t *testing.T, s *Store, c *review.Conspect) {
	t.Helper()
	for _, item := range c.Items {
		must(t, s.SaveReviewDecision(context.Background(), item.ID, review.Decision{Resolution: review.Create}))
	}
}
func countRows(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	must(t, s.db.QueryRow(query, args...).Scan(&n))
	return n
}

func TestAtomicApplyGatesGraphAndAddsLinksProvenanceAndSingleExport(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c := prepareTestConspect(t, s, nil)
	if len(c.Items) != 2 || len(c.Taxonomy) != 0 {
		t.Fatalf("review = %+v", c)
	}
	for _, item := range c.Items {
		d := review.Decision{Resolution: review.Create}
		if item.Kind == review.ObjectThought {
			v := item.FinalValue
			v.Body = "user correction"
			d.FinalValue = &v
		}
		must(t, s.SaveReviewDecision(ctx, item.ID, d))
	}
	before, err := s.Snapshot(ctx)
	must(t, err)
	if before.Revision != 0 || len(before.Terms)+len(before.Thoughts) != 0 {
		t.Fatal("decision mutated canonical graph")
	}
	must(t, s.ApplyConspect(ctx, c.ID))
	must(t, s.ApplyConspect(ctx, c.ID))
	after, err := s.Snapshot(ctx)
	must(t, err)
	if after.Revision != 1 || len(after.Terms) != 1 || len(after.Thoughts) != 1 || len(after.Links) != 1 || after.Thoughts[0].Body != "user correction" {
		t.Fatalf("graph=%+v", after)
	}
	if after.Terms[0].Tags[0] != "mathematics" {
		t.Fatal("taxonomy alias was not resolved")
	}
	if countRows(t, s, `SELECT COUNT(*) FROM jobs WHERE kind='export'`) != 1 {
		t.Fatal("expected one export")
	}
	if countRows(t, s, `SELECT COUNT(*) FROM canonical_provenance`) != 2 {
		t.Fatal("missing provenance")
	}
	if countRows(t, s, `SELECT COUNT(DISTINCT graph_revision) FROM revisions`) != 1 {
		t.Fatal("more than one graph revision")
	}
	if countRows(t, s, `SELECT COUNT(*) FROM review_items WHERE status='applied'`) != 2 {
		t.Fatal("items not applied")
	}
}

func TestThoughtRelationshipsCanBeReviewedAndChangedWithinConspect(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c := prepareTestConspect(t, s, func(extraction *ingest.Extraction) {
		second := extraction.Conspects[0].Terms[0]
		second.Ref = "term-2"
		second.Name = "Geometry"
		second.Description = "Spatial relationships"
		extraction.Conspects[0].Terms = append(extraction.Conspects[0].Terms, second)
	})
	var thought, topology, geometry review.Item
	for _, item := range c.Items {
		switch item.Kind {
		case review.ObjectThought:
			thought = item
		case review.ObjectTerm:
			if item.FinalValue.Name == "Geometry" {
				geometry = item
			} else {
				topology = item
			}
		}
	}
	if len(thought.RelatedItemIDs) != 1 || thought.RelatedItemIDs[0] != topology.ID {
		t.Fatalf("initial relationships = %v, want topology item", thought.RelatedItemIDs)
	}
	related := []string{geometry.ID}
	must(t, s.SaveReviewDecision(ctx, thought.ID, review.Decision{Resolution: review.Create, RelatedItemIDs: &related}))
	for _, term := range []review.Item{topology, geometry} {
		must(t, s.SaveReviewDecision(ctx, term.ID, review.Decision{Resolution: review.Create}))
	}
	updated, err := s.GetReviewConspect(ctx, c.ID)
	must(t, err)
	for _, item := range updated.Items {
		if item.ID == thought.ID && (len(item.RelatedItemIDs) != 1 || item.RelatedItemIDs[0] != geometry.ID) {
			t.Fatalf("updated relationships = %v, want geometry item", item.RelatedItemIDs)
		}
	}
	must(t, s.ApplyConspect(ctx, c.ID))
	snapshot, err := s.Snapshot(ctx)
	must(t, err)
	if len(snapshot.Links) != 1 {
		t.Fatalf("links = %+v", snapshot.Links)
	}
	var geometryID string
	for _, term := range snapshot.Terms {
		if term.Name == "Geometry" {
			geometryID = term.ID
		}
	}
	if snapshot.Links[0].TermID != geometryID {
		t.Fatalf("link points to %q, want Geometry %q", snapshot.Links[0].TermID, geometryID)
	}
}

func TestReviewRejectsThoughtThatReferencesRejectedTerm(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c := prepareTestConspect(t, s, nil)
	var term, thought review.Item
	for _, item := range c.Items {
		if item.Kind == review.ObjectTerm {
			term = item
		} else {
			thought = item
		}
	}
	must(t, s.SaveReviewDecision(ctx, term.ID, review.Decision{Resolution: review.Reject}))
	err := s.SaveReviewDecision(ctx, thought.ID, review.Decision{Resolution: review.Create})
	if !errors.Is(err, review.ErrDependency) {
		t.Fatalf("thought with rejected term = %v, want dependency error", err)
	}
}

func TestRejectingTermReopensAcceptedDependentThought(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c := prepareTestConspect(t, s, nil)
	var term, thought review.Item
	for _, item := range c.Items {
		if item.Kind == review.ObjectTerm {
			term = item
		} else {
			thought = item
		}
	}
	must(t, s.SaveReviewDecision(ctx, thought.ID, review.Decision{Resolution: review.Create}))
	must(t, s.SaveReviewDecision(ctx, term.ID, review.Decision{Resolution: review.Reject}))
	updated, err := s.GetReviewConspect(ctx, c.ID)
	must(t, err)
	for _, item := range updated.Items {
		if item.ID == thought.ID && (item.Status != review.Pending || item.Resolution != "") {
			t.Fatalf("dependent thought was not reopened: %+v", item)
		}
	}
}

func TestExactMatchMustBeKeptOrOverwritten(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	first := prepareTestConspect(t, s, func(e *ingest.Extraction) { e.Conspects[0].Thoughts = nil })
	resolveCreate(t, s, first)
	must(t, s.ApplyConspect(ctx, first.ID))
	second := prepareTestConspect(t, s, func(e *ingest.Extraction) { e.Conspects[0].Thoughts = nil })
	item := second.Items[0]
	if len(item.CanonicalMatches) == 0 || !item.CanonicalMatches[0].Exact {
		t.Fatalf("missing exact match: %+v", item.CanonicalMatches)
	}
	for _, resolution := range []review.Resolution{review.Create, review.Reject} {
		err := s.SaveReviewDecision(ctx, item.ID, review.Decision{Resolution: resolution})
		if !errors.Is(err, review.ErrExactMatch) {
			t.Fatalf("%s exact match = %v, want exact-match error", resolution, err)
		}
	}
	target := item.CanonicalMatches[0]
	must(t, s.SaveReviewDecision(ctx, item.ID, review.Decision{Resolution: review.KeepExisting, TargetID: target.ID, TargetVersion: target.Version}))
}

func TestUpdatingSimilarTermReplacesTheTargetWithoutLeavingItsOldName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	original := prepareTestConspect(t, s, func(e *ingest.Extraction) { e.Conspects[0].Thoughts = nil })
	resolveCreate(t, s, original)
	must(t, s.ApplyConspect(ctx, original.ID))
	before, err := s.ListTerms(ctx)
	must(t, err)
	if len(before) != 1 {
		t.Fatalf("initial terms = %+v", before)
	}

	incoming := prepareTestConspect(t, s, func(e *ingest.Extraction) {
		e.Conspects[0].Thoughts = nil
		e.Conspects[0].Terms[0].Name = "Topology model"
		e.Conspects[0].Terms[0].Description = "Updated description"
	})
	item := incoming.Items[0]
	var target review.Match
	for _, match := range item.CanonicalMatches {
		if match.ID == before[0].ID && !match.Exact {
			target = match
			break
		}
	}
	if target.ID == "" {
		t.Fatalf("missing non-exact target: %+v", item.CanonicalMatches)
	}
	updated := item.FinalValue
	updated.Name = "Topological structure"
	must(t, s.SaveReviewDecision(ctx, item.ID, review.Decision{
		Resolution: review.Update, TargetID: target.ID, TargetVersion: target.Version, FinalValue: &updated,
	}))
	must(t, s.ApplyConspect(ctx, incoming.ID))

	after, err := s.ListTerms(ctx)
	must(t, err)
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].Name != "Topological structure" || after[0].Description != "Updated description" || after[0].Version != before[0].Version+1 {
		t.Fatalf("updated target = %+v, original = %+v", after, before[0])
	}
	old, err := s.FindTerm(ctx, before[0].Name)
	must(t, err)
	if old != nil {
		t.Fatalf("old canonical name still resolves: %+v", old)
	}
}

func TestApplyFailureRollsBackEverything(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c := prepareTestConspect(t, s, func(e *ingest.Extraction) { e.Conspects[0].Terms[0].Tags = []string{"new-label"} })
	resolveCreate(t, s, c)
	must(t, s.SaveTaxonomyDecision(ctx, c.Taxonomy[0].ID, review.TaxonomyDecision{Resolution: "create"}))
	_, err := s.db.Exec(`CREATE TRIGGER fail_thought BEFORE INSERT ON thoughts BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
	must(t, err)
	if err = s.ApplyConspect(ctx, c.ID); err == nil {
		t.Fatal("expected injected failure")
	}
	snapshot, err := s.Snapshot(ctx)
	must(t, err)
	if snapshot.Revision != 0 || len(snapshot.Terms) != 0 {
		t.Fatalf("partial graph=%+v", snapshot)
	}
	for _, table := range []string{"revisions", "canonical_provenance", "thought_terms"} {
		if countRows(t, s, "SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatalf("partial %s", table)
		}
	}
	if countRows(t, s, `SELECT COUNT(*) FROM tags WHERE name='new-label'`) != 0 || countRows(t, s, `SELECT COUNT(*) FROM jobs WHERE kind='export'`) != 0 {
		t.Fatal("partial taxonomy/export")
	}
	got, err := s.GetReviewConspect(ctx, c.ID)
	must(t, err)
	if got.Status != ingest.ConspectReadyToApply {
		t.Fatal("review state changed on rollback")
	}
	_, err = s.db.Exec(`DROP TRIGGER fail_thought`)
	must(t, err)
	must(t, s.ApplyConspect(ctx, c.ID))
}

func TestTaxonomyMapCreateRemoveAndRejectedOnlyLabels(t *testing.T) {
	for _, resolution := range []string{"map", "create", "remove", "reject"} {
		t.Run(resolution, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			c := prepareTestConspect(t, s, func(e *ingest.Extraction) {
				e.Conspects[0].Thoughts = nil
				e.Conspects[0].Terms[0].Tags = []string{"Astrophysics"}
			})
			d := review.Decision{Resolution: review.Create}
			if resolution == "reject" {
				d.Resolution = review.Reject
			}
			must(t, s.SaveReviewDecision(ctx, c.Items[0].ID, d))
			if resolution != "reject" {
				if err := s.ApplyConspect(ctx, c.ID); !errors.Is(err, review.ErrNotReady) {
					t.Fatalf("unknown taxonomy apply = %v", err)
				}
				td := review.TaxonomyDecision{Resolution: resolution}
				if resolution == "map" {
					td.TargetID = "tag-natural-sciences"
				}
				must(t, s.SaveTaxonomyDecision(ctx, c.Taxonomy[0].ID, td))
				if countRows(t, s, `SELECT COUNT(*) FROM tags WHERE name='astrophysics'`) != 0 {
					t.Fatal("decision wrote taxonomy")
				}
			}
			must(t, s.ApplyConspect(ctx, c.ID))
			snapshot, err := s.Snapshot(ctx)
			must(t, err)
			switch resolution {
			case "reject":
				if len(snapshot.Terms) != 0 || snapshot.Revision != 0 {
					t.Fatal("rejected item changed graph")
				}
			case "remove":
				if len(snapshot.Terms[0].Tags) != 0 {
					t.Fatal("label not removed")
				}
			case "map":
				if snapshot.Terms[0].Tags[0] != "natural-sciences" {
					t.Fatal("label not mapped")
				}
			case "create":
				if snapshot.Terms[0].Tags[0] != "astrophysics" {
					t.Fatal("label not created")
				}
			}
		})
	}
}

func TestVersionConflictRollsBackAndDurablyRematches(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	original := prepareTestConspect(t, s, nil)
	resolveCreate(t, s, original)
	must(t, s.ApplyConspect(ctx, original.ID))
	incoming := prepareTestConspect(t, s, nil)
	competitor := prepareTestConspect(t, s, nil)
	for _, c := range []*review.Conspect{incoming, competitor} {
		for _, item := range c.Items {
			if len(item.CanonicalMatches) == 0 {
				t.Fatal("missing exact match")
			}
			m := item.CanonicalMatches[0]
			must(t, s.SaveReviewDecision(ctx, item.ID, review.Decision{Resolution: review.Update, TargetID: m.ID, TargetVersion: m.Version}))
		}
	}
	must(t, s.ApplyConspect(ctx, competitor.ID))
	before, err := s.Snapshot(ctx)
	must(t, err)
	if err = s.ApplyConspect(ctx, incoming.ID); !errors.Is(err, review.ErrConflict) {
		t.Fatalf("apply conflict=%v", err)
	}
	after, err := s.Snapshot(ctx)
	must(t, err)
	if after.Revision != before.Revision {
		t.Fatal("conflict changed graph")
	}
	got, err := s.GetReviewConspect(ctx, incoming.ID)
	must(t, err)
	if got.Status != ingest.ConspectPreparingReview {
		t.Fatalf("status=%s", got.Status)
	}
	must(t, (review.Preparer{Knowledge: s, Repository: s, Threshold: 0.3, Limit: 5}).Prepare(ctx, incoming.ID))
	got, err = s.GetReviewConspect(ctx, incoming.ID)
	must(t, err)
	if got.Status != ingest.ConspectReviewPending || got.Items[0].Status != review.Pending || got.Items[0].CanonicalMatches[0].Version != 2 {
		t.Fatalf("rematched=%+v", got)
	}
	if countRows(t, s, `SELECT COUNT(*) FROM user_events WHERE type='conspect.apply_conflict'`) != 1 {
		t.Fatal("missing conflict event")
	}
}

func TestIncomingVariantsRemainEditableAndCanBeSplit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c := prepareTestConspect(t, s, func(e *ingest.Extraction) {
		e.Conspects[0].Thoughts = nil
		term := e.Conspects[0].Terms[0]
		term.Ref = "term-2"
		term.Description = "another meaning"
		e.Conspects[0].Terms = append(e.Conspects[0].Terms, term)
	})
	if len(c.Items) != 1 || len(c.Items[0].IncomingVariants) != 2 {
		t.Fatalf("variants lost: %+v", c.Items)
	}
	must(t, s.SplitReviewItem(ctx, c.Items[0].ID, []string{c.Items[0].IncomingVariants[1].CandidateID}))
	must(t, (review.Preparer{Knowledge: s, Repository: s, Threshold: 0.3, Limit: 5}).Prepare(ctx, c.ID))
	got, err := s.GetReviewConspect(ctx, c.ID)
	must(t, err)
	if len(got.Items) != 2 {
		t.Fatal("split was regrouped")
	}
	another := prepareTestConspect(t, s, nil)
	if len(another.Items[0].PendingMatches) == 0 {
		t.Fatal("missing pending review matches")
	}
}

func TestBatchTimelineHasNoFocusOverlapAndBoundedUnicodeContext(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	start := time.Now().UTC()
	must(t, s.SaveSourceSession(ctx, app.SourceSession{ID: "stream", SourceID: "fake", Kind: "test", State: app.SourceRunning, StartedAt: start}))
	// 20 minutes plus an incomplete tail: four full batches and one Stop flush.
	for n := 0; n <= 40; n++ {
		tx, err := s.db.BeginTx(ctx, nil)
		must(t, err)
		err = s.saveChunk(ctx, tx, ingest.Chunk{ID: fmt.Sprintf("chunk-%d", n), SourceSessionID: "stream", Text: stringsOfRunes(400)}, start.Add(time.Duration(n)*30*time.Second))
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		must(t, tx.Commit())
	}
	must(t, s.UpdateSourceSession(ctx, "stream", app.SourceStopped, nil))
	must(t, s.FinalizeSource(ctx, "stream"))
	must(t, s.SweepBatches(ctx, start.Add(time.Hour)))
	batches, err := s.ListAnalysisBatches(ctx, 100)
	must(t, err)
	if len(batches) != 5 {
		t.Fatalf("batches=%d", len(batches))
	}
	seen := map[string]bool{}
	for _, b := range batches {
		if b.Status != ingest.BatchReady {
			t.Fatalf("status=%s", b.Status)
		}
		runes := 0
		for _, c := range b.Input.Context {
			if !utf8.ValidString(c.Text) {
				t.Fatal("invalid Unicode")
			}
			runes += utf8.RuneCountInString(c.Text)
		}
		if runes > 400 {
			t.Fatal("context limit exceeded")
		}
		for _, c := range b.Input.Focus {
			if seen[c.ID] {
				t.Fatal("duplicate focus")
			}
			seen[c.ID] = true
		}
	}
	if len(seen) != 41 || countRows(t, s, `SELECT COUNT(*) FROM jobs WHERE kind='extract_analysis_batch'`) != 5 {
		t.Fatal("lost focus or duplicate extraction job")
	}
}

func TestContinuitySuffixKeepsOnlyUnfinishedBoundaryText(t *testing.T) {
	tests := []struct {
		input string
		want  string
		stop  bool
	}{
		{"Завершённая мысль. Незавершённый переход", " Незавершённый переход", true},
		{"Полностью завершено!", "", true},
		{"фрагмент без пунктуации", "фрагмент без пунктуации", false},
	}
	for _, test := range tests {
		got, stop := continuitySuffix(test.input)
		if string(got) != test.want || stop != test.stop {
			t.Fatalf("continuitySuffix(%q) = %q, %v; want %q, %v", test.input, string(got), stop, test.want, test.stop)
		}
	}
}
func stringsOfRunes(n int) string {
	r := make([]rune, n)
	for i := range r {
		r[i] = '界'
	}
	return string(r)
}

func TestIdleDeadlineFinalizeAndConcurrentCloseAreIdempotent(t *testing.T) {
	for _, reason := range []string{"idle", "deadline", "finalize", "stop"} {
		t.Run(reason, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			now := time.Now().UTC()
			must(t, s.SaveSourceSession(ctx, app.SourceSession{ID: "s", SourceID: "test", Kind: "test", State: app.SourceRunning, StartedAt: now}))
			must(t, s.SaveChunk(ctx, ingest.Chunk{ID: "c", SourceSessionID: "s", Text: "text"}))
			switch reason {
			case "idle":
				must(t, s.SweepBatches(ctx, now.Add(91*time.Second)))
			case "deadline":
				must(t, s.SweepBatches(ctx, now.Add(301*time.Second)))
			case "finalize":
				must(t, s.FinalizeSource(ctx, "s"))
			case "stop":
				must(t, s.UpdateSourceSession(ctx, "s", app.SourceStopped, nil))
			}
			var wg sync.WaitGroup
			for n := 0; n < 5; n++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := s.FinalizeSource(ctx, "s"); err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			if countRows(t, s, `SELECT COUNT(*) FROM jobs`) != 1 {
				t.Fatal("duplicate close jobs")
			}
			batches, err := s.ListAnalysisBatches(ctx, 10)
			must(t, err)
			want := reason
			if reason == "stop" {
				want = "source_stopped"
			}
			if batches[0].CloseReason != want {
				t.Fatalf("reason=%s", batches[0].CloseReason)
			}
		})
	}
}

func TestRecoveryPersistsChunksResponsesAndStableConspects(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "knowledge.db")
	s, err := Open(path)
	must(t, err)
	defer func() { s.Close() }()
	must(t, s.SaveSourceSession(ctx, app.SourceSession{ID: "s", SourceID: "test", Kind: "test", State: app.SourceRunning, StartedAt: time.Now()}))
	must(t, s.SaveChunk(ctx, ingest.Chunk{ID: "c", SourceSessionID: "s", Text: "text"}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	must(t, s.StartAppRun(ctx, app.AppRun{ID: id.New(), Version: "test", State: app.StateStarting, StartedAt: time.Now()}))
	batches, err := s.ListAnalysisBatches(ctx, 10)
	must(t, err)
	if len(batches) != 1 || batches[0].Status != ingest.BatchReady {
		t.Fatalf("recovery=%+v", batches)
	}
	job, err := s.Claim(ctx, "old", time.Hour)
	must(t, err)
	must(t, s.BeginExtraction(ctx, batches[0].ID))
	must(t, s.SaveBatchFailure(ctx, batches[0].ID, "malformed", "fake", "v1", errors.New("bad structure")))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	failed, err := s.GetAnalysisBatch(ctx, batches[0].ID)
	must(t, err)
	if failed.RawModelResponse != "malformed" || failed.Status != ingest.BatchFailed {
		t.Fatal("lost failed raw response")
	}
	must(t, s.StartAppRun(ctx, app.AppRun{ID: id.New(), Version: "test", State: app.StateStarting, StartedAt: time.Now()}))
	recovered, err := s.Claim(ctx, "new", time.Minute)
	must(t, err)
	if recovered.ID != job.ID {
		t.Fatal("lost lease")
	}
	e := testExtraction("c")
	must(t, s.SaveBatchExtraction(ctx, failed.ID, e, "raw", "fake", "v1"))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	must(t, s.SaveBatchExtraction(ctx, failed.ID, e, "raw", "fake", "v1"))
	if countRows(t, s, `SELECT COUNT(*) FROM conspects`) != 1 || countRows(t, s, `SELECT COUNT(*) FROM jobs WHERE kind='prepare_conspect_review'`) != 1 {
		t.Fatal("duplicated conspect")
	}
	var cid string
	must(t, s.db.QueryRow(`SELECT id FROM conspects`).Scan(&cid))
	preparer := review.Preparer{Knowledge: s, Repository: s, Threshold: .3, Limit: 5}
	must(t, preparer.Prepare(ctx, cid))
	before, err := s.GetReviewConspect(ctx, cid)
	must(t, err)
	must(t, preparer.Prepare(ctx, cid))
	after, err := s.GetReviewConspect(ctx, cid)
	must(t, err)
	if before.Items[0].ID != after.Items[0].ID {
		t.Fatal("duplicated review")
	}
}

func TestFreshDatabaseDoesNotImportOrDeleteLegacyFiles(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"knowledgecrawler.db", "knowledgecrawler.db-wal", "knowledgecrawler.db-shm"} {
		must(t, os.WriteFile(filepath.Join(directory, name), []byte("legacy"), 0600))
	}
	s, err := Open(filepath.Join(directory, "knowledge.db"))
	must(t, err)
	must(t, s.Close())
	for _, name := range []string{"knowledgecrawler.db", "knowledgecrawler.db-wal", "knowledgecrawler.db-shm"} {
		b, err := os.ReadFile(filepath.Join(directory, name))
		must(t, err)
		if string(b) != "legacy" {
			t.Fatalf("changed %s", name)
		}
	}
	legacy := filepath.Join(directory, "old-schema.db")
	db, err := sql.Open("sqlite", legacy)
	must(t, err)
	_, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at INTEGER);INSERT INTO schema_migrations VALUES (2,0)`)
	must(t, err)
	must(t, db.Close())
	if opened, err := Open(legacy); err == nil {
		opened.Close()
		t.Fatal("legacy schema was migrated")
	}
}

func TestBatchProducesThreeConspectsAndNoExtraJobs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	b := testBatch(t, s)
	e := testExtraction(b.Input.Focus[0].ID)
	e.Conspects = append(e.Conspects, e.Conspects[0], e.Conspects[0])
	for i := range e.Conspects {
		e.Conspects[i].Topic = fmt.Sprintf("topic-%d", i)
	}
	raw, _ := json.Marshal(e)
	must(t, s.SaveBatchExtraction(ctx, b.ID, e, string(raw), "fake", "v1"))
	must(t, s.SaveBatchExtraction(ctx, b.ID, e, string(raw), "fake", "v1"))
	if countRows(t, s, `SELECT COUNT(*) FROM conspects`) != 3 || countRows(t, s, `SELECT COUNT(*) FROM jobs WHERE kind='extract_analysis_batch'`) != 1 || countRows(t, s, `SELECT COUNT(*) FROM jobs WHERE kind='prepare_conspect_review'`) != 3 {
		t.Fatal("incorrect fanout")
	}
}

func TestKeepExistingPreservesValueAndRejectedEndpointsDoNotCreateLinks(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	original := prepareTestConspect(t, s, nil)
	resolveCreate(t, s, original)
	must(t, s.ApplyConspect(ctx, original.ID))
	incoming := prepareTestConspect(t, s, func(e *ingest.Extraction) {
		e.Conspects[0].Terms[0].Description = "Unwanted replacement"
		e.Conspects[0].Thoughts[0].Thesis = "Continuity may matter"
		e.Conspects[0].Thoughts[0].Tags = []string{"rejected-only"}
	})
	for _, item := range incoming.Items {
		d := review.Decision{Resolution: review.Reject}
		if item.Kind == review.ObjectTerm {
			target := item.CanonicalMatches[0]
			d = review.Decision{Resolution: review.KeepExisting, TargetID: target.ID, TargetVersion: target.Version, FinalValue: &review.Value{Name: "Unwanted"}}
		}
		must(t, s.SaveReviewDecision(ctx, item.ID, d))
	}
	must(t, s.ApplyConspect(ctx, incoming.ID))
	snapshot, err := s.Snapshot(ctx)
	must(t, err)
	if len(snapshot.Terms) != 1 || snapshot.Terms[0].Description != "Shapes" || snapshot.Terms[0].Version != 1 || len(snapshot.Links) != 1 {
		t.Fatalf("keep/reject changed values: %+v", snapshot)
	}
	if countRows(t, s, `SELECT COUNT(*) FROM canonical_provenance WHERE conspect_id=?`, incoming.ID) != 1 {
		t.Fatal("keep-existing provenance missing")
	}
	if countRows(t, s, `SELECT COUNT(*) FROM tags WHERE name='rejected-only'`) != 0 {
		t.Fatal("rejected-only taxonomy was created")
	}
}

func TestPrepareFailureAndManualRetryRestoreConspectState(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	batch := testBatch(t, s)
	must(t, s.SaveBatchExtraction(ctx, batch.ID, testExtraction(batch.Input.Focus[0].ID), "raw", "fake", "v1"))
	jobs, err := s.ListJobs(ctx, "", 20)
	must(t, err)
	var prepare ingest.Job
	for _, job := range jobs {
		if job.Kind == ingest.JobPrepareConspectReview {
			prepare = job
		}
	}
	var p ingest.ConspectPayload
	must(t, json.Unmarshal(prepare.Payload, &p))
	must(t, s.Fail(ctx, prepare.ID, errors.New("injected matcher failure")))
	c, err := s.GetConspect(ctx, p.ConspectID)
	must(t, err)
	if c.Status != ingest.ConspectFailed {
		t.Fatal("failed job did not mark conspect")
	}
	must(t, s.RetryFailedJob(ctx, prepare.ID))
	must(t, (review.Preparer{Knowledge: s, Repository: s, Threshold: .3, Limit: 5}).Prepare(ctx, c.ID))
	c, err = s.GetConspect(ctx, c.ID)
	must(t, err)
	if c.Status != ingest.ConspectReviewPending {
		t.Fatalf("retry status=%s", c.Status)
	}
}

func TestStaleCardDecisionQueuesFreshMatches(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	first := prepareTestConspect(t, s, nil)
	resolveCreate(t, s, first)
	must(t, s.ApplyConspect(ctx, first.ID))
	c := prepareTestConspect(t, s, nil)
	item := c.Items[0]
	target := item.CanonicalMatches[0]
	err := s.SaveReviewDecision(ctx, item.ID, review.Decision{Resolution: review.Update, TargetID: target.ID, TargetVersion: target.Version - 1})
	if !errors.Is(err, review.ErrConflict) {
		t.Fatalf("stale save=%v", err)
	}
	got, err := s.GetConspect(ctx, c.ID)
	must(t, err)
	if got.Status != ingest.ConspectPreparingReview {
		t.Fatal("stale card did not rematch")
	}
}
