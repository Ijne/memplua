# Processing pipeline

This document follows one piece of information from capture to export and states
which component owns each transition.

## 1. Source session

`app.SourceManager.Start` resolves a registered `SourceFactory`, creates a unique
`SourceSession`, persists it, and runs the source under a session context. The
microphone and loopback sources are independent and can run simultaneously.

Source states are `starting`, `recording`, `paused`, `stopped`, and `failed`.
Pause and stop cancel the current source. Pause preserves a resumable logical
session record; resume creates the next active execution for that session.
Stopping or pausing finalizes its collecting analysis batch.

Before accepting another emitted chunk, SourceManager checks durable queue
pressure. If the processing limit is reached, it returns backpressure and emits
a user event rather than silently discarding stored work.

## 2. Realtime native capture

The Windows `WASAPISource` owns WASAPI interfaces, VAD, transcriber, tickers, and
their cleanup. Its mode is either microphone capture or render-device loopback.

```text
WASAPI float samples
  -> signal/peak check
  -> Silero windows and maximum speech probability
  -> segment boundary (configured duration or shutdown)
  -> Whisper transcription with timeout
  -> emit text Chunk
```

Raw audio stays in memory and is not persisted. On graceful cancellation, the
source attempts to finish the current non-empty segment. A crash can therefore
lose only the unfinished audio segment; previously stored text is durable.

Builds without `windows && native` register microphone and loopback descriptors
as unavailable. The rest of the application still runs.

## 3. Chunk persistence and batching

`sqlite.Store.SaveChunk` is the durability boundary. It stores the chunk and
places it into the current collecting `AnalysisBatch` for the source session.

A batch closes on the first applicable condition:

- focus duration reaches `pipeline.conspect_max_duration`;
- no new text arrives for `pipeline.conspect_idle_timeout`;
- the source is paused, stopped, or explicitly finalized;
- recovery discovers a collecting batch from an interrupted session.

`processing.Coordinator` periodically calls `SweepBatches` to enforce time and
idle deadlines. Closing is idempotent and atomically queues one
`extract_analysis_batch` job.

Each chunk belongs to the `Focus` of exactly one batch. Up to
`context_tail_chars` Unicode characters from the previous focus are copied into
`Context`. Context improves continuity but is not new evidence; candidates must
cite focus chunk IDs.

## 4. Durable jobs

Workers claim one eligible job with an owner and lease expiration. While a
handler runs, the worker renews the lease every third of the lease duration.

| Outcome | Transition |
|---|---|
| Success | `running -> done` |
| Ordinary error below attempt limit | `running -> retry`, exponential delay capped at one minute |
| LLM/review backpressure | `running -> retry`, 30-second delay |
| Attempt limit reached | `running -> failed`, manual retry available |
| Root cancellation | lease released as an immediate retry |
| Worker crash | expired lease becomes claimable during recovery |

Job handlers are expected to be idempotent. Storage transactions and stable
source identifiers prevent duplicate conspects or graph writes after retries.

## 5. LLM extraction

`processing.ExtractHandler` loads the batch and stops early if it is already
extracted. It also enforces the review-queue limit before beginning another LLM
request.

`inference.LlamaClient.Extract`:

1. waits until the managed/external model lifecycle is ready;
2. loads the current allowed tag vocabulary;
3. builds the preserved user prompt plus the versioned batch protocol;
4. sends one completion request for the entire analysis batch;
5. consumes a normal or streaming llama-server response;
6. parses and normalizes the strict JSON extraction;
7. validates local refs and evidence against the focus chunks.

One batch can yield zero, one, or several topic conspects. Exact duplicate
candidates inside the response are coalesced while semantically distinct
variants remain visible for review. The raw response, normalized response,
model name, prompt version, and any parse error are stored for diagnosis. They
are not emitted to ordinary logs.

Saving a successful extraction and creating its conspects/jobs is atomic. A
replayed extraction job sees the committed batch state and performs no second
write.

## 6. Deterministic review preparation

Each conspect gets a `prepare_conspect_review` job. `review.Preparer` normalizes
case and whitespace, groups incoming variants, and asks the canonical repository
for a bounded shortlist of similar terms or thoughts. Similarity combines token
and Unicode character-trigram Dice scores; stable ordering makes retries
reproducible.

No LLM is called for matching or merging. Review items initially contain:

- the incoming candidate variants;
- provenance and relationships;
- exact/similar canonical matches and their versions;
- editable final value initialized from the candidate or selected target;
- a pending resolution.

Unknown tags become taxonomy resolutions. They must be mapped to an existing
tag, created with a reviewed name, or removed. Aliases such as `math` resolve to
their canonical entry before review.

## 7. User review

The user resolves every term and thought independently:

- `create` — create a new canonical object with a new UUID;
- `update` — replace the selected canonical object's value if its version is
  still current;
- `keep_existing` — map incoming candidates to the existing object unchanged;
- `reject` — discard the incoming candidates;
- `split` — divide grouped incoming variants into separate review items before
  choosing the above decisions.

Saving a decision changes review data only. The canonical graph remains
unchanged. Rejecting a term reopens dependent accepted thoughts until their
relationships are valid.

## 8. Atomic apply

`sqlite.Store.ApplyConspect` is the only canonical write boundary. One SQLite
transaction validates every item and taxonomy decision, verifies target
versions, advances the graph revision, writes terms/thoughts/tags/links,
persists revisions and provenance, marks review data applied, updates the
conspect, and queues automatic export.

If all items are rejected, the conspect becomes rejected without advancing the
graph. If a target version changed since review, the transaction rolls back,
the conspect returns to preparation, and a new matching job is queued.

## 9. Export

An export job reads a consistent `knowledge.Snapshot`. The Obsidian adapter
renders canonical objects only; it never reads conspect JSON. Writes use
temporary files plus atomic replacement. A manifest maps stable graph UUIDs to
owned filenames, prevents title collisions, and limits cleanup to files created
by this exporter.

Export failure does not roll back the graph. The job retries independently and
produces a user event.

## Recovery matrix

| Interruption point | Recovery behavior |
|---|---|
| Before chunk commit | unfinished audio/text may be lost |
| After chunk commit, before batch close | interrupted session batch is closed on next app run |
| During extraction request | job lease expires or is released; request may repeat |
| After response, before transaction commit | response may be requested again; no duplicate conspect is committed |
| During review preparation | job retries; deterministic IDs/state remain consistent |
| During manual review | saved decisions remain; unsaved browser edits do not |
| During ApplyConspect | SQLite rolls back the entire graph transaction |
| During export | canonical graph remains committed; export job retries |

## Backpressure

Two limits protect different user experiences:

- `processing_limit`: sources stop accepting additional chunks when durable
  processing is overloaded;
- `review_limit`: extraction pauses, but already captured text can continue to
  accumulate in chunks/batches until the processing limit is reached.

Backpressure is represented by retryable state and user events, never by
silently deleting durable chunks.
