package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crawler/internal/api"
	"crawler/internal/app"
	"crawler/internal/config"
	"crawler/internal/observability"
	"crawler/internal/review"
	"crawler/internal/storage/sqlite"
)

func TestAuthenticationAndHealthBoundary(t *testing.T) {
	server, _, _, cleanup := newTestServer(t)
	defer cleanup()

	health := httptest.NewRecorder()
	server.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}
	frontend := httptest.NewRecorder()
	server.Handler().ServeHTTP(frontend, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if frontend.Code != http.StatusOK || frontend.Body.String() != "test UI" {
		t.Fatalf("public frontend = %d %q", frontend.Code, frontend.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/graph", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/graph", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	server.Handler().ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, body=%s", authorized.Code, authorized.Body.String())
	}

	sameOrigin := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/graph", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Origin", "http://"+request.Host)
	server.Handler().ServeHTTP(sameOrigin, request)
	if sameOrigin.Code != http.StatusOK {
		t.Fatalf("same-origin UI request status = %d, body=%s", sameOrigin.Code, sameOrigin.Body.String())
	}

	crossOrigin := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/graph", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Origin", "https://malicious.example")
	server.Handler().ServeHTTP(crossOrigin, request)
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", crossOrigin.Code)
	}
}

func TestSSEReplaysDurableEvent(t *testing.T) {
	server, events, _, cleanup := newTestServer(t)
	defer cleanup()
	events.Publish(context.Background(), observability.Event{
		ID: "event-1", Time: time.Now().UTC(), Severity: observability.SeverityInfo,
		Type: "review.pending", Message: "Review is ready",
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	var frame strings.Builder
	for {
		line, readErr := reader.ReadString('\n')
		frame.WriteString(line)
		if line == "\n" || readErr != nil {
			break
		}
	}
	cancel()
	if !strings.Contains(frame.String(), "id: event-1") || !strings.Contains(frame.String(), "event: review.pending") {
		t.Fatalf("unexpected SSE frame: %q", frame.String())
	}
}

func TestManualTextIngestRequiresAuthAndQueuesDurableExtraction(t *testing.T) {
	server, _, store, cleanup := newTestServer(t)
	defer cleanup()

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/v1/ingest/text", strings.NewReader(`{"text":"knowledge"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/text", strings.NewReader(`{"text":"knowledge"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var submission struct {
		SourceSessionID string `json:"source_session_id"`
		ChunkID         string `json:"chunk_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &submission); err != nil {
		t.Fatal(err)
	}
	if submission.SourceSessionID == "" || submission.ChunkID == "" {
		t.Fatalf("submission = %#v", submission)
	}
	jobs, err := store.ListJobs(context.Background(), "queued", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || string(jobs[0].Kind) != "extract_analysis_batch" {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func newTestServer(t *testing.T) (*api.Server, *observability.Bus, *sqlite.Store, func()) {
	t.Helper()
	directory := t.TempDir()
	store, err := sqlite.Open(filepath.Join(directory, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := observability.NewBus(store, logger)
	sources := app.NewSourceManager(store, events, logger, 100)
	application := app.New("test", store, sources, events, logger)
	cfg := config.Default()
	cfg.DataDir = directory
	cfg.API = config.API{Listen: "127.0.0.1:0", Token: "test-token"}
	settings := config.NewManager(filepath.Join(directory, "config.toml"), cfg)
	frontend := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write([]byte("test UI")) })
	server, err := api.New(cfg.API, application, review.NewService(store), store, store, events, settings, nil, logger,
		api.WithFrontend(frontend), api.WithTextIngestor(store), api.WithBatchRepository(store))
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return server, events, store, func() {
		events.Close()
		_ = store.Close()
	}
}
