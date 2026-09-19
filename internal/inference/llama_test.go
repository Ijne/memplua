package inference

import (
	"context"
	"crawler/internal/config"
	"crawler/internal/ingest"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeLifecycle struct {
	ready    chan struct{}
	restarts chan error
}

func (l *fakeLifecycle) WaitReady(ctx context.Context) error {
	select {
	case <-l.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *fakeLifecycle) Restart(err error) { l.restarts <- err }

func TestParseExtractionRejectsLegacyAndMalformedShape(t *testing.T) {
	for _, input := range []string{`{"entities":[]}`, `{"terms":[],"thoughts":[]}`, `{}`, `{"conspects":null}`, `bad`} {
		if _, _, err := parseExtraction(input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	// Local models sometimes include harmless forward-compatible fields. The
	// extraction should not be retried just because a model emits one of them.
	if result, _, err := parseExtraction(`{"conspects":[],"operations":[],"ref":"batch-1"}`); err != nil || len(result.Conspects) != 0 {
		t.Fatalf("forward-compatible response: result=%+v err=%v", result, err)
	}
	result, normalized, err := parseExtraction(`prefix {"conspects":[]} suffix`)
	if err != nil || len(result.Conspects) != 0 || normalized != `{"conspects":[]}` {
		t.Fatalf("empty result: %v %s", err, normalized)
	}
}

func TestExtractionMakesOneRequestForThreeTopics(t *testing.T) {
	calls := 0
	response := ingest.Extraction{Conspects: []ingest.TopicExtraction{}}
	for _, topic := range []string{"one", "two", "three"} {
		response.Conspects = append(response.Conspects, ingest.TopicExtraction{Topic: topic, Terms: []ingest.TermCandidate{{Ref: "term-1", Name: topic, Tags: []string{"technology"}, Provenance: ingest.Provenance{EvidenceChunkIDs: []string{"focus"}, AnchorChunkID: "focus"}}}, Thoughts: []ingest.ThoughtCandidate{}})
	}
	raw, _ := json.Marshal(response)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req completionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if !strings.Contains(req.Prompt, "FOCUS:") || !strings.Contains(req.Prompt, "CONTEXT:") || !strings.Contains(req.Prompt, `"chunk_id":"focus"`) {
			t.Error("missing prompt boundaries")
		}
		json.NewEncoder(w).Encode(completionResponse{Content: string(raw)})
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Models.LLMURL = server.URL
	client := NewLlamaClient(cfg.Models, "en", nil)
	result, gotRaw, err := client.Extract(context.Background(), ingest.BatchInput{Focus: []ingest.Chunk{{ID: "focus", Text: "source"}}})
	if err != nil || calls != 1 || len(result.Conspects) != 3 || gotRaw != string(raw) {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
}

func TestLlamaClientHonorsConfiguredParallelLimit(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		for previous := maximum.Load(); current > previous && !maximum.CompareAndSwap(previous, current); previous = maximum.Load() {
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		_ = json.NewEncoder(w).Encode(completionResponse{Content: `{"conspects":[]}`})
	}))
	defer server.Close()
	cfg := config.Default().Models
	cfg.LLMURL = server.URL
	cfg.Parallel = 1
	cfg.RequestTimeout = time.Second
	client := NewLlamaClient(cfg, "en", nil)
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := client.complete(context.Background(), "prompt", false)
			done <- err
		}()
	}
	<-started
	select {
	case <-started:
		t.Fatal("a second request reached llama-server while parallel=1")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent requests = %d, want 1", got)
	}
}

func TestLlamaClientTimeoutIsFiniteFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(completionResponse{Content: `{}`})
	}))
	defer server.Close()
	cfg := config.Default().Models
	cfg.LLMURL = server.URL
	cfg.RequestTimeout = 20 * time.Millisecond
	client := NewLlamaClient(cfg, "en", nil)
	_, err := client.complete(context.Background(), "prompt", false)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if errors.Is(err, ingest.ErrPaused) {
		t.Fatalf("generation timeout must consume a bounded retry attempt: %v", err)
	}
}

func TestLlamaClientPreservesConfiguredLongGenerationTimeout(t *testing.T) {
	if got := boundedRequestTimeout(10 * time.Minute); got != 10*time.Minute {
		t.Fatalf("bounded request timeout = %s, want 10m", got)
	}
	if got := boundedRequestTimeout(45 * time.Minute); got != 30*time.Minute {
		t.Fatalf("maximum request timeout = %s, want 30m", got)
	}
}

func TestLlamaClientBoundsGenerationAndDisablesThinkingInUserTurn(t *testing.T) {
	requestSeen := make(chan completionRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requestSeen <- request
		_ = json.NewEncoder(w).Encode(completionResponse{Content: `{"conspects":[]}`})
	}))
	defer server.Close()

	cfg := config.Default().Models
	cfg.LLMURL = server.URL
	cfg.MaxTokens = 30720 // A value persisted by older application versions.
	cfg.ContextSize = 16384
	client := NewLlamaClient(cfg, "ru", nil)
	if _, err := client.complete(context.Background(), "prompt", false); err != nil {
		t.Fatal(err)
	}
	request := <-requestSeen
	if request.MaxTokens != 4096 {
		t.Fatalf("n_predict = %d, want 4096", request.MaxTokens)
	}
	if !strings.Contains(request.Prompt, "<|im_start|>user\nprompt\n/no_think<|im_end|>") {
		t.Fatalf("thinking switch is not in the user turn: %q", request.Prompt)
	}
	if strings.Contains(request.Prompt, "<|im_start|>system\n/no_think") {
		t.Fatalf("thinking switch remained in the system turn: %q", request.Prompt)
	}
}

func TestLlamaClientWaitsForReadyBeforeRequest(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		_ = json.NewEncoder(w).Encode(completionResponse{Content: `{"conspects":[]}`})
	}))
	defer server.Close()
	cfg := config.Default().Models
	cfg.LLMURL = server.URL
	lifecycle := &fakeLifecycle{ready: make(chan struct{}), restarts: make(chan error, 1)}
	client := NewLlamaClient(cfg, "en", nil).WithLifecycle(lifecycle)
	done := make(chan error, 1)
	go func() { _, err := client.complete(context.Background(), "prompt", false); done <- err }()
	select {
	case <-requests:
		t.Fatal("request reached llama-server before readiness")
	case <-time.After(40 * time.Millisecond):
	}
	close(lifecycle.ready)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not resume when llama-server became ready")
	}
}

func TestLlamaClientRestartsStalledStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	cfg := config.Default().Models
	cfg.LLMURL = server.URL
	lifecycle := &fakeLifecycle{ready: make(chan struct{}), restarts: make(chan error, 1)}
	close(lifecycle.ready)
	client := NewLlamaClient(cfg, "en", nil).WithLifecycle(lifecycle)
	client.stallTimeout = 30 * time.Millisecond
	_, err := client.complete(context.Background(), "prompt", false)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("stall error = %v", err)
	}
	select {
	case restartErr := <-lifecycle.restarts:
		if !strings.Contains(restartErr.Error(), "stalled") {
			t.Fatalf("restart cause = %v", restartErr)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled request did not restart llama-server")
	}
}

func TestLlamaClientConsumesStreamingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"content\":\"{\\\"conspects\\\":\"}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"content\":\"[]}\"}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := config.Default().Models
	cfg.LLMURL = server.URL
	client := NewLlamaClient(cfg, "en", nil)
	client.stallTimeout = time.Second
	content, err := client.complete(context.Background(), "prompt", false)
	if err != nil || content != `{"conspects":[]}` {
		t.Fatalf("content=%q err=%v", content, err)
	}
}
