package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crawler/internal/id"
	"crawler/internal/ingest"
	"crawler/internal/observability"

	"golang.org/x/sync/errgroup"
)

// Runner is a long-lived component owned by Application. Run blocks until its
// context is cancelled or the component fails.
type Runner interface {
	Run(ctx context.Context) error
}

// HealthProvider exposes a serializable component health snapshot.
type HealthProvider interface {
	Health() any
}

type namedRunner struct {
	name   string
	runner Runner
}

// Status is the aggregate runtime state returned by status APIs.
type Status struct {
	RunID         string             `json:"run_id"`
	Version       string             `json:"version"`
	State         State              `json:"state"`
	StartedAt     time.Time          `json:"started_at"`
	Queues        ingest.QueueStats  `json:"queues"`
	ReviewPending int                `json:"pending_review_conspects"`
	Components    map[string]any     `json:"components"`
	Sources       []SourceDescriptor `json:"sources"`
	Sessions      []SourceSession    `json:"sessions"`
}

// Application supervises the root run, registered components, and source
// sessions. Register all runners before calling Run.
type Application struct {
	version string
	store   RuntimeRepository
	sources *SourceManager
	bus     *observability.Bus
	logger  *slog.Logger
	mu      sync.RWMutex
	run     AppRun
	cancel  context.CancelFunc
	runners []namedRunner
	health  map[string]func() any
}

// New creates an application lifecycle controller without starting it.
func New(version string, store RuntimeRepository, sources *SourceManager, bus *observability.Bus, logger *slog.Logger) *Application {
	if logger == nil {
		logger = slog.Default()
	}
	return &Application{version: version, store: store, sources: sources, bus: bus, logger: logger, health: make(map[string]func() any)}
}

// AddRunner registers one owned background component and an optional health
// snapshot callback. It must be called before Run.
func (a *Application) AddRunner(name string, runner Runner, health func() any) {
	a.mu.Lock()
	a.runners = append(a.runners, namedRunner{name: name, runner: runner})
	if health != nil {
		a.health[name] = health
	}
	a.mu.Unlock()
}

// Run records an AppRun and supervises every component under a shared errgroup.
// The first unexpected error cancels the remaining components.
func (a *Application) Run(ctx context.Context) error {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	started := time.Now().UTC()
	a.mu.Lock()
	a.run = AppRun{ID: id.New(), Version: a.version, State: StateStarting, StartedAt: started}
	a.cancel = cancel
	run := a.run
	runners := append([]namedRunner(nil), a.runners...)
	a.mu.Unlock()
	if err := a.store.StartAppRun(runContext, run); err != nil {
		return fmt.Errorf("start app run: %w", err)
	}
	a.setState(StateRunning)
	a.publish(runContext, observability.SeverityInfo, "application.started", "KnowledgeCrawler started")

	group, groupContext := errgroup.WithContext(runContext)
	if a.sources != nil {
		group.Go(func() error { return a.sources.Run(groupContext) })
	}
	for _, item := range runners {
		item := item
		group.Go(func() error {
			if err := item.runner.Run(groupContext); err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("%s: %w", item.name, err)
			}
			return nil
		})
	}
	err := group.Wait()
	a.setState(StateStopping)
	finalState := StateStopped
	if err != nil {
		finalState = StateDegraded
		a.logger.Error("application stopped with error", "app_run_id", run.ID, "error", err)
		a.publish(context.Background(), observability.SeverityError, "application.failed", err.Error())
	} else {
		a.publish(context.Background(), observability.SeverityInfo, "application.stopped", "KnowledgeCrawler stopped")
	}
	if finishErr := a.store.FinishAppRun(context.Background(), run.ID, finalState, err); finishErr != nil && err == nil {
		err = finishErr
	}
	a.setState(finalState)
	if a.bus != nil {
		a.bus.Close()
	}
	return err
}

// Status returns an instantaneous aggregate of lifecycle, components, queues,
// pending review, registered sources, and persisted source sessions.
func (a *Application) Status(ctx context.Context) (Status, error) {
	a.mu.RLock()
	status := Status{
		RunID: a.run.ID, Version: a.version, State: a.run.State, StartedAt: a.run.StartedAt,
		Components: make(map[string]any),
	}
	for name, health := range a.health {
		component := health()
		status.Components[name] = component
		if state, ok := component.(interface{ ComponentState() string }); ok {
			switch state.ComponentState() {
			case "unavailable", "restarting":
				if status.State == StateRunning {
					status.State = StateDegraded
				}
			}
		}
	}
	a.mu.RUnlock()
	var err error
	status.Queues, err = a.store.QueueStats(ctx)
	if err != nil {
		return Status{}, err
	}
	status.ReviewPending, err = a.store.PendingCount(ctx)
	if err != nil {
		return Status{}, err
	}
	if a.sources != nil {
		status.Sources = a.sources.Sources()
		status.Sessions, err = a.sources.Sessions(ctx)
	}
	return status, err
}

// StartSource starts a new session for a registered source.
func (a *Application) StartSource(ctx context.Context, sourceID string) (SourceSession, error) {
	return a.sources.Start(ctx, sourceID)
}

// StopSource permanently stops and finalizes a source session.
func (a *Application) StopSource(ctx context.Context, sessionID string) error {
	return a.sources.Stop(ctx, sessionID, false)
}

// PauseSource stops capture while retaining a resumable session state.
func (a *Application) PauseSource(ctx context.Context, sessionID string) error {
	return a.sources.Stop(ctx, sessionID, true)
}

// ResumeSource restarts capture for a paused session.
func (a *Application) ResumeSource(ctx context.Context, sessionID string) (SourceSession, error) {
	return a.sources.Resume(ctx, sessionID)
}

// SourceSessions lists persisted sessions in repository order.
func (a *Application) SourceSessions(ctx context.Context) ([]SourceSession, error) {
	return a.sources.Sessions(ctx)
}

// SourceDescriptors returns the registered source capabilities.
func (a *Application) SourceDescriptors() []SourceDescriptor {
	return a.sources.Sources()
}

// Shutdown requests cancellation of the current run and returns immediately.
func (a *Application) Shutdown() {
	a.mu.RLock()
	cancel := a.cancel
	a.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Application) setState(state State) {
	a.mu.Lock()
	a.run.State = state
	a.mu.Unlock()
}

func (a *Application) publish(ctx context.Context, severity observability.Severity, eventType, message string) {
	if a.bus != nil {
		a.bus.Publish(ctx, observability.Event{
			Severity: severity, Type: eventType, Message: message,
			ResourceKind: "app_run", ResourceID: a.run.ID,
		})
	}
}
