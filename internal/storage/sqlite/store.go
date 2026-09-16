package sqlite

import (
	"context"
	"crawler/internal/ingest"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store implements the application's small repository interfaces over one
// versioned SQLite database.
type Store struct {
	db          *sql.DB
	batchPolicy ingest.BatchPolicy
}

// Option configures a Store before its schema is initialized.
type Option func(*Store)

// WithBatchPolicy configures new analysis-batch boundaries.
func WithBatchPolicy(policy ingest.BatchPolicy) Option {
	return func(s *Store) { s.batchPolicy = policy }
}

// Open creates parent directories, opens SQLite, applies migrations, and
// recovers interrupted runtime/job state.
func Open(path string, options ...Option) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite: empty database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, batchPolicy: ingest.DefaultBatchPolicy()}
	for _, option := range options {
		option(store)
	}
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	for _, statement := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("sqlite initialize: %w", err)
		}
	}
	return migrate(ctx, s.db)
}

// Close releases the SQLite connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

func nullableTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := time.UnixMilli(value.Int64).UTC()
	return &parsed
}
