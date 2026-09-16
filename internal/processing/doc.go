// Package processing contains durable job handlers. It closes analysis batches,
// calls extraction, prepares deterministic review data, and runs configured
// exporters while keeping orchestration separate from storage implementations.
package processing
