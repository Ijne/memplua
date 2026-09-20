// Package metrics records the anonymous, daily aggregates used by the public
// website. It deliberately has no reader API: the website can submit an event
// but cannot retrieve statistics.
package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// EventType is an anonymous event accepted from the website.
type EventType string

const (
	EventVisit    EventType = "visit"
	EventDownload EventType = "download"
)

// ErrInvalidEvent is returned for an event that is not part of the allowlist.
var ErrInvalidEvent = errors.New("metrics: invalid event type")

// Recorder is the narrow dependency needed by the HTTP handler.
type Recorder interface {
	Record(context.Context, EventType, time.Time) error
}

// Store persists only aggregate counters grouped by a UTC calendar day.
// It does not store client addresses, user agents, cookies, identifiers, or
// individual event records.
type Store struct {
	db *sql.DB
}

// Open creates an independent SQLite database for anonymous site metrics.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("metrics: empty database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("metrics: create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("metrics: open sqlite: %w", err)
	}
	// A single connection lets SQLite serialize concurrent increments without
	// exposing callers to transient write-lock errors.
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS daily_metrics (
			day TEXT PRIMARY KEY,
			visits INTEGER NOT NULL DEFAULT 0,
			downloads INTEGER NOT NULL DEFAULT 0
		)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("metrics: initialize storage: %w", err)
		}
	}
	return nil
}

// Record atomically increments one counter for the supplied UTC day.
func (s *Store) Record(ctx context.Context, event EventType, occurredAt time.Time) error {
	var statement string
	switch event {
	case EventVisit:
		statement = `INSERT INTO daily_metrics (day, visits, downloads) VALUES (?, 1, 0)
			ON CONFLICT(day) DO UPDATE SET visits = visits + 1`
	case EventDownload:
		statement = `INSERT INTO daily_metrics (day, visits, downloads) VALUES (?, 0, 1)
			ON CONFLICT(day) DO UPDATE SET downloads = downloads + 1`
	default:
		return ErrInvalidEvent
	}

	_, err := s.db.ExecContext(ctx, statement, occurredAt.UTC().Format("2006-01-02"))
	if err != nil {
		return fmt.Errorf("metrics: record event: %w", err)
	}
	return nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
