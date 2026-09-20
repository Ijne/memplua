package metrics

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHandlerRecordsOnlyValidEvents(t *testing.T) {
	store := openTestStore(t)
	handler := NewHandler(store)
	handler.now = func() time.Time { return time.Date(2026, 9, 20, 23, 30, 0, 0, time.FixedZone("UTC+3", 3*60*60)) }

	for _, test := range []struct {
		name   string
		method string
		body   string
		status int
	}{
		{name: "visit", method: http.MethodPost, body: `{"type":"visit"}`, status: http.StatusNoContent},
		{name: "download", method: http.MethodPost, body: `{"type":"download"}`, status: http.StatusNoContent},
		{name: "wrong method", method: http.MethodGet, body: `{"type":"visit"}`, status: http.StatusMethodNotAllowed},
		{name: "invalid type", method: http.MethodPost, body: `{"type":"install"}`, status: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPost, body: `{"type":"visit","id":"not stored"}`, status: http.StatusBadRequest},
		{name: "invalid JSON", method: http.MethodPost, body: `{`, status: http.StatusBadRequest},
		{name: "multiple values", method: http.MethodPost, body: `{"type":"visit"} {}`, status: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/api/metrics/event", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}

	visits, downloads := counts(t, store.db, "2026-09-20")
	if visits != 1 || downloads != 1 {
		t.Fatalf("counts = (%d, %d), want (1, 1)", visits, downloads)
	}
}

func TestStoreRecordsAtomicallyForUTCDay(t *testing.T) {
	store := openTestStore(t)
	const events = 40
	day := time.Date(2026, 9, 20, 23, 0, 0, 0, time.FixedZone("UTC-2", -2*60*60))

	var group sync.WaitGroup
	for index := 0; index < events; index++ {
		group.Add(1)
		event := EventDownload
		if index%2 == 0 {
			event = EventVisit
		}
		go func() {
			defer group.Done()
			if err := store.Record(context.Background(), event, day); err != nil {
				t.Errorf("Record: %v", err)
			}
		}()
	}
	group.Wait()

	visits, downloads := counts(t, store.db, "2026-09-21")
	if visits != events/2 || downloads != events/2 {
		t.Fatalf("counts = (%d, %d), want (%d, %d)", visits, downloads, events/2, events/2)
	}
}

func TestStoreRejectsInvalidEvent(t *testing.T) {
	store := openTestStore(t)
	if err := store.Record(context.Background(), EventType("install"), time.Now()); err != ErrInvalidEvent {
		t.Fatalf("Record error = %v, want %v", err, ErrInvalidEvent)
	}
	var total int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM daily_metrics`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("daily metric rows = %d, want 0", total)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func counts(t *testing.T, db *sql.DB, day string) (int, int) {
	t.Helper()
	var visits, downloads int
	if err := db.QueryRow(`SELECT visits, downloads FROM daily_metrics WHERE day = ?`, day).Scan(&visits, &downloads); err != nil {
		t.Fatal(err)
	}
	return visits, downloads
}
