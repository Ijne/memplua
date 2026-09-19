# Architecture

## Design goal

memplua keeps uncertain machine interpretation separate from trusted
user knowledge. Realtime capture may lose an unfinished in-memory audio segment,
but once text is stored as a chunk, every later transition is durable,
retryable, and designed to be idempotent.

## Four contours

```text
┌──────────────────── capture ────────────────────┐
│ WASAPI -> Silero -> Whisper -> text Chunk       │
└───────────────────────────────┬─────────────────┘
                                v
┌────────────────── durable processing ───────────┐
│ AnalysisBatch -> extraction Job -> Conspect(s)  │
│ -> deterministic PrepareReview Job              │
└───────────────────────────────┬─────────────────┘
                                v
┌──────────────────── human review ───────────────┐
│ item decisions + taxonomy decisions             │
│ -> atomic ApplyConspect                          │
└───────────────────────────────┬─────────────────┘
                                v
┌──────────────── canonical knowledge ────────────┐
│ Term + Thought + ThoughtTerm + Tag + revisions  │
│ -> read APIs / Obsidian / future projections    │
└─────────────────────────────────────────────────┘
```

## Dependency direction

Domain packages define their own ports. Implementations depend inward on those
contracts and are wired only in `internal/runtime`.

```text
cmd/memplua     internal/desktop
          \                 /
           internal/runtime                 composition
              /   |   \
       internal/app | internal/processing   orchestration
           /        |        \
 internal/ingest  review  knowledge         domain contracts
           \        |        /
          internal/storage/sqlite           infrastructure

internal/audio + internal/inference          native/model adapters
internal/api + internal/ui                   transport adapters
internal/export/obsidian                     output adapter
internal/observability                       logs and durable events
```

No domain package imports the desktop frontend, HTTP server, SQLite driver, or
Obsidian representation. `research/` is a separate Python project and is not a
production dependency.

## Package ownership

| Package | Owns | Must not own |
|---|---|---|
| `cmd/memplua` | CLI parsing and command selection | business logic |
| `internal/runtime` | object graph and resource ownership | domain rules |
| `internal/app` | root lifecycle and source sessions | native capture details |
| `internal/audio` | audio contracts and WASAPI capture | durable processing |
| `internal/inference` | local models, HTTP inference, process supervision | canonical mutations |
| `internal/ingest` | chunks, batches, conspects, jobs, worker semantics | UI and SQL |
| `internal/processing` | durable job orchestration | persistence details |
| `internal/review` | review vocabulary and deterministic matching | graph writes |
| `internal/knowledge` | canonical graph contracts | exporter formatting |
| `internal/storage/sqlite` | migrations and repository implementations | HTTP presentation |
| `internal/api` | authenticated REST/SSE transport | domain decisions |
| `internal/desktop` | Wails windows and OS integration | direct database access |
| `internal/ui` | contributor browser assets | end-user desktop behavior |
| `internal/export/obsidian` | Markdown projection and manifest | source of truth |
| `internal/observability` | logs and user events | workflow decisions |
| `internal/native` | platform capability registration | general orchestration |

## Non-negotiable invariants

1. Canonical knowledge changes only through `sqlite.Store.ApplyConspect`.
2. Every applied object is traceable to a review item, conspect, candidate, and
   evidence chunk.
3. LLM output cannot request deletion of canonical data.
4. Focus chunks do not overlap between analysis batches. A bounded tail from the
   previous batch is context only and cannot be cited as new evidence.
5. Every post-chunk asynchronous stage is represented by a durable leased job.
6. A job is completed only after its result transaction commits.
7. A canonical update checks the target version; conflicts roll back and queue
   fresh deterministic matching.
8. Thought-to-term links are applied only when both reviewed endpoints survive.
9. Obsidian writes only files recorded in its own manifest.
10. API listeners and managed model URLs must be loopback addresses.
11. Transcripts and prompt/response bodies are not written to developer logs by
    default. Diagnostic copies live in access-controlled SQLite instead.
12. The old prototype database and JSON files are not migrated or deleted.

## Composition

`runtime.New` validates configuration, reserves the API listener before opening
SQLite, creates logging and storage, registers sources, constructs the
application and model supervisor, installs job handlers/workers, and builds the
HTTP server. Reserving the listener first makes port conflicts fail before any
database or model side effect.

`app.Application.Run` records an `AppRun`, creates an `errgroup` derived from the
root context, and runs the source manager and every registered runner. The first
unexpected runner error cancels siblings. Normal cancellation releases active
job leases and closes resources through their owner.

## Interfaces live with their consumers

Examples:

- `ingest.Queue` is consumed by `ingest.Worker`;
- `review.Repository` is consumed by `review.Service` and `review.Preparer`;
- `knowledge.Exporter` is consumed by processing;
- `app.RuntimeRepository` is consumed by `Application` and `SourceManager`.

The SQLite store implements several small interfaces. This is intentional: one
physical database supplies atomicity, while domain packages remain independent
of SQL.

## Decisions and current limitations

The accepted decisions are recorded in `docs/adr`. Current deliberate limits:

- Windows-first native capture;
- one SQLite file, not a distributed database;
- local models selected manually; no download manager;
- raw audio is not retained;
- no automatic canonical deletes;
- no import of prototype databases;
- Obsidian is the only implemented exporter;
- installer, updater, retention UI, and rich graph editing remain future work.
