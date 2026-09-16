package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crawler/internal/app"
	"crawler/internal/id"
	"crawler/internal/ingest"
	"crawler/internal/observability"
	"crawler/internal/review"
)

func TestDesktopSourceTextContainsEntireOrderedFocusWithoutContextOrIDs(t *testing.T) {
	server, _, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	session := id.New()
	now := time.Now().UTC()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(store.SaveSourceSession(ctx, app.SourceSession{ID: session, SourceID: "microphone", Kind: "audio/microphone", State: app.SourceRunning, StartedAt: now}))
	save := func(text string, sequence int64) string {
		chunk := ingest.Chunk{ID: id.New(), SourceSessionID: session, Sequence: sequence, CapturedAt: now.Add(time.Duration(sequence) * time.Second), Text: text, Status: ingest.ChunkReady}
		check(store.SaveChunk(ctx, chunk))
		return chunk.ID
	}
	save("private context tail", 1)
	check(store.FinalizeSource(ctx, session))
	save("First focus paragraph", 2)
	last := save("Second focus paragraph", 3)
	check(store.FinalizeSource(ctx, session))
	batches, err := store.ListAnalysisBatches(ctx, 20)
	check(err)
	var batch ingest.AnalysisBatch
	for _, candidate := range batches {
		if len(candidate.Input.Focus) == 2 {
			batch = candidate
		}
	}
	if batch.ID == "" || len(batch.Input.Context) == 0 {
		t.Fatalf("fixture missing context/focus: %+v", batches)
	}
	check(store.SaveBatchExtraction(ctx, batch.ID, ingest.Extraction{Conspects: []ingest.TopicExtraction{{Topic: "Focus", Terms: []ingest.TermCandidate{{Ref: "t", Name: "Term", Tags: []string{"math"}, Provenance: ingest.Provenance{EvidenceChunkIDs: []string{last}, AnchorChunkID: last}}}}}}, "secret raw model text", "model", "v1"))
	conspects, err := store.ListReviewConspects(ctx, 20)
	check(err)
	c := conspects[0]
	check((review.Preparer{Knowledge: store, Repository: store, Threshold: .3, Limit: 5}).Prepare(ctx, c.ID))
	response := callAPI(t, server.Handler(), "GET", "/api/v1/review/conspects/"+c.ID+"/source-text", nil, 200)
	var text map[string]any
	check(json.Unmarshal(response.Body.Bytes(), &text))
	if text["text"] != "First focus paragraph\n\nSecond focus paragraph" || text["source_kind"] != "audio/microphone" || len(text) != 4 {
		t.Fatalf("source text=%s", response.Body.String())
	}
	for _, forbidden := range []string{session, batch.ID, c.ID, last, "private context tail", "secret raw model text", "chunk_id", "session_id", "context"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("source text leaked %q", forbidden)
		}
	}
	detail := callAPI(t, server.Handler(), "GET", "/api/v1/review/conspects/"+c.ID+"?view=desktop", nil, 200)
	for _, forbidden := range []string{"analysis_batch_id", "source_session_id", "evidence_chunk_ids", "anchor_chunk_id", "secret raw model text", "\"score\""} {
		if strings.Contains(detail.Body.String(), forbidden) {
			t.Fatalf("detail leaked %q", forbidden)
		}
	}
	list := callAPI(t, server.Handler(), "GET", "/api/v1/review/conspects?view=desktop", nil, 200)
	if strings.Contains(list.Body.String(), "incoming_variants") || !strings.Contains(list.Body.String(), "unresolved_items") {
		t.Fatalf("list is not compact: %s", list.Body.String())
	}
}

func TestDesktopStateCountsPendingEntitiesAndDoesNotExposeRawErrors(t *testing.T) {
	server, events, store, cleanup := newTestServer(t)
	defer cleanup()
	c := apiConspect(t, store)
	events.Publish(context.Background(), observability.Event{Type: "model.configuration_required", Severity: observability.SeverityWarning, Message: "secret C:/private/model.gguf", Data: map[string]any{"error": "raw native failure"}})
	response := callAPI(t, server.Handler(), "GET", "/api/v1/ui/state", nil, 200)
	var state struct {
		PendingConspects int `json:"pending_conspects"`
		PendingItems     int `json:"pending_items"`
		Warning          struct {
			Code string `json:"code"`
		} `json:"warning"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.PendingConspects != 1 || state.PendingItems != len(c.Items) || state.Warning.Code != "model.unavailable" {
		t.Fatalf("state=%s", response.Body.String())
	}
	for _, forbidden := range []string{"secret", "native failure", "queues", "attempts", "last_error"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("state leaked %q", forbidden)
		}
	}
	callAPI(t, server.Handler(), "PUT", "/api/v1/review/items/"+c.Items[0].ID, map[string]string{"resolution": "reject"}, 204)
	response = callAPI(t, server.Handler(), "GET", "/api/v1/ui/state", nil, 200)
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.PendingItems != 0 {
		t.Fatal("resolved entity still counted as pending")
	}
}

func TestDesktopEventsReplaySafeCodes(t *testing.T) {
	server, events, _, cleanup := newTestServer(t)
	defer cleanup()
	events.Publish(context.Background(), observability.Event{ID: "safe-event", Type: "model.configuration_required", Severity: observability.SeverityWarning, Message: "secret-path", Data: map[string]any{"error": "native stack"}})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/api/v1/events?view=desktop", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		frame.WriteString(line)
		if line == "\n" || err != nil {
			break
		}
	}
	cancel()
	value := frame.String()
	if !strings.Contains(value, "event: user") || !strings.Contains(value, `"code":"model.unavailable"`) || !strings.Contains(value, `"audience":"user"`) {
		t.Fatalf("frame=%s", value)
	}
	if strings.Contains(value, "secret-path") || strings.Contains(value, "native stack") {
		t.Fatalf("unsafe event=%s", value)
	}
}

func TestSettingsRestartKeysOnlyReportChangedRuntimeSettings(t *testing.T) {
	server, _, _, cleanup := newTestServer(t)
	defer cleanup()
	response := callAPI(t, server.Handler(), "PATCH", "/api/v1/settings", map[string]any{"ui_language": "en", "conspect_language": "en", "theme": "dark"}, 200)
	var result struct {
		Restart bool     `json:"restart_required"`
		Keys    []string `json:"restart_keys"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Restart || len(result.Keys) != 0 {
		t.Fatalf("live settings require restart: %s", response.Body.String())
	}
	response = callAPI(t, server.Handler(), "PATCH", "/api/v1/settings", map[string]any{"model_parallel": 3}, 200)
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Restart || len(result.Keys) != 1 || result.Keys[0] != "models.parallel" {
		t.Fatalf("restart keys=%s", response.Body.String())
	}
	response = callAPI(t, server.Handler(), "PATCH", "/api/v1/settings", map[string]any{"model_parallel": 3}, 200)
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Restart {
		t.Fatal("unchanged setting requires restart")
	}
}
