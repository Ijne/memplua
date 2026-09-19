# AGENTS.md

## Scope

- Treat this directory (`go/`) as the project root for commands and edits.
- Do not inspect, edit, build, or depend on sibling `../llama.cpp/`; it is environment-only.
- Preserve unrelated user changes and prototype/runtime data.

## Architecture

- `cmd/memplua` is the composition root and implements `serve`, `doctor`, `export`, and `version`.
- `internal/app` owns application lifecycle and concurrent named source sessions.
- `internal/ingest` owns chunks, conspects, durable job contracts, leases, retries, and worker cancellation.
- `internal/processing` wires the batch coordinator, extraction, deterministic review preparation, and exporter job handlers.
- `internal/knowledge` owns canonical `Term`, `Thought`, `ThoughtTerm`, and controlled taxonomy contracts.
- `internal/review` produces structured review items and deterministic match shortlists; LLM output must never mutate the canonical graph directly.
- `internal/storage/sqlite` owns versioned migrations and repository implementations in the new `knowledge.db`; legacy `knowledgecrawler.db` files are never imported or removed.
- `internal/audio` and `internal/inference` isolate native adapters behind `windows && native` build tags.
- `internal/export/obsidian` is only a projection of the canonical graph.
- `internal/api` is the loopback REST/SSE boundary for the future UI; keep `api/openapi.yaml` synchronized.
- `internal/ui` is the embedded local control panel; it calls only authenticated API contracts and must not bypass review or storage boundaries.
- `internal/observability` separates structured developer logs from durable user events.
- `research` is contributor-only. Production Go must not import or launch it.

## Invariants

- Prototype `.sqlite/db.db`, JSON chunks/conspects, model files, and existing vault output are never migrated or overwritten implicitly.
- IDs are stable UUIDs. A term name or thought thesis is not an external identity.
- Every canonical mutation is transactionally tied to an resolved review item through atomic Apply Conspect.
- LLM-generated delete operations are forbidden.
- Producers own and close output channels; all blocking operations accept and observe `context.Context`.
- Once a chunk is in SQLite, every later stage is a durable, retryable, idempotent job.
- Do not log transcripts or prompt/response bodies by default.
- Do not add machine-specific paths, model names, URLs, or run modes to production code.

## Development

- Run Go commands from this directory.
- Use `gofmt` on changed Go files and keep package-level tests beside behavior.
- Primary checks are `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- `go test -tags native ./internal/audio` checks the WASAPI adapter without Whisper headers.
- Full `-tags native` validation requires the external Whisper/ONNX toolchain and model runtime; use `scripts/dev/build-native.ps1` with an explicit `-WhisperRoot`.
