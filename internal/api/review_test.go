package api_test

import (
	"context"
	"crawler/internal/ingest"
	"crawler/internal/review"
	"crawler/internal/storage/sqlite"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func apiConspect(t *testing.T, s *sqlite.Store) *review.Conspect {
	t.Helper()
	ctx := context.Background()
	submission, err := s.SubmitText(ctx, "Evidence")
	if err != nil {
		t.Fatal(err)
	}
	batches, err := s.ListAnalysisBatches(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	p := ingest.Provenance{EvidenceChunkIDs: []string{submission.ChunkID}, AnchorChunkID: submission.ChunkID}
	extraction := ingest.Extraction{Conspects: []ingest.TopicExtraction{{Topic: "Topic", Terms: []ingest.TermCandidate{{Ref: "term", Name: "API term", Description: "Definition", Tags: []string{"unknown"}, Provenance: p}}}}}
	if err = s.SaveBatchExtraction(ctx, batches[0].ID, extraction, "raw", "model", "v1"); err != nil {
		t.Fatal(err)
	}
	conspects, err := s.ListReviewConspects(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err = (review.Preparer{Knowledge: s, Repository: s, Threshold: .3, Limit: 5}).Prepare(ctx, conspects[0].ID); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetReviewConspect(ctx, conspects[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func callAPI(t *testing.T, h http.Handler, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	encoded, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	r.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}
func TestStructuredReviewAPIContract(t *testing.T) {
	server, _, store, cleanup := newTestServer(t)
	defer cleanup()
	h := server.Handler()
	c := apiConspect(t, store)
	callAPI(t, h, "GET", "/api/v1/review/conspects", nil, 200)
	callAPI(t, h, "GET", "/api/v1/review/conspects/"+c.ID, nil, 200)
	callAPI(t, h, "GET", "/api/v1/review/conspects/"+c.ID+"/chunks", nil, 200)
	callAPI(t, h, "GET", "/api/v1/review/conspects/missing", nil, 404)
	callAPI(t, h, "POST", "/api/v1/review/conspects/"+c.ID+"/apply", nil, 409)
	itemPath := "/api/v1/review/items/" + c.Items[0].ID
	callAPI(t, h, "PUT", itemPath, map[string]any{"resolution": "create", "final_value": map[string]any{"name": "Term", "id": "injected"}}, 400)
	callAPI(t, h, "PUT", itemPath, map[string]any{"resolution": "delete"}, 400)
	callAPI(t, h, "PUT", itemPath, map[string]any{"resolution": "create"}, 204)
	snapshot, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 {
		t.Fatal("card decision changed graph")
	}
	callAPI(t, h, "POST", "/api/v1/review/conspects/"+c.ID+"/apply", nil, 409)
	callAPI(t, h, "PUT", "/api/v1/review/taxonomy/"+c.Taxonomy[0].ID, map[string]any{"resolution": "map", "target_id": "missing"}, 400)
	callAPI(t, h, "PUT", "/api/v1/review/taxonomy/"+c.Taxonomy[0].ID, map[string]any{"resolution": "create"}, 204)
	callAPI(t, h, "POST", "/api/v1/review/conspects/"+c.ID+"/apply", nil, 204)
	callAPI(t, h, "POST", "/api/v1/review/conspects/"+c.ID+"/apply", nil, 204)
	result := callAPI(t, h, "GET", "/api/v1/graph/search?kind=term&q=API&limit=1", nil, 200)
	var matches []review.Match
	if err = json.Unmarshal(result.Body.Bytes(), &matches); err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Version != 1 {
		t.Fatalf("search=%+v", matches)
	}
	callAPI(t, h, "GET", "/api/v1/graph/search?kind=thought_term", nil, 400)
	callAPI(t, h, "GET", "/api/v1/developer/analysis-batches", nil, 200)
	callAPI(t, h, "GET", "/api/v1/developer/analysis-batches/"+c.AnalysisBatchID, nil, 200)
	callAPI(t, h, "POST", "/api/v1/source-sessions/"+c.SourceSessionID+"/finalize", nil, 204)
	callAPI(t, h, "POST", "/api/v1/source-sessions/missing/finalize", nil, 404)
	callAPI(t, h, "POST", "/api/v1/review/operations/old/decision", map[string]string{"decision": "approve"}, 405)
}

func TestNewReviewEndpointsRequireAuthentication(t *testing.T) {
	server, _, _, cleanup := newTestServer(t)
	defer cleanup()
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/review/conspects"}, {"PUT", "/api/v1/review/items/id"}, {"PUT", "/api/v1/review/taxonomy/id"}, {"POST", "/api/v1/review/conspects/id/apply"}, {"POST", "/api/v1/source-sessions/id/finalize"}, {"GET", "/api/v1/graph/search?kind=term"}, {"GET", "/api/v1/developer/analysis-batches"}} {
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s = %d", route.path, w.Code)
		}
	}
}
