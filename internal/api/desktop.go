package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"crawler/internal/app"
	"crawler/internal/ingest"
	"crawler/internal/observability"
	"crawler/internal/review"
)

type userAction struct {
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
}
type userEvent struct {
	ID       string                 `json:"id,omitempty"`
	Time     time.Time              `json:"time"`
	Severity observability.Severity `json:"severity"`
	Code     string                 `json:"code"`
	Params   map[string]any         `json:"params"`
	Audience string                 `json:"audience"`
	Action   *userAction            `json:"action,omitempty"`
}

// projectEvent is an explicit allowlist. Developer messages, paths, IDs and
// arbitrary event data never cross the desktop user-event boundary.
func projectEvent(event observability.Event) *userEvent {
	out := &userEvent{ID: event.ID, Time: event.Time, Severity: event.Severity, Code: event.Type, Params: map[string]any{}, Audience: "user"}
	switch event.Type {
	case "conspect.review_ready", "review.pending":
		out.Code = "review.ready"
		out.Action = &userAction{Type: "open_window", Target: "review"}
	case "conspect.applied":
		out.Code = "review.applied"
	case "model.configuration_required", "model.unavailable":
		out.Code = "model.unavailable"
		out.Action = &userAction{Type: "open_window", Target: "settings"}
	case "job.failed":
		out.Code = "processing.failed"
		out.Action = &userAction{Type: "open_window", Target: "settings"}
	case "model.restarting":
		out.Code = "processing.retrying"
	case "model.ready":
		out.Code = "model.ready"
	case "pipeline.processing_paused":
		out.Code = "processing.paused"
		out.Action = &userAction{Type: "open_window", Target: "review"}
	case "pipeline.processing_resumed":
		out.Code = "processing.resumed"
	case "source.chunk_ready":
		out.Code = "source.chunk_ready"
	case "job.retry_scheduled":
		out.Code = "processing.retrying"
	case "source.failed":
		out.Code = "source.failed"
		out.Action = &userAction{Type: "open_window", Target: "settings"}
	case "source.started", "source.paused", "source.resumed", "source.stopped", "application.started", "application.stopped", "export.completed", "ingest.text_submitted", "settings.changed":
	case "application.failed":
		out.Action = &userAction{Type: "open_window", Target: "settings"}
	default:
		return nil
	}
	return out
}

type desktopSource struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Available bool       `json:"available"`
	State     string     `json:"state"`
	SessionID string     `json:"session_id,omitempty"`
	Actions   []string   `json:"actions"`
	Code      string     `json:"code,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
}
type desktopState struct {
	Application      string          `json:"application"`
	Sources          []desktopSource `json:"sources"`
	Processing       string          `json:"processing"`
	PendingConspects int             `json:"pending_conspects"`
	PendingItems     int             `json:"pending_items"`
	Warning          *userEvent      `json:"warning,omitempty"`
}

func (s *Server) uiState(w http.ResponseWriter, r *http.Request) {
	status, err := s.application.Status(r.Context())
	if err != nil {
		s.logger.Error("read desktop state", "error", err)
		writeError(w, 503, "Application state is unavailable")
		return
	}
	state := desktopState{Application: string(status.State), Sources: []desktopSource{}, Processing: "idle", PendingConspects: status.ReviewPending}
	if state.Application == "" {
		state.Application = "starting"
	}
	if counts, ok := s.batches.(interface {
		DesktopCounts(context.Context) (int, int, string, error)
	}); ok {
		state.PendingConspects, state.PendingItems, state.Processing, err = counts.DesktopCounts(r.Context())
		if err != nil {
			s.logger.Error("read desktop counters", "error", err)
			writeError(w, 503, "Application state is unavailable")
			return
		}
	} else {
		switch {
		case status.Queues.Running > 0 || status.Queues.Queued > 0:
			state.Processing = "working"
		case status.Queues.Retry > 0:
			state.Processing = "retrying"
		case status.Queues.Failed > 0:
			state.Processing = "failed"
		}
	}
	for _, descriptor := range status.Sources {
		source := desktopSource{ID: descriptor.ID, Kind: descriptor.Kind, Available: descriptor.Available, State: "off", Actions: []string{}}
		if source.Available {
			source.Actions = []string{"start"}
		} else {
			source.Code = "source.unavailable"
		}
		for _, session := range status.Sessions {
			if session.SourceID != source.ID {
				continue
			}
			// Recovered historical sessions do not start recording or show stale failures.
			if !status.StartedAt.IsZero() && session.StartedAt.Before(status.StartedAt) {
				break
			}
			source.SessionID = session.ID
			started := session.StartedAt
			source.StartedAt = &started
			source.StoppedAt = session.StoppedAt
			switch session.State {
			case app.SourceStarting:
				source.State = "starting"
				source.Actions = []string{"stop"}
			case app.SourceRunning:
				source.State = "recording"
				source.Actions = []string{"pause", "stop"}
			case app.SourcePaused:
				source.State = "paused"
				source.Actions = []string{"resume", "stop"}
			case app.SourceFailed:
				source.State = "failed"
				source.Code = "source.failed"
			default:
				source.SessionID = ""
			}
			break
		}
		state.Sources = append(state.Sources, source)
	}
	for _, component := range status.Components {
		if health, ok := component.(interface{ ComponentState() string }); ok {
			switch health.ComponentState() {
			case "unavailable":
				state.Processing = "failed"
				state.Warning = projectEvent(observability.Event{Type: "model.unavailable", Severity: observability.SeverityError})
			case "restarting":
				state.Processing = "retrying"
			}
		}
	}
	if status.State == app.StatePaused {
		state.Processing = "paused"
	}
	if state.Warning == nil && s.events != nil {
		events, historyErr := s.events.RecentWarnings(r.Context(), 500)
		if historyErr != nil {
			s.logger.Warn("read desktop notifications", "error", historyErr)
		}
		for i := len(events) - 1; i >= 0; i-- {
			event := events[i]
			if event.Read || event.Severity == observability.SeverityInfo || (!status.StartedAt.IsZero() && event.Time.Before(status.StartedAt)) {
				continue
			}
			if warning := projectEvent(event); warning != nil && warning.Action != nil {
				state.Warning = warning
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, state)
}

type desktopSummary struct {
	ID              string                `json:"id"`
	Topic           string                `json:"topic"`
	CreatedAt       time.Time             `json:"created_at"`
	Status          ingest.ConspectStatus `json:"status"`
	SourceKind      string                `json:"source_kind"`
	ItemCount       int                   `json:"item_count"`
	UnresolvedItems int                   `json:"unresolved_items"`
	UnresolvedTags  int                   `json:"unresolved_tags"`
}

func summary(c review.Conspect, sourceKind string) desktopSummary {
	out := desktopSummary{ID: c.ID, Topic: c.Topic, CreatedAt: c.CreatedAt, Status: c.Status, SourceKind: sourceKind, ItemCount: len(c.Items)}
	for _, item := range c.Items {
		if item.Status != review.Resolved && item.Status != review.Applied {
			out.UnresolvedItems++
		}
	}
	for _, tag := range c.Taxonomy {
		if tag.Required && tag.Resolution == "" {
			out.UnresolvedTags++
		}
	}
	return out
}
func (s *Server) sourceKinds(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.application != nil {
		if sessions, err := s.application.SourceSessions(ctx); err == nil {
			for _, session := range sessions {
				out[session.ID] = session.Kind
			}
		}
	}
	return out
}
func desktopDetail(c review.Conspect, kind string) map[string]any {
	items := make([]map[string]any, 0, len(c.Items))
	names := map[string]string{}
	for _, term := range c.Terms {
		names[term.Ref] = term.Name
	}
	for _, item := range c.Items {
		variants := make([]map[string]any, 0, len(item.IncomingVariants))
		for _, v := range item.IncomingVariants {
			related := []string{}
			for _, ref := range v.RelatedTermRefs {
				if name := names[ref]; name != "" {
					related = append(related, name)
				}
			}
			variants = append(variants, map[string]any{"candidate_id": v.CandidateID, "value": v.Value, "related_terms": related})
		}
		matches := func(values []review.Match) []map[string]any {
			out := []map[string]any{}
			for _, m := range values {
				out = append(out, map[string]any{"id": m.ID, "kind": m.Kind, "source": m.Source, "conspect_id": m.ConspectID, "exact": m.Exact, "version": m.Version, "value": m.Value})
			}
			return out
		}
		items = append(items, map[string]any{"id": item.ID, "conspect_id": item.ConspectID, "kind": item.Kind, "incoming_variants": variants, "canonical_matches": matches(item.CanonicalMatches), "pending_matches": matches(item.PendingMatches), "resolution": item.Resolution, "target_id": item.TargetID, "target_version": item.TargetVersion, "final_value": item.FinalValue, "status": item.Status})
	}
	out := map[string]any{}
	encoded, _ := json.Marshal(summary(c, kind))
	_ = json.Unmarshal(encoded, &out)
	out["items"] = items
	out["taxonomy"] = c.Taxonomy
	return out
}

func (s *Server) sourceText(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		writeError(w, 503, "Source text is unavailable")
		return
	}
	c, err := s.review.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		reviewResult(w, nil, err)
		return
	}
	if c == nil {
		reviewResult(w, nil, sql.ErrNoRows)
		return
	}
	batch, err := s.batches.GetAnalysisBatch(r.Context(), c.AnalysisBatchID)
	if err != nil {
		reviewResult(w, nil, err)
		return
	}
	if batch == nil {
		reviewResult(w, nil, sql.ErrNoRows)
		return
	}
	// Focus is the immutable full input, not merely candidate evidence, and
	// deliberately excludes the previous batch's context tail.
	focus := append([]ingest.Chunk{}, batch.Input.Focus...)
	sort.SliceStable(focus, func(i, j int) bool { return focus[i].Sequence < focus[j].Sequence })
	texts := make([]string, 0, len(focus))
	started, ended := batch.StartedAt, batch.LastTextAt
	for i, chunk := range focus {
		texts = append(texts, chunk.Text)
		if i == 0 {
			started = chunk.CapturedAt
		}
		ended = chunk.CapturedAt
	}
	writeJSON(w, 200, struct {
		Text       string    `json:"text"`
		SourceKind string    `json:"source_kind"`
		StartedAt  time.Time `json:"started_at"`
		EndedAt    time.Time `json:"ended_at"`
	}{strings.Join(texts, "\n\n"), s.sourceKinds(r.Context())[c.SourceSessionID], started, ended})
}

// desktopErrors preserves success streaming while replacing errors with stable
// localization codes. Detailed causes remain exclusively in developer logs.
type desktopErrors struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *desktopErrors) WriteHeader(status int) {
	w.status = status
	if status < 400 {
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *desktopErrors) Write(p []byte) (int, error) {
	if w.status >= 400 {
		return w.body.Write(p)
	}
	return w.ResponseWriter.Write(p)
}
func (w *desktopErrors) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *desktopErrors) finish(s *Server) {
	if w.status < 400 {
		return
	}
	s.logger.Warn("desktop API request failed", "status", w.status, "error", strings.TrimSpace(w.body.String()))
	code := "request.failed"
	switch w.status {
	case 400:
		code = "request.invalid"
	case 401:
		code = "auth.required"
	case 403:
		code = "request.forbidden"
	case 404:
		code = "request.not_found"
	case 409:
		code = "review.conflict"
	case 503:
		code = "service.unavailable"
	}
	if strings.Contains(w.body.String(), review.ErrNotReady.Error()) {
		code = "review.not_ready"
	}
	writeJSON(w.ResponseWriter, w.status, map[string]any{"error": code, "code": code, "params": map[string]any{}, "audience": "user"})
}
