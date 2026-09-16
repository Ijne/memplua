# Data model and persistence

KnowledgeCrawler uses one SQLite database with three logical data flows:
processing evidence, human review, and canonical knowledge. One file allows the
final review application and export outbox to commit atomically.

The default file is `<data_dir>/knowledge.db`. Migrations are versioned through
SQLite `PRAGMA user_version`. Legacy prototype databases are intentionally not
imported, modified, or deleted.

## Identity and time

- Persisted domain objects use stable UUID strings.
- Names, titles, and theses are mutable values, never external identities.
- Times are stored as UTC Unix milliseconds.
- Canonical terms and thoughts carry monotonic object versions.
- The complete graph carries a monotonic graph revision.

## Processing tables

| Table | Purpose |
|---|---|
| `app_runs` | process executions and final state/error |
| `source_sessions` | independently controlled capture sessions |
| `chunks` | durable transcript/manual text units in source order |
| `analysis_batches` | non-overlapping focus windows and extraction diagnostics |
| `analysis_batch_chunks` | ordered context/focus membership |
| `conspects` | topic-level draft containers produced by one extraction |
| `conspect_terms` | extracted term candidates |
| `conspect_thoughts` | extracted thought candidates |
| `conspect_links` | candidate thought-to-term refs |
| `candidate_provenance` | evidence and anchor chunk links |
| `jobs` | leased durable work queue |

An `AnalysisBatch` belongs to one source session. Each successful batch may
produce multiple `Conspect` rows but performs one LLM extraction request.

## Review tables

| Table | Purpose |
|---|---|
| `review_items` | one user-facing term or thought decision |
| `review_matches` | deterministic canonical shortlist snapshots |
| `review_item_variants` or embedded variant JSON | grouped incoming candidates retained for split/edit |
| `review_relationships` | thought-to-term dependencies inside the conspect |
| `review_taxonomy_resolutions` | map/create/remove decision for unknown tags |

Review state is a draft layer. Editing or resolving these rows does not mutate
canonical tables. A conspect is applyable only when every item and every required
taxonomy value is resolved and all surviving relationships are valid.

The supported object decisions are `create`, `update`, `keep_existing`, and
`reject`. `update` and `keep_existing` store the observed target version so apply
can detect stale review decisions.

## Canonical graph tables

| Table | Purpose |
|---|---|
| `terms` | canonical name, description, version, timestamps |
| `thoughts` | canonical thesis, body, version, timestamps |
| `thought_terms` | many-to-many semantic relation |
| `tags` | enabled system and user taxonomy entries |
| `term_tags`, `thought_tags` | universal tag assignments |
| `taxonomy_aliases` | normalized aliases pointing to canonical tags |
| `graph_meta` | singleton current graph revision |
| `revisions` | object snapshots written by graph revision |
| `canonical_provenance` | applied object back-links to review/candidate/chunk |

System tags seed broad domains and useful note roles. They are suggestions, not
hard-coded entity types. The model receives the enabled vocabulary. Unknown
values are proposals and cannot silently extend the taxonomy.

`knowledge.Snapshot` is the read-only exporter/query representation containing
terms, thoughts, links, taxonomy, and the graph revision. Export formats are not
stored in canonical tables.

## Jobs and leases

`jobs` stores kind, JSON payload, status, attempt count, availability time,
lease owner/expiry, last error, and timestamps. Claiming is transactional. A
running job whose lease expires can be recovered by another worker. Payloads are
small identifiers, not copies of the underlying domain data.

Current job kinds:

- `extract_analysis_batch`;
- `prepare_conspect_review`;
- `export`.

## Events

`user_events` is the durable notification history. Events have stable IDs,
timestamp, severity, type, human message, resource reference, structured data,
and read state. SSE subscriptions replay persisted events before switching to
the in-memory live bus, using the event ID as a reconnect cursor.

Developer logs are separate files and are not part of the user-event model.

## Transaction boundaries

Important atomic operations include:

- store a chunk and attach it to the collecting batch;
- close a batch and enqueue extraction once;
- persist extraction, conspects, candidates, provenance, and preparation jobs;
- persist a complete prepared review snapshot;
- apply one conspect, its graph changes, revisions, provenance, and export job;
- claim, renew, complete, retry, or fail a durable job.

## Migration policy

Schema changes are append-only migration steps in `internal/storage/sqlite`.
Each migration must:

1. run inside a transaction where SQLite permits it;
2. preserve canonical knowledge and pending review work;
3. update `user_version` only after success;
4. include an integration test from the previous schema;
5. avoid reading unrelated legacy files.

Schema v4 folded the former `areas` taxonomy into universal tags and preserved
existing classifications and aliases. This historical migration is why some
tests mention areas even though the current domain does not expose them.

## Direct database access

The database is an implementation detail, not a public integration API. External
tools should use the REST query endpoints or an exporter. Reading the file while
the application is active must account for WAL mode; writing it directly can
violate review, versioning, provenance, and outbox invariants.
