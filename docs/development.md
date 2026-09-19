# Development guide

## Repository layout

The Go module root is this `go/` directory. Do not make production code depend
on a neighboring `llama.cpp` checkout; it is a developer-provided toolchain.

```text
api/                 OpenAPI contract
cmd/                 executable composition/commands
docs/                architecture and operations documentation
frontend/desktop/    end-user React/Wails frontend source
internal/            production Go packages
research/            isolated contributor-only Python project
scripts/dev/         local Windows build/run/smoke helpers
```

Runtime data, models, generated desktop assets, databases, vault output,
datasets, and checkpoints are not source code.

## Pure-Go workflow

```powershell
go test ./...
go test -race ./...
go vet ./...
```

Core tests use temporary SQLite databases, fake sources, and fake model servers.
They must not require Windows audio devices, model weights, or network access.

Start the developer host without native audio:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File ./scripts/dev/run.ps1 -CoreOnly -Config ./config.toml
```

## Native Windows workflow

Build whisper.cpp outside this repository, then provide its root explicitly or
through `MEMPLUA_WHISPER_ROOT`:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File ./scripts/dev/build-native.ps1 `
  -WhisperRoot C:/src/whisper.cpp
```

`native-env.ps1` validates headers/libraries and sets `CGO_CFLAGS`,
`CGO_LDFLAGS`, and runtime `PATH`. It does not download dependencies.

Build the complete desktop executable:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File ./scripts/dev/build-desktop.ps1 `
  -Native -WhisperRoot C:/src/whisper.cpp
```

Use `-CoreOnly` intentionally when testing UI/runtime without audio. Use
`-SkipFrontend` only when embedded assets are already current.

## Desktop frontend

```powershell
npm.cmd --prefix frontend/desktop ci
npm.cmd --prefix frontend/desktop run typecheck
npm.cmd --prefix frontend/desktop test
npm.cmd --prefix frontend/desktop run test:e2e
```

Playwright uses mocked domain data and visual snapshots across language, theme,
and DPI variants. Updating snapshots is a reviewed visual change, not a generic
test fix. The native smoke test uses the `desktopdebug` tag and an isolated data
directory; production builds disable that debugging channel.

## Adding a source

1. Implement `ingest.Source`; observe context on every blocking operation.
2. Make Run blocking and return errors instead of printing them.
3. Make Close idempotent and release every owned resource exactly once.
4. Register an `app.SourceFactory` in the platform adapter.
5. Preserve a stable source ID and descriptive kind.
6. Add fake-source lifecycle/cancellation tests and a platform integration test.
7. Document privacy and persistence implications.

## Adding a durable job

1. Add a `JobKind` and small identifier-only payload.
2. Define the consumed repository interface in the domain/processing package.
3. Implement an idempotent handler that accepts context.
4. Make the state/result transaction distinguish already-completed work.
5. Register the handler in `runtime.New`.
6. Define retryable, paused/backpressure, and permanent failure behavior.
7. Add restart/duplicate-delivery tests and update pipeline/data-model docs.

## Changing prompts or extraction JSON

The long prompts in `internal/inference/prompts/user_prompts.go` are curated
source assets. Do not replace them with abbreviated generated prompts. Add
protocol-specific requirements in `prompt.go`, bump `prompts.Version`, and keep
RU/EN behavior aligned.

Any shape change requires parser, normalization, provenance, fake-model,
storage, and review fixture updates. Old raw responses remain diagnostic data and
need not be rewritten.

## Changing SQLite

Never edit an already-released schema as though old databases do not exist.
Append a migration, advance `user_version` after success, and test migration from
the previous version plus fresh database creation. Preserve pending work and
canonical provenance. Do not import prototype files.

## Changing REST/SSE

Update in one change:

- handler and transport validation;
- `api/openapi.yaml`;
- server/desktop projection tests;
- frontend types/callers;
- applicable docs.

Avoid exposing internal SQL errors, chunk IDs, or raw model diagnostics in the
end-user desktop projection.

## Go documentation policy

Every package has a package comment. Exported declarations need comments that
explain responsibility, lifecycle, ownership, side effects, idempotency, and
error semantics—not merely repeat the identifier. Complex unexported functions
should document invariants or transaction boundaries. Keep the package guide in
`code-reference.md` synchronized when entry points change.

## Pull-request checklist

- format changed Go with `gofmt`;
- preserve user data and unrelated worktree changes;
- run checks proportional to the affected layer;
- add tests for failure/cancellation/restart paths, not only success;
- update OpenAPI, schema docs, prompts version, or ADR when applicable;
- verify logs do not reveal transcripts, prompts, credentials, or model output;
- avoid machine-specific paths and bundled model artifacts.
