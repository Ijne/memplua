package app_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"crawler/internal/app"
	"crawler/internal/ingest"
	"crawler/internal/observability"
	"crawler/internal/storage/sqlite"
)

func TestNamedSourcesAreIndependentlyPausedResumedAndStopped(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memplua.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := observability.NewBus(store, logger)
	manager := app.NewSourceManager(store, events, logger, 100)
	registry := &fakeSourceRegistry{started: make(chan string, 8), stopped: make(chan string, 8)}
	for _, sourceID := range []string{"microphone", "loopback"} {
		sourceID := sourceID
		manager.Register(sourceID, app.SourceFactory{
			Kind: "audio/" + sourceID, Available: true,
			New: func(sessionID string) (ingest.Source, error) {
				return &fakeSource{id: sessionID, kind: "audio/" + sourceID, registry: registry}, nil
			},
		})
	}
	root, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- manager.Run(root) }()

	microphone := startEventually(t, manager, "microphone")
	loopback := startEventually(t, manager, "loopback")
	waitForIDs(t, registry.started, microphone.ID, loopback.ID)

	operationContext, operationCancel := context.WithTimeout(context.Background(), time.Second)
	defer operationCancel()
	if err := manager.Stop(operationContext, microphone.ID, true); err != nil {
		t.Fatal(err)
	}
	paused, err := store.GetSourceSession(context.Background(), microphone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.State != app.SourcePaused {
		t.Fatalf("microphone state = %s, want paused", paused.State)
	}
	select {
	case stoppedID := <-registry.stopped:
		if stoppedID != microphone.ID {
			t.Fatalf("pausing microphone stopped %s", stoppedID)
		}
	case <-time.After(time.Second):
		t.Fatal("microphone did not stop for pause")
	}

	resumed, err := manager.Resume(operationContext, microphone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != microphone.ID {
		t.Fatalf("resume created another session: %s", resumed.ID)
	}
	waitForIDs(t, registry.started, microphone.ID)
	if err := manager.Stop(operationContext, loopback.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(operationContext, microphone.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(operationContext, microphone.ID, false); err != nil {
		t.Fatalf("repeated stop must be idempotent: %v", err)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("source manager did not shut down")
	}
}

func TestConcurrentStopWaitersAllComplete(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := observability.NewBus(store, logger)
	manager := app.NewSourceManager(store, events, logger, 100)
	registry := &fakeSourceRegistry{started: make(chan string, 1), stopped: make(chan string, 1)}
	manager.Register("test", app.SourceFactory{Kind: "test", Available: true, New: func(sessionID string) (ingest.Source, error) {
		return &fakeSource{id: sessionID, kind: "test", registry: registry}, nil
	}})
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- manager.Run(root) }()
	session := startEventually(t, manager, "test")
	waitForIDs(t, registry.started, session.ID)
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := manager.Stop(ctx, session.ID, false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	cancel()
	if err = <-runDone; err != nil {
		t.Fatal(err)
	}
}

func startEventually(t *testing.T, manager *app.SourceManager, sourceID string) app.SourceSession {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		session, err := manager.Start(context.Background(), sourceID)
		if err == nil {
			return session
		}
		if time.Now().After(deadline) {
			t.Fatalf("start %s: %v", sourceID, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForIDs(t *testing.T, values <-chan string, expected ...string) {
	t.Helper()
	wanted := make(map[string]bool, len(expected))
	for _, value := range expected {
		wanted[value] = true
	}
	deadline := time.After(time.Second)
	for len(wanted) > 0 {
		select {
		case value := <-values:
			if wanted[value] {
				delete(wanted, value)
			}
		case <-deadline:
			t.Fatalf("sources did not start: still waiting for %v", wanted)
		}
	}
}

type fakeSourceRegistry struct {
	mu      sync.Mutex
	started chan string
	stopped chan string
}

type fakeSource struct {
	id       string
	kind     string
	registry *fakeSourceRegistry
}

func (s *fakeSource) ID() string   { return s.id }
func (s *fakeSource) Kind() string { return s.kind }
func (s *fakeSource) Run(ctx context.Context, _ func(context.Context, ingest.Chunk) error) error {
	s.registry.started <- s.id
	<-ctx.Done()
	s.registry.stopped <- s.id
	return ctx.Err()
}
func (s *fakeSource) Close() error { return nil }
