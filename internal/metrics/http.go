package metrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

const maxEventBodyBytes = 1024

// Handler accepts anonymous website events at POST /api/metrics/event when it
// is mounted by the public web backend.
type Handler struct {
	recorder Recorder
	now      func() time.Time
}

// NewHandler creates a handler with the system clock in UTC.
func NewHandler(recorder Recorder) *Handler {
	return &Handler{recorder: recorder, now: time.Now}
}

// ServeHTTP validates an event before making any persistent change.
func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method must be POST")
		return
	}
	if h.recorder == nil {
		writeError(response, http.StatusServiceUnavailable, "metrics storage is unavailable")
		return
	}

	var event struct {
		Type EventType `json:"type"`
	}
	defer request.Body.Close()
	body, err := io.ReadAll(io.LimitReader(request.Body, maxEventBodyBytes+1))
	if err != nil || len(body) > maxEventBodyBytes {
		writeError(response, http.StatusBadRequest, "invalid JSON")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		writeError(response, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "request body must contain exactly one value")
		return
	}
	if event.Type != EventVisit && event.Type != EventDownload {
		writeError(response, http.StatusBadRequest, "invalid event type")
		return
	}
	if err := h.recorder.Record(request.Context(), event.Type, h.now()); err != nil {
		writeError(response, http.StatusInternalServerError, "could not record event")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func writeError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": message})
}
