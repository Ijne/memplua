// Package observability separates developer diagnostics from user-facing events.
// Developer logs use slog and rotating files; AppEvents are durable SQLite rows
// that can be replayed over SSE and marked as read.
package observability
