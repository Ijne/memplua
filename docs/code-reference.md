# Code reference

This is a guide to responsibilities and important entry points, not a duplicate
of generated Go documentation. Run `go doc -all ./internal/<package>` for exact
signatures and read interface comments next to the code.

## `cmd/knowledgecrawler`

- `main` prints terminal errors and returns a non-zero exit status.
- `run` selects `desktop` by default and dispatches subcommands.
- `loadRuntimeOptions` applies config, environment, and CLI precedence.
- `serve` runs the shared runtime under an OS-signal context.
- `launchDesktop` transfers ownership to the Wails shell.
- `doctor` probes configuration, paths, assets, and external LLM health.
- `export` performs a one-shot canonical snapshot export.

## `internal/runtime`

- `Options` supplies the effective config, config path, version, and host mode.
- `New` is the composition root and failure-safe resource constructor.
- `Runtime.Run` delegates lifecycle to `app.Application`.
- `Runtime.Close` idempotently closes listener, SQLite, and log writers.
- `httpRunner.Run` turns the pre-reserved listener into a context-aware server.

Call `Run` once and wait for it before `Close`.

## `internal/app`

- `Runner` is the common blocking `Run(context.Context) error` contract.
- `Application.AddRunner` registers owned background components before Run.
- `Application.Run` records app state and supervises all runners with errgroup.
- `Application.Status` combines app, queue, source, review, and component health.
- source command methods delegate to `SourceManager`.
- `SourceManager.Register` installs a named platform-independent factory.
- `Start`, `Stop`, `Resume`, and `Sessions` manage persisted executions.
- `SourceFactory` describes availability separately from source construction.

The manager owns source goroutines and cancellation. Source implementations own
their native resources and close them exactly once.

## `internal/audio`

- `VoiceDetector` abstracts stateful VAD (`IsSpeech`, `Reset`, `Close`).
- `Transcriber` abstracts cancellation-aware speech-to-text.
- `CaptureConfig` contains realtime timings and VAD parameters.
- `NewWASAPISource` builds microphone or loopback capture.
- `WASAPISource.Run` captures, segments, transcribes, and emits chunks.
- `WASAPISource.Close` idempotently releases COM/model resources.
- `ErrNativeUnavailable` explains an untagged or incomplete native build.

## `internal/inference`

- `NewLlamaClient` configures the OpenAI-compatible completion client.
- `WithLanguageProvider` makes future requests use live language settings.
- `WithLifecycle` connects readiness and stalled-request recovery.
- `LlamaClient.Extract` performs one validated batch extraction.
- `NewSupervisor` configures managed/external model lifecycle state.
- `Supervisor.Run` owns launch, readiness, restart, and termination.
- `WaitReady`, `Restart`, and `Health` form the client/supervisor contract.
- `NewWhisper` / `Whisper.Transcribe` wrap whisper.cpp.
- `NewSilero` / `Silero.IsSpeech` wrap ONNX Runtime.

`internal/inference/prompts.Build` combines the preserved user prompt, the
language-specific pipeline protocol, input sections, and allowed tags.
`prompts.Version` is stored beside each extraction result.

## `internal/ingest`

Domain types:

- `Chunk` — smallest durable text unit with source order;
- `BatchInput` — non-evidence context plus unique focus;
- `AnalysisBatch` — batching state and extraction diagnostics;
- `TermCandidate`, `ThoughtCandidate`, `TopicExtraction`, `Extraction` — strict
  LLM result model;
- `Conspect` — one extracted topic prepared for review;
- `Job` — leased durable task and retry state.

Functions and ports:

- `NormalizeExtraction` validates evidence, refs, tags, and duplicates.
- `NormalizedValues` cleans and deduplicates labels.
- `Worker.Run` claims, renews, handles, retries, fails, and completes jobs.
- `Handler`, `Queue`, `Repository`, `Extractor`, `Source`, and `TextSubmitter`
  are consumer-owned interfaces implemented by adapters.

## `internal/processing`

- `Coordinator.Run` closes expired/idle batches on a ticker.
- `ExtractHandler.Handle` enforces review pressure, runs one extraction, and
  persists success or diagnostic failure.
- `PrepareReviewHandler.Handle` delegates deterministic review preparation.
- `ExportHandler.Handle` selects exporters, reads one snapshot, and reports
  completion; auto jobs are ignored when auto-export is disabled.

## `internal/review`

- `Item` represents one term/thought decision, incoming variants, matches,
  relationships, final value, target version, and status.
- `Decision` and `TaxonomyDecision` are validated write commands.
- `Service` is the use-case facade for list/get/decide/split/taxonomy/apply.
- `Preparer.Prepare` builds or refreshes review items for one conspect.
- `Similarity` scores normalized token and Unicode trigram overlap.
- `Shortlist` filters by kind/threshold, limits size, and stabilizes ties.

Review repository methods save draft state; only `ApplyConspect` crosses into
canonical knowledge.

## `internal/knowledge`

- `Term` and `Thought` are versioned canonical nodes.
- `ThoughtTerm` is the implemented canonical edge.
- `TaxonomyEntry` and aliases define controlled universal tags.
- `Snapshot` is the read-only graph/export boundary.
- `Repository` supplies canonical search and taxonomy resolution to consumers.
- `Exporter` names and renders one independent projection.
- `Clean` normalizes display whitespace; `Normalize` also case-folds identity
  comparison values.

## `internal/storage/sqlite`

Construction and schema:

- `Open` creates directories, opens SQLite, configures pragmas, migrates, and
  performs runtime recovery.
- `WithBatchPolicy` supplies batching rules before Open.
- `Close` closes the database.

Processing:

- `SubmitText` creates a synthetic source/chunk through the normal pipeline.
- `SaveChunk`, `FinalizeSource`, and `SweepBatches` maintain unique focus batches.
- `BeginExtraction`, `SaveBatchExtraction`, and `SaveBatchFailure` persist the
  extraction state machine and diagnostics.
- `GetConspect` and `ConspectChunks` reconstruct review evidence.

Queue:

- `Enqueue`, `Claim`, `Renew`, `Complete`, `Retry`, and `Fail` implement leases.
- `ListJobs`, `RetryFailedJob`, and `Stats` support operations and UI.

Review and apply:

- `SavePreparedReview`, `SaveReviewDecision`, `SplitReviewItem`, and
  `SaveTaxonomyDecision` change only review state.
- `ApplyConspect` validates and writes the complete canonical transaction; on a
  version conflict it queues rematching after the failed transaction rolls back.

Queries/runtime:

- `ListTerms`, `ListThoughts`, `FindTerm`, `FindThought`, `ListTaxonomy`, and
  `Snapshot` implement canonical reads.
- app-run, session, event, notification, and compact desktop methods implement
  their small consumer interfaces.

## `internal/api`

- `New` validates/authenticates server configuration and registers routes.
- `Handler` returns middleware-wrapped routes for a caller-owned HTTP server.
- `Run` is retained for hosts that let the API own listening directly.
- `WithFrontend`, `WithTextIngestor`, and `WithBatchRepository` enable optional
  adapters without widening the core constructor.
- `desktop.go` contains end-user projections and safe error/event mapping.

Keep route additions synchronized with `api/openapi.yaml` and transport tests.

## `internal/observability`

- `NewLogger` builds JSON rotation and optional readable console output.
- `Bus.Publish` persists first and then fans out a user event.
- `Subscribe` returns a bounded channel plus mandatory unsubscribe function.
- `History`, `MarkRead`, and `RecentWarnings` delegate durable queries.
- `Bus.Close` closes subscribers and rejects later live publication.

## `internal/export/obsidian`

- `New` configures an output directory.
- `Exporter.Name` returns the stable job/config identifier `obsidian`.
- `Exporter.Export` loads its manifest, allocates collision-safe filenames,
  renders terms/thoughts, atomically writes them, removes only formerly owned
  files, and atomically updates the manifest.

## `internal/config`

- `Default`, `DefaultPath`, and `DatabasePath` provide portable defaults.
- `Load` parses strict TOML, applies environment, resolves paths, and validates.
- `Config.Validate` enforces loopback, ranges, language/theme, and owned flags.
- `Manager.Current` returns a safe value copy.
- `Manager.Update` applies an allowlisted patch and atomically persists TOML.
- `WithDataDir` rebases managed paths for a CLI override.
- `RestartKeys` reports settings that construction-time components cannot apply
  live.

## `internal/desktop`, `internal/ui`, and `internal/native`

- `desktop.Run` owns Wails, runtime readiness, lazy windows, tray, and shutdown.
- exported `Shell` methods are the intentionally narrow Wails bridge.
- `ui.New` returns the embedded contributor panel HTTP handler.
- `native.RegisterSources` provides the same microphone/loopback descriptors in
  native and non-native builds, with accurate availability reasons.

## Frontend modules

- `src/App.tsx` bootstraps Wails/API, language, theme, query client, and routing
  by logical window.
- `lib/api.ts` owns authenticated requests and safe error shape.
- `lib/events.ts` parses/reconnects SSE and invalidates cached views.
- `lib/bridge.ts` is the complete Wails capability boundary.
- `features/widget` controls sources and opens primary windows.
- `features/review` owns autosave, entity decisions, taxonomy, apply, and source
  text.
- `features/settings` maps typed settings to basic/model/advanced forms.
- `features/graph` is a read-only current graph view.

CSS Modules keep window styles local; `styles/global.css` owns tokens, reset,
themes, and native window behavior.
