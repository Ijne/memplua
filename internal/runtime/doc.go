// Package runtime is the shared composition root for desktop and headless hosts.
// It wires configuration, SQLite, sources, workers, inference, API, events, and
// exporters, and owns their resources until Run has stopped and Close is called.
package runtime
