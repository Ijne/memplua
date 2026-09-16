package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"crawler/internal/id"
	"crawler/internal/ingest"
	"crawler/internal/observability"
)

// ErrBackpressure tells a source to pause intake rather than discard data.
var ErrBackpressure = errors.New("processing is paused because a queue limit was reached")

// SourceFactory describes a registered source and constructs one session-bound
// Source instance when the capability is available.
type SourceFactory struct {
	Kind      string
	Available bool
	Reason    string
	New       func(sessionID string) (ingest.Source, error)
}

// SourceDescriptor is the UI/API view of a registered source capability.
type SourceDescriptor struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type sourceStore interface {
	SaveSourceSession(ctx context.Context, session SourceSession) error
	GetSourceSession(ctx context.Context, id string) (*SourceSession, error)
	UpdateSourceSession(ctx context.Context, id string, state SourceState, cause error) error
	ListSourceSessions(ctx context.Context) ([]SourceSession, error)
	SaveChunk(ctx context.Context, chunk ingest.Chunk) error
	QueueStats(ctx context.Context) (ingest.QueueStats, error)
}

type activeSource struct {
	session SourceSession
	cancel  context.CancelFunc
	final   SourceState
	done    chan struct{}
}

// SourceManager owns registered factories, active source goroutines, session
// cancellation, and intake backpressure.
type SourceManager struct {
	store           sourceStore
	bus             *observability.Bus
	logger          *slog.Logger
	processingLimit int
	mu              sync.RWMutex
	root            context.Context
	factories       map[string]SourceFactory
	active          map[string]*activeSource
	wg              sync.WaitGroup
}

// NewSourceManager creates an idle manager with the supplied durable queue limit.
func NewSourceManager(store sourceStore, bus *observability.Bus, logger *slog.Logger, processingLimit int) *SourceManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &SourceManager{
		store: store, bus: bus, logger: logger, processingLimit: processingLimit,
		factories: make(map[string]SourceFactory), active: make(map[string]*activeSource),
	}
}

// Register installs or replaces a stable named source factory.
func (m *SourceManager) Register(id string, factory SourceFactory) {
	m.mu.Lock()
	m.factories[id] = factory
	m.mu.Unlock()
}

// Sources returns a stable snapshot of registered source descriptors.
func (m *SourceManager) Sources() []SourceDescriptor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]SourceDescriptor, 0, len(m.factories))
	for sourceID, factory := range m.factories {
		result = append(result, SourceDescriptor{ID: sourceID, Kind: factory.Kind, Available: factory.Available, Reason: factory.Reason})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// Run blocks until cancellation and stops all active sessions before returning.
func (m *SourceManager) Run(ctx context.Context) error {
	m.mu.Lock()
	m.root = ctx
	m.mu.Unlock()
	<-ctx.Done()
	m.mu.Lock()
	m.root = nil
	for _, active := range m.active {
		active.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

// Start persists and starts a new session for sourceID. At most one active
// session for a given source ID is allowed.
func (m *SourceManager) Start(ctx context.Context, sourceID string) (SourceSession, error) {
	m.mu.RLock()
	factory, ok := m.factories[sourceID]
	root := m.root
	m.mu.RUnlock()
	if !ok {
		return SourceSession{}, fmt.Errorf("unknown source %q", sourceID)
	}
	if !factory.Available {
		return SourceSession{}, fmt.Errorf("source %q is unavailable: %s", sourceID, factory.Reason)
	}
	if root == nil {
		return SourceSession{}, errors.New("application is not running")
	}
	session := SourceSession{
		ID: id.New(), SourceID: sourceID, Kind: factory.Kind,
		State: SourceStarting, StartedAt: time.Now().UTC(),
	}
	source, err := factory.New(session.ID)
	if err != nil {
		return SourceSession{}, err
	}
	if err := m.store.SaveSourceSession(ctx, session); err != nil {
		_ = source.Close()
		return SourceSession{}, err
	}
	sourceContext, cancel := context.WithCancel(root)
	active := &activeSource{session: session, cancel: cancel, final: SourceStopped, done: make(chan struct{})}
	m.mu.Lock()
	m.active[session.ID] = active
	m.mu.Unlock()
	m.wg.Add(1)
	go m.runSource(sourceContext, source, active)
	return session, nil
}

func (m *SourceManager) runSource(ctx context.Context, source ingest.Source, active *activeSource) {
	defer m.wg.Done()
	defer close(active.done)
	active.session.State = SourceRunning
	_ = m.store.UpdateSourceSession(context.Background(), active.session.ID, SourceRunning, nil)
	m.publish(context.Background(), observability.SeverityInfo, "source.started", "Source started", active.session.ID)
	err := source.Run(ctx, func(emitContext context.Context, chunk ingest.Chunk) error {
		if chunk.SourceSessionID == "" {
			chunk.SourceSessionID = active.session.ID
		}
		// Persist the completed transcript before applying backpressure. The
		// source blocks here, so no new segment is captured while the durable
		// processing queue remains full.
		if err := m.store.SaveChunk(emitContext, chunk); err != nil {
			return err
		}
		m.logger.Info("transcript chunk stored",
			"source_session_id", active.session.ID,
			"chunk_id", chunk.ID,
		)
		m.publish(emitContext, observability.SeverityInfo, "source.chunk_ready", "Transcript is ready for processing", active.session.ID)
		if ctx.Err() != nil {
			return nil
		}
		return m.waitForCapacity(ctx, active.session.ID)
	})
	if closeErr := source.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("close source: %w", closeErr)
	}
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	state := SourceStopped
	if err != nil {
		if errors.Is(err, ErrBackpressure) {
			state = SourcePaused
			m.publish(context.Background(), observability.SeverityWarning, "source.paused", err.Error(), active.session.ID)
		} else {
			state = SourceFailed
			m.logger.Error("source stopped with error", "source_session_id", active.session.ID, "error", err)
			m.publish(context.Background(), observability.SeverityError, "source.failed", err.Error(), active.session.ID)
		}
	} else {
		m.mu.RLock()
		state = active.final
		m.mu.RUnlock()
		m.publish(context.Background(), observability.SeverityInfo, "source."+string(state), "Source "+string(state), active.session.ID)
	}
	_ = m.store.UpdateSourceSession(context.Background(), active.session.ID, state, err)
	m.mu.Lock()
	delete(m.active, active.session.ID)
	m.mu.Unlock()
}

// Stop cancels an active session, waits for its source to exit, finalizes its
// collecting batch, and persists either paused or stopped state.
func (m *SourceManager) Stop(ctx context.Context, sessionID string, pause bool) error {
	m.mu.Lock()
	active := m.active[sessionID]
	if active == nil {
		m.mu.Unlock()
		session, err := m.store.GetSourceSession(ctx, sessionID)
		if err != nil {
			return err
		}
		if session == nil {
			return fmt.Errorf("source session %q does not exist", sessionID)
		}
		if session.State == SourceStopped || session.State == SourceFailed || (pause && session.State == SourcePaused) {
			return nil
		}
		if !pause && session.State == SourcePaused {
			return m.store.UpdateSourceSession(ctx, sessionID, SourceStopped, nil)
		}
		return fmt.Errorf("source session %q is not active", sessionID)
	}
	active.final = SourceStopped
	if pause {
		active.final = SourcePaused
	}
	active.cancel()
	done := active.done
	m.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Resume starts capture again for a persisted paused session.
func (m *SourceManager) Resume(ctx context.Context, sessionID string) (SourceSession, error) {
	m.mu.RLock()
	if m.active[sessionID] != nil {
		m.mu.RUnlock()
		return SourceSession{}, fmt.Errorf("source session %q is already active", sessionID)
	}
	root := m.root
	m.mu.RUnlock()
	if root == nil {
		return SourceSession{}, errors.New("application is not running")
	}
	session, err := m.store.GetSourceSession(ctx, sessionID)
	if err != nil {
		return SourceSession{}, err
	}
	if session == nil {
		return SourceSession{}, fmt.Errorf("source session %q does not exist", sessionID)
	}
	if session.State != SourcePaused {
		return SourceSession{}, fmt.Errorf("source session %q is %s, not paused", sessionID, session.State)
	}
	m.mu.RLock()
	factory, ok := m.factories[session.SourceID]
	m.mu.RUnlock()
	if !ok || !factory.Available {
		return SourceSession{}, fmt.Errorf("source %q is unavailable", session.SourceID)
	}
	source, err := factory.New(session.ID)
	if err != nil {
		return SourceSession{}, err
	}
	if err := m.store.UpdateSourceSession(ctx, session.ID, SourceStarting, nil); err != nil {
		_ = source.Close()
		return SourceSession{}, err
	}
	session.State = SourceStarting
	session.StoppedAt = nil
	sourceContext, cancel := context.WithCancel(root)
	active := &activeSource{session: *session, cancel: cancel, final: SourceStopped, done: make(chan struct{})}
	m.mu.Lock()
	if m.active[sessionID] != nil {
		m.mu.Unlock()
		cancel()
		_ = source.Close()
		return SourceSession{}, fmt.Errorf("source session %q became active", sessionID)
	}
	m.active[session.ID] = active
	m.mu.Unlock()
	m.wg.Add(1)
	go m.runSource(sourceContext, source, active)
	return *session, nil
}

// Sessions lists all persisted source sessions.
func (m *SourceManager) Sessions(ctx context.Context) ([]SourceSession, error) {
	return m.store.ListSourceSessions(ctx)
}

func (m *SourceManager) waitForCapacity(ctx context.Context, sessionID string) error {
	paused := false
	for {
		stats, err := m.store.QueueStats(ctx)
		if err != nil {
			return err
		}
		full := m.processingLimit > 0 && stats.Queued+stats.Retry+stats.Running >= m.processingLimit
		if !full {
			if paused {
				_ = m.store.UpdateSourceSession(ctx, sessionID, SourceRunning, nil)
				m.publish(ctx, observability.SeverityInfo, "pipeline.processing_resumed", "Processing queue has capacity again", sessionID)
			}
			return nil
		}
		if !paused {
			paused = true
			_ = m.store.UpdateSourceSession(ctx, sessionID, SourcePaused, ErrBackpressure)
			m.publish(ctx, observability.SeverityWarning, "pipeline.processing_paused", ErrBackpressure.Error(), sessionID)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *SourceManager) publish(ctx context.Context, severity observability.Severity, eventType, message, sessionID string) {
	if m.bus != nil {
		m.bus.Publish(ctx, observability.Event{
			Severity: severity, Type: eventType, Message: message,
			ResourceKind: "source_session", ResourceID: sessionID,
		})
	}
}
