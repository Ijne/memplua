package observability

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crawler/internal/id"
)

// Severity is the user-facing importance of an AppEvent.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Event is a durable user notification and SSE payload.
type Event struct {
	ID           string         `json:"id"`
	Time         time.Time      `json:"time"`
	Severity     Severity       `json:"severity"`
	Type         string         `json:"type"`
	Message      string         `json:"message"`
	ResourceKind string         `json:"resource_kind,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
	Read         bool           `json:"read"`
}

// Repository persists and replays user events.
type Repository interface {
	SaveEvent(ctx context.Context, event Event) error
	ListEvents(ctx context.Context, afterID string, limit int) ([]Event, error)
	MarkEventRead(ctx context.Context, id string) error
}

// Bus persists events before non-blocking fan-out to live subscribers.
type Bus struct {
	repository  Repository
	logger      *slog.Logger
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
	closed      bool
}

// NewBus creates an open event bus. A nil repository disables persistence.
func NewBus(repository Repository, logger *slog.Logger) *Bus {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bus{repository: repository, logger: logger, subscribers: make(map[chan Event]struct{})}
}

// Publish fills missing identity/time/severity, persists the event, and offers it
// to each live subscriber. Slow subscribers recover from durable history.
func (b *Bus) Publish(ctx context.Context, event Event) {
	if event.ID == "" {
		event.ID = id.New()
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Severity == "" {
		event.Severity = SeverityInfo
	}
	if b.repository != nil {
		if err := b.repository.SaveEvent(ctx, event); err != nil {
			b.logger.Error("persist user event", "error", err, "event_type", event.Type)
		}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			// Events are durable. A slow subscriber reconnects using Last-Event-ID.
		}
	}
}

// Subscribe returns a bounded live channel and an idempotent unsubscribe
// function. The caller must invoke the function when it stops reading.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	channel := make(chan Event, buffer)
	b.mu.Lock()
	if b.closed {
		close(channel)
		b.mu.Unlock()
		return channel, func() {}
	}
	b.subscribers[channel] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subscribers[channel]; ok {
				delete(b.subscribers, channel)
				close(channel)
			}
			b.mu.Unlock()
		})
	}
}

// Close closes every live subscription and prevents later fan-out.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for subscriber := range b.subscribers {
		close(subscriber)
		delete(b.subscribers, subscriber)
	}
}

// History returns durable events after the supplied reconnect cursor.
func (b *Bus) History(ctx context.Context, afterID string, limit int) ([]Event, error) {
	if b.repository == nil {
		return nil, nil
	}
	return b.repository.ListEvents(ctx, afterID, limit)
}

// MarkRead marks one durable notification as read.
func (b *Bus) MarkRead(ctx context.Context, id string) error {
	if b.repository == nil {
		return nil
	}
	return b.repository.MarkEventRead(ctx, id)
}

// RecentWarnings reads from the end of the durable history for compact status
// surfaces; it must not be confused with forward-only SSE replay.
func (b *Bus) RecentWarnings(ctx context.Context, limit int) ([]Event, error) {
	if repository, ok := b.repository.(interface {
		RecentWarnings(context.Context, int) ([]Event, error)
	}); ok {
		return repository.RecentWarnings(ctx, limit)
	}
	return b.History(ctx, "", limit)
}
