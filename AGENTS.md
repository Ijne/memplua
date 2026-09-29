# Repository Guidelines

## Project Structure & Module Organization

This directory is the Go module root (`crawler`) for memplua. `cmd/memplua/` composes the executable; `internal/` contains ingestion, inference, processing, review, SQLite storage, API, and desktop packages. Go tests live beside implementation files as `*_test.go`; exporter fixtures use `testdata/`.

`frontend/desktop/` holds React/TypeScript source, component tests, and Playwright checks under `e2e/`. Its build regenerates embedded assets in `internal/desktop/assets/`. `internal/ui/` contains the browser panel. `api/openapi.yaml` defines transport contracts; `docs/` contains architecture, operations, and ADRs.

## Build, Test, and Development Commands

Use Go 1.25.1+ and Node.js 22.12+. Run from this module root:

- `go build ./cmd/memplua`: build the default executable without the desktop shell or native audio.
- `go run ./cmd/memplua serve -config ./config.toml`: start the headless API/browser host using local configuration copied from `config.example.toml`.
- `go test ./...`, `go test -race ./...`, `go vet ./...`: run backend tests, race detection, and static checks.
- `npm.cmd --prefix frontend/desktop ci`: install locked frontend dependencies.
- `npm.cmd --prefix frontend/desktop run dev`: start Vite's browser preview.
- `npm.cmd --prefix frontend/desktop run build`: type-check and generate embedded assets.
- `npm.cmd --prefix frontend/desktop run typecheck`, `test`, or `run test:e2e`: check TypeScript, Vitest, or Playwright respectively.
- `npm.cmd --prefix internal/ui ci`, then `npm.cmd --prefix internal/ui test`: run browser-panel Node tests.

## Coding Style & Naming Conventions

Format Go with `gofmt` (tabs). Use lowercase package names and document exported declarations and lifecycle ownership. Follow existing TypeScript style: two-space indentation, single quotes, PascalCase components, and `*.module.css`. Keep RU/EN localization aligned.

## Testing Guidelines

Use Go's `testing` package, Vitest/Testing Library, and Playwright. Name frontend unit tests `*.test.ts` or `*.test.tsx`. No numeric coverage threshold is specified. Test cancellation, retries, idempotency, conflicts, and rollback where relevant. Ordinary backend tests must use temporary databases and fakes without models, devices, or external networking. Review intentional visual changes before updating snapshots.

## Commit & Pull Request Guidelines

History uses short imperative subjects such as `Update README`; no enforced commit prefix convention is evident. Keep commits focused. Describe the problem, chosen boundary, compatibility/security impact, and checks performed; include screenshots for visible desktop changes. Open an issue before changing domain or security boundaries. Update OpenAPI, migrations, prompt versions, and ADRs when applicable.

## Data Integrity & Privacy

LLM output must pass user review; `ApplyConspect` remains the transactional graph mutation boundary. Never commit local configuration, databases, tokens, models, or transcripts. Keep sensitive content out of default logs.
