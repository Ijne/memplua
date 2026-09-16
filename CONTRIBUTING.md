# Contributing to KnowledgeCrawler

Thank you for improving KnowledgeCrawler. The project processes private data and
maintains a user-approved knowledge base, so correctness on cancellation,
retries, and transaction boundaries matters as much as the happy path.

## Before starting

Read:

1. [architecture](docs/architecture.md);
2. [processing pipeline](docs/pipeline.md);
3. the ADR related to your change;
4. the package comment and nearby tests.

Open an issue before changing a domain boundary, storage format, review
semantics, prompt JSON shape, security boundary, or public API.

## Core rules

- LLM output never mutates canonical knowledge directly.
- `ApplyConspect` remains the single transactional graph mutation boundary.
- Persisted chunks are not lost or duplicated by retry/restart behavior.
- Stable UUIDs are identities; display strings are values.
- Sources and runners block in `Run(ctx)` and observe cancellation.
- Owners close channels and native resources exactly once.
- Do not log transcripts, prompts, responses, tokens, or secrets by default.
- Do not introduce machine-specific paths, model files, or external network
  requirements into production or ordinary tests.
- Do not inspect or depend on a sibling llama.cpp checkout from production code.
- Production Go must not import or launch `research/`.

## Setup and checks

Use Go 1.25.1+ and Node.js 22.12+. From the module root:

```powershell
go test ./...
go test -race ./...
go vet ./...

npm.cmd --prefix frontend/desktop ci
npm.cmd --prefix frontend/desktop run typecheck
npm.cmd --prefix frontend/desktop test
```

Native/model integration is a separate Windows check described in
[docs/development.md](docs/development.md). State clearly which checks you ran.

## Change expectations

- Format Go with `gofmt`.
- Add tests at the layer that owns the behavior.
- Cover cancellation, retry, idempotency, conflict, and rollback where relevant.
- Update `api/openapi.yaml` with route or payload changes.
- Add a versioned SQLite migration and migration test for schema changes.
- Preserve curated prompts; protocol changes require a prompt-version bump.
- Update the documentation map or code reference when ownership changes.
- Keep end-user desktop projections free of developer-only IDs and raw errors.

## Commits and pull requests

Prefer focused commits. A pull request should explain:

- the user or contributor problem;
- the chosen boundary and alternatives considered;
- persistence/recovery/security impact;
- tests performed;
- screenshots for visible desktop changes;
- configuration, migration, or compatibility notes.

Do not commit local `config.toml`, databases, tokens, logs, models, exports,
datasets, checkpoints, generated native libraries, or personal transcripts.

## Architecture decisions

Use an ADR for decisions that are difficult to reverse or affect multiple
packages: data stores, domain identity, review semantics, IPC/API boundaries,
batching, privacy, or runtime ownership. Number new records sequentially under
`docs/adr/` and include context, decision, consequences, and rejected options.

## License status

The repository does not yet contain a public license. Contributions should not
be solicited or merged from external authors until the maintainer selects a
license and adds an explicit contribution/licensing policy.
