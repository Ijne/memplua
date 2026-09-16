package app

import (
	"context"
	"time"

	"crawler/internal/ingest"
)

// State describes the persisted lifecycle state of one application run.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StatePaused   State = "paused"
	StateDegraded State = "degraded"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
)

// AppRun records one process execution for crash recovery and diagnostics.
type AppRun struct {
	ID        string     `json:"id"`
	Version   string     `json:"version"`
	State     State      `json:"state"`
	StartedAt time.Time  `json:"started_at"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

// SourceState describes a persisted source-session lifecycle state.
type SourceState string

const (
	SourceStarting SourceState = "starting"
	SourceRunning  SourceState = "running"
	SourcePaused   SourceState = "paused"
	SourceStopped  SourceState = "stopped"
	SourceFailed   SourceState = "failed"
)

// SourceSession identifies one independently controlled source execution.
type SourceSession struct {
	ID        string      `json:"id"`
	SourceID  string      `json:"source_id"`
	Kind      string      `json:"kind"`
	State     SourceState `json:"state"`
	StartedAt time.Time   `json:"started_at"`
	StoppedAt *time.Time  `json:"stopped_at,omitempty"`
	LastError string      `json:"last_error,omitempty"`
}

// RuntimeRepository is the persistence contract consumed by Application and
// SourceManager. The SQLite store is its production implementation.
type RuntimeRepository interface {
	StartAppRun(ctx context.Context, run AppRun) error
	FinishAppRun(ctx context.Context, id string, state State, cause error) error
	SaveSourceSession(ctx context.Context, session SourceSession) error
	UpdateSourceSession(ctx context.Context, id string, state SourceState, cause error) error
	ListSourceSessions(ctx context.Context) ([]SourceSession, error)
	QueueStats(ctx context.Context) (ingest.QueueStats, error)
	PendingCount(ctx context.Context) (int, error)
}
