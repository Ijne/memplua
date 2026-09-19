package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"crawler/internal/app"
	"crawler/internal/config"
	"crawler/internal/ingest"
	"crawler/internal/knowledge"
	"crawler/internal/observability"
	"crawler/internal/review"
)

// Server owns authenticated loopback routes and transport projections.
type Server struct {
	batches        BatchRepository
	config         config.API
	application    *app.Application
	review         *review.Service
	knowledge      knowledge.Repository
	jobs           JobAdmin
	events         *observability.Bus
	settings       *config.Manager
	scheduleExport func(context.Context) error
	logger         *slog.Logger
	textIngestor   ingest.TextSubmitter
	frontend       http.Handler
	token          string
	http           *http.Server
}

// BatchRepository exposes developer batch diagnostics and explicit finalization.
type BatchRepository interface {
	FinalizeSource(context.Context, string) error
	ListAnalysisBatches(context.Context, int) ([]ingest.AnalysisBatch, error)
	GetAnalysisBatch(context.Context, string) (*ingest.AnalysisBatch, error)
	ConspectChunks(context.Context, string) ([]ingest.Chunk, error)
}

// WithBatchRepository enables developer batch endpoints and source finalization.
func WithBatchRepository(repository BatchRepository) Option {
	return func(s *Server) { s.batches = repository }
}

// JobAdmin exposes durable job inspection and manual retry.
type JobAdmin interface {
	ListJobs(ctx context.Context, status ingest.JobStatus, limit int) ([]ingest.Job, error)
	RetryFailedJob(ctx context.Context, id string) error
}

const manualTextMaxRunes = 250_000

// Option installs an optional API adapter.
type Option func(*Server)

// WithFrontend mounts an embedded browser UI at the root/UI paths.
func WithFrontend(frontend http.Handler) Option {
	return func(server *Server) { server.frontend = frontend }
}

// WithTextIngestor enables authenticated manual text submission.
func WithTextIngestor(ingestor ingest.TextSubmitter) Option {
	return func(server *Server) { server.textIngestor = ingestor }
}

// New validates server configuration and registers the v1 REST/SSE routes.
func New(
	cfg config.API,
	application *app.Application,
	reviewService *review.Service,
	knowledgeRepository knowledge.Repository,
	jobs JobAdmin,
	events *observability.Bus,
	settings *config.Manager,
	scheduleExport func(context.Context) error,
	logger *slog.Logger,
	options ...Option,
) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	token, err := ensureToken(cfg)
	if err != nil {
		return nil, err
	}
	server := &Server{
		config: cfg, application: application, review: reviewService, knowledge: knowledgeRepository, jobs: jobs,
		events: events, settings: settings, scheduleExport: scheduleExport, logger: logger, token: token,
	}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	mux := http.NewServeMux()
	if server.frontend != nil {
		mux.Handle("GET /", server.frontend)
		mux.Handle("GET /ui", server.frontend)
		mux.Handle("GET /ui/", server.frontend)
	}
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /api/v1/status", server.status)
	mux.HandleFunc("GET /api/v1/ui/state", server.uiState)
	mux.HandleFunc("GET /api/v1/sources", server.sources)
	mux.HandleFunc("POST /api/v1/sources/{id}/start", server.startSource)
	mux.HandleFunc("POST /api/v1/source-sessions/{id}/pause", server.pauseSource)
	mux.HandleFunc("POST /api/v1/source-sessions/{id}/resume", server.resumeSource)
	mux.HandleFunc("POST /api/v1/source-sessions/{id}/stop", server.stopSource)
	mux.HandleFunc("POST /api/v1/ingest/text", server.submitText)
	mux.HandleFunc("POST /api/v1/source-sessions/{id}/finalize", server.finalizeSource)
	mux.HandleFunc("GET /api/v1/review/conspects", server.reviewConspects)
	mux.HandleFunc("GET /api/v1/review/conspects/{id}", server.reviewConspect)
	mux.HandleFunc("GET /api/v1/review/conspects/{id}/chunks", server.reviewChunks)
	mux.HandleFunc("GET /api/v1/review/conspects/{id}/source-text", server.sourceText)
	mux.HandleFunc("PUT /api/v1/review/items/{id}", server.reviewDecision)
	mux.HandleFunc("POST /api/v1/review/items/{id}/split", server.splitReviewItem)
	mux.HandleFunc("PUT /api/v1/review/taxonomy/{id}", server.reviewTaxonomy)
	mux.HandleFunc("POST /api/v1/review/conspects/{id}/apply", server.applyConspect)
	mux.HandleFunc("GET /api/v1/graph/search", server.graphSearch)
	mux.HandleFunc("GET /api/v1/developer/analysis-batches", server.analysisBatches)
	mux.HandleFunc("GET /api/v1/developer/analysis-batches/{id}", server.analysisBatch)
	mux.HandleFunc("GET /api/v1/graph", server.graph)
	mux.HandleFunc("GET /api/v1/graph/terms", server.terms)
	mux.HandleFunc("GET /api/v1/graph/thoughts", server.thoughts)
	mux.HandleFunc("GET /api/v1/taxonomy/{kind}", server.taxonomy)
	mux.HandleFunc("GET /api/v1/jobs", server.listJobs)
	mux.HandleFunc("POST /api/v1/jobs/{id}/retry", server.retryJob)
	mux.HandleFunc("GET /api/v1/notifications", server.notifications)
	mux.HandleFunc("POST /api/v1/notifications/{id}/read", server.markNotificationRead)
	mux.HandleFunc("GET /api/v1/events", server.eventStream)
	mux.HandleFunc("GET /api/v1/settings", server.getSettings)
	mux.HandleFunc("PATCH /api/v1/settings", server.patchSettings)
	mux.HandleFunc("POST /api/v1/exports/obsidian", server.exportObsidian)
	mux.HandleFunc("POST /api/v1/application/shutdown", server.shutdown)
	server.http = &http.Server{
		Addr: cfg.Listen, Handler: server.middleware(mux),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 2 * time.Minute,
	}
	return server, nil
}

// Run listens on the configured address and performs bounded shutdown when ctx
// is cancelled. Shared Runtime uses Handler with a pre-reserved listener.
func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return fmt.Errorf("listen API: %w", err)
	}
	s.http.BaseContext = func(net.Listener) context.Context { return ctx }
	s.logger.Info("local API listening", "address", listener.Addr().String())
	done := make(chan error, 1)
	go func() {
		done <- s.http.Serve(listener)
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutdownContext); err != nil {
			s.logger.Warn("graceful API shutdown timed out; closing active connections", "error", err)
			if closeErr := s.http.Close(); closeErr != nil {
				return errors.Join(err, closeErr)
			}
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Handler returns routes wrapped with CORS, authentication, and desktop-safe
// error projection middleware.
func (s *Server) Handler() http.Handler {
	return s.http.Handler
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if isDesktopClient(request) || request.URL.Query().Get("view") == "desktop" || request.URL.Path == "/api/v1/ui/state" || strings.HasSuffix(request.URL.Path, "/source-text") {
			safe := &desktopErrors{ResponseWriter: response}
			response = safe
			defer safe.finish(s)
		}
		if origin := request.Header.Get("Origin"); origin != "" {
			if !sameOrigin(request, origin) && (s.config.AllowedOrigin == "" || origin != s.config.AllowedOrigin) {
				writeError(response, http.StatusForbidden, "origin is not allowed")
				return
			}
			response.Header().Set("Access-Control-Allow-Origin", origin)
			response.Header().Set("Vary", "Origin")
		}
		if request.Method == http.MethodOptions {
			response.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Last-Event-ID, X-Memplua-Client, X-KnowledgeCrawler-Client")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
			response.WriteHeader(http.StatusNoContent)
			return
		}
		publicFrontend := s.frontend != nil && (request.URL.Path == "/" || request.URL.Path == "/ui" || strings.HasPrefix(request.URL.Path, "/ui/"))
		if request.URL.Path != "/healthz" && !publicFrontend {
			provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
				writeError(response, http.StatusUnauthorized, "invalid bearer token")
				return
			}
		}
		next.ServeHTTP(response, request)
	})
}

func isDesktopClient(request *http.Request) bool {
	return request.Header.Get("X-Memplua-Client") == "desktop" ||
		request.Header.Get("X-KnowledgeCrawler-Client") == "desktop"
}

func sameOrigin(request *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == request.Host
}

func (s *Server) submitText(response http.ResponseWriter, request *http.Request) {
	if s.textIngestor == nil {
		writeError(response, http.StatusServiceUnavailable, "manual text ingest is unavailable")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeError(response, http.StatusBadRequest, "text is required")
		return
	}
	if utf8.RuneCountInString(body.Text) > manualTextMaxRunes {
		writeError(response, http.StatusBadRequest, "text exceeds the 250000 character limit")
		return
	}
	submission, err := s.textIngestor.SubmitText(request.Context(), body.Text)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	s.events.Publish(request.Context(), observability.Event{
		Severity: observability.SeverityInfo, Type: "ingest.text_submitted", Message: "Manual text was queued for processing",
		ResourceKind: "chunk", ResourceID: submission.ChunkID,
		Data: map[string]any{"source_session_id": submission.SourceSessionID},
	})
	writeJSON(response, http.StatusAccepted, submission)
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) status(response http.ResponseWriter, request *http.Request) {
	status, err := s.application.Status(request.Context())
	respond(response, status, err)
}

func (s *Server) sources(response http.ResponseWriter, request *http.Request) {
	sessions, err := s.application.SourceSessions(request.Context())
	respond(response, map[string]any{"sources": s.application.SourceDescriptors(), "sessions": sessions}, err)
}

func (s *Server) startSource(response http.ResponseWriter, request *http.Request) {
	session, err := s.application.StartSource(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	if isDesktopClient(request) {
		session.LastError = ""
	}
	writeJSON(response, http.StatusAccepted, session)
}

func (s *Server) pauseSource(response http.ResponseWriter, request *http.Request) {
	respondEmpty(response, s.application.PauseSource(request.Context(), request.PathValue("id")))
}

func (s *Server) resumeSource(response http.ResponseWriter, request *http.Request) {
	session, err := s.application.ResumeSource(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	if isDesktopClient(request) {
		session.LastError = ""
	}
	writeJSON(response, http.StatusAccepted, session)
}

func (s *Server) stopSource(response http.ResponseWriter, request *http.Request) {
	respondEmpty(response, s.application.StopSource(request.Context(), request.PathValue("id")))
}

func (s *Server) finalizeSource(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		writeError(w, 503, "batch repository unavailable")
		return
	}
	reviewResult(w, nil, s.batches.FinalizeSource(r.Context(), r.PathValue("id")))
}
func (s *Server) reviewConspects(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	value, err := s.review.ListPending(r.Context(), limit)
	if err == nil && r.URL.Query().Get("view") == "desktop" {
		kinds := s.sourceKinds(r.Context())
		summaries := []desktopSummary{}
		for _, c := range value {
			summaries = append(summaries, summary(c, kinds[c.SourceSessionID]))
		}
		reviewResult(w, summaries, nil)
		return
	}
	reviewResult(w, value, err)
}
func (s *Server) reviewConspect(w http.ResponseWriter, r *http.Request) {
	value, err := s.review.Get(r.Context(), r.PathValue("id"))
	if err == nil && value == nil {
		err = sql.ErrNoRows
	}
	if err == nil && r.URL.Query().Get("view") == "desktop" {
		reviewResult(w, desktopDetail(*value, s.sourceKinds(r.Context())[value.SourceSessionID]), nil)
		return
	}
	reviewResult(w, value, err)
}
func (s *Server) reviewDecision(w http.ResponseWriter, r *http.Request) {
	var d review.Decision
	if err := decodeJSON(r, &d); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	reviewResult(w, nil, s.review.Decide(r.Context(), r.PathValue("id"), d))
}
func (s *Server) splitReviewItem(w http.ResponseWriter, r *http.Request) {
	var d struct {
		VariantIDs []string `json:"variant_ids"`
	}
	if err := decodeJSON(r, &d); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	reviewResult(w, nil, s.review.Split(r.Context(), r.PathValue("id"), d.VariantIDs))
}
func (s *Server) reviewTaxonomy(w http.ResponseWriter, r *http.Request) {
	var d review.TaxonomyDecision
	if err := decodeJSON(r, &d); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	reviewResult(w, nil, s.review.Taxonomy(r.Context(), r.PathValue("id"), d))
}
func (s *Server) applyConspect(w http.ResponseWriter, r *http.Request) {
	reviewResult(w, nil, s.review.Apply(r.Context(), r.PathValue("id")))
}
func (s *Server) graphSearch(w http.ResponseWriter, r *http.Request) {
	kind := review.ObjectKind(r.URL.Query().Get("kind"))
	if kind != review.ObjectTerm && kind != review.ObjectThought {
		writeError(w, 400, "kind must be term or thought")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	snapshot, err := s.knowledge.Snapshot(r.Context())
	if err != nil {
		reviewResult(w, nil, err)
		return
	}
	query := knowledge.Normalize(r.URL.Query().Get("q"))
	out := []review.Match{}
	for _, m := range review.CanonicalMatches(snapshot) {
		if m.Kind == kind && (query == "" || strings.Contains(knowledge.Normalize(m.Value.Text(kind)), query)) {
			out = append(out, m)
			if len(out) == limit {
				break
			}
		}
	}
	reviewResult(w, out, nil)
}
func (s *Server) reviewChunks(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		writeError(w, 503, "batch repository unavailable")
		return
	}
	value, err := s.batches.ConspectChunks(r.Context(), r.PathValue("id"))
	reviewResult(w, value, err)
}
func (s *Server) analysisBatches(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		writeError(w, 503, "batch repository unavailable")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	value, err := s.batches.ListAnalysisBatches(r.Context(), limit)
	reviewResult(w, value, err)
}
func (s *Server) analysisBatch(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		writeError(w, 503, "batch repository unavailable")
		return
	}
	value, err := s.batches.GetAnalysisBatch(r.Context(), r.PathValue("id"))
	if err == nil && value == nil {
		err = sql.ErrNoRows
	}
	reviewResult(w, value, err)
}
func reviewResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		code := "review.failed"
		switch {
		case errors.Is(err, sql.ErrNoRows):
			status = 404
			code = "review.not_found"
		case errors.Is(err, review.ErrInvalid):
			status = 400
			code = "review.invalid"
		case errors.Is(err, review.ErrExactMatch):
			status = 409
			code = "review.exact_match"
		case errors.Is(err, review.ErrDependency):
			status = 409
			code = "review.rejected_dependency"
		case errors.Is(err, review.ErrConflict):
			status = 409
			code = "review.conflict"
		case errors.Is(err, review.ErrNotReady):
			status = 409
			code = "review.not_ready"
		}
		writeJSON(w, status, map[string]string{"error": err.Error(), "code": code})
		return
	}
	if value == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, 200, value)
}

func (s *Server) graph(response http.ResponseWriter, request *http.Request) {
	value, err := s.knowledge.Snapshot(request.Context())
	respond(response, value, err)
}

func (s *Server) terms(response http.ResponseWriter, request *http.Request) {
	value, err := s.knowledge.ListTerms(request.Context())
	respond(response, value, err)
}

func (s *Server) thoughts(response http.ResponseWriter, request *http.Request) {
	value, err := s.knowledge.ListThoughts(request.Context())
	respond(response, value, err)
}

func (s *Server) taxonomy(response http.ResponseWriter, request *http.Request) {
	kind := knowledge.TaxonomyKind(request.PathValue("kind"))
	if kind != knowledge.TaxonomyTag {
		writeError(response, http.StatusBadRequest, "taxonomy kind must be tag")
		return
	}
	value, err := s.knowledge.ListTaxonomy(request.Context(), kind)
	respond(response, value, err)
}

func (s *Server) listJobs(response http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	status := ingest.JobStatus(request.URL.Query().Get("status"))
	if !validJobStatus(status) {
		writeError(response, http.StatusBadRequest, "invalid job status")
		return
	}
	value, err := s.jobs.ListJobs(request.Context(), status, limit)
	respond(response, value, err)
}

func (s *Server) retryJob(response http.ResponseWriter, request *http.Request) {
	if err := s.jobs.RetryFailedJob(request.Context(), request.PathValue("id")); err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]string{"status": "queued"})
}

func validJobStatus(status ingest.JobStatus) bool {
	switch status {
	case "", ingest.JobQueued, ingest.JobRunning, ingest.JobRetry, ingest.JobDone, ingest.JobFailed:
		return true
	default:
		return false
	}
}

func (s *Server) notifications(response http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	value, err := s.events.History(request.Context(), request.URL.Query().Get("after_id"), limit)
	respond(response, value, err)
}

func (s *Server) markNotificationRead(response http.ResponseWriter, request *http.Request) {
	respondEmpty(response, s.events.MarkRead(request.Context(), request.PathValue("id")))
}

func (s *Server) eventStream(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	afterID := request.Header.Get("Last-Event-ID")
	// Read the durable event outbox so transactionally inserted events are streamed too.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		for {
			history, err := s.events.History(request.Context(), afterID, 500)
			if err != nil {
				return
			}
			for _, event := range history {

				if request.URL.Query().Get("view") == "desktop" {
					if projected := projectEvent(event); projected != nil {
						data, _ := json.Marshal(projected)
						fmt.Fprintf(response, "id: %s\nevent: user\ndata: %s\n\n", event.ID, data)
					} else {
						fmt.Fprintf(response, "id: %s\n: checkpoint\n\n", event.ID)
					}
				} else {
					writeSSE(response, event)
				}
				afterID = event.ID
			}
			flusher.Flush()
			if len(history) < 500 {
				break
			}
		}
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
		}
	}

}

func (s *Server) getSettings(response http.ResponseWriter, _ *http.Request) {
	cfg := s.settings.Current()
	cfg.API.Token = ""
	respond(response, cfg, nil)
}

func (s *Server) patchSettings(response http.ResponseWriter, request *http.Request) {
	var patch config.SettingsPatch
	if err := decodeJSON(request, &patch); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	before := s.settings.Current()
	cfg, err := s.settings.Update(patch)
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	cfg.API.Token = ""
	restartKeys := config.RestartKeys(before, cfg)
	if s.events != nil {
		s.events.Publish(request.Context(), observability.Event{Severity: observability.SeverityInfo, Type: "settings.changed", Message: "Preferences updated"})
	}
	writeJSON(response, http.StatusOK, map[string]any{"settings": cfg, "restart_required": len(restartKeys) > 0, "restart_keys": restartKeys})
}

func (s *Server) exportObsidian(response http.ResponseWriter, request *http.Request) {
	if s.scheduleExport == nil {
		writeError(response, http.StatusServiceUnavailable, "exporter is unavailable")
		return
	}
	if err := s.scheduleExport(request.Context()); err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) shutdown(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusAccepted, map[string]string{"status": "stopping"})
	if flusher, ok := response.(http.Flusher); ok {
		flusher.Flush()
	}
	s.application.Shutdown()
}

func ensureToken(cfg config.API) (string, error) {
	if cfg.Token != "" {
		return cfg.Token, nil
	}
	if data, err := os.ReadFile(cfg.TokenFile); err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.TokenFile), 0700); err != nil {
		return "", err
	}
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	token := hex.EncodeToString(value)
	if err := os.WriteFile(cfg.TokenFile, []byte(token+"\n"), 0600); err != nil {
		return "", err
	}
	return token, nil
}

func decodeJSON(request *http.Request, target any) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid JSON: request body must contain exactly one value")
	}
	return nil
}

func respond(response http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func respondEmpty(response http.ResponseWriter, err error) {
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func writeSSE(response http.ResponseWriter, event observability.Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(response, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, data)
}
