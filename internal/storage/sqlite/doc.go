// Package sqlite implements application, ingest, review, knowledge, event, and
// job repositories in one versioned SQLite database. Transactions enforce the
// review gate, provenance, graph revisions, and export outbox invariants.
package sqlite
