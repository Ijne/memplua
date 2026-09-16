# Runtime, interfaces, and lifecycle

## Hosts and commands

The same `internal/runtime.Runtime` is embedded by two hosts:

- `desktop` (default): Wails windows, tray, and an in-memory API credential;
- `serve`: headless process plus contributor browser panel and token file.

`doctor` performs read-only configuration/model checks plus a temporary
data-directory write probe. `export --target obsidian` opens the database,
reads a canonical snapshot, exports it, and exits without starting workers or
models. `version` prints the linked version.

## Startup order

1. CLI loads defaults, TOML, environment, and flags, then validates.
2. Desktop acquires the single-instance lock before opening runtime resources.
3. Runtime reserves the API TCP listener. Port conflict therefore fails early.
4. Logger and versioned SQLite store open.
5. Event bus, settings manager, source manager, application, model supervisor,
   LLM client, handlers, workers, exporters, and API server are composed.
6. `Application.Run` records/recoveries the `AppRun` and starts all runners with
   a shared `errgroup` context.
7. Desktop waits for `/healthz`, then creates only the widget. Other WebViews are
   lazy to avoid startup races and unnecessary memory.

Missing model files do not prevent status/settings/review access. The supervisor
reports `unavailable` and creates an actionable user event. A database or API
listener failure is fatal because core durability/control cannot be guaranteed.

## Application state

Persisted states are `starting`, `running`, `paused`, `degraded`, `stopping`, and
`stopped`. Component health can make a running status appear degraded without
terminating the process. Unexpected runner failure cancels sibling runners and
finishes the app run as degraded.

At startup the store closes interrupted source sessions, closes their collecting
batches, and makes abandoned leased/transient failed work retryable. This is
runtime recovery, not migration from prototype files.

## Managed model process

When `models.managed=true`, `inference.Supervisor`:

1. validates executable/model paths and loopback URL;
2. terminates only a stale process proven to match its recorded executable/PID;
3. starts llama-server with application-owned arguments;
4. attaches it to Windows process-lifecycle protection;
5. polls `/health` until `startup_timeout`;
6. exposes `starting`, `ready`, `restarting`, `unavailable`, or `stopped` health;
7. restarts unexpected exits with exponential backoff up to one minute;
8. responds to a stalled inference request by cycling the process;
9. kills the owned child during application shutdown.

Server stdout/stderr is discarded; lifecycle facts go through structured logs.

## Desktop UI

The React application lives in `frontend/desktop`; the Wails host lives in
`internal/desktop`. There are five logical windows:

- `widget` — always-available compact status/source controls;
- `review` — primary user workflow;
- `settings` — basic, model, and advanced settings;
- `source` — complete ordered focus text for a conspect;
- `graph` — current read-only graph view.

Closing a window hides it. Tray Quit performs the real graceful shutdown. Window
bounds are stored in `<data_dir>/ui-state.json`, clamped to current monitors, and
do not belong in the database.

The frontend calls Wails only for desktop capabilities: bootstrap, window
operations, autostart, pickers, and quit. Knowledge/source operations use the
same REST API as other clients. React Query owns request caching; the SSE client
invalidates affected queries and reconnects with `Last-Event-ID`.

Desktop authentication is automatic. Runtime generates a random credential for
the process, Wails returns it to the embedded page in memory, and fetch attaches
it as a bearer header. It is neither a cloud API key nor persisted user setup.

## Browser developer UI

`serve` embeds `internal/ui` at `/ui/`. Static assets and `/healthz` are public
on loopback; data routes require the local bearer token. If no inline token is
configured, the server reads or creates `api.token`. The browser keeps the token
only in `sessionStorage`.

The developer panel exposes batches, jobs, raw/normalized extraction diagnostics,
manual text submission, and other technical state intentionally omitted from the
end-user desktop UI.

## REST and SSE

`api/openapi.yaml` is the normative transport specification. Endpoint groups:

- health/status and compact desktop state;
- source discovery and session start/pause/resume/stop/finalize;
- manual text ingest;
- conspect list/detail/source text and review decisions;
- graph, search, taxonomy, and settings queries;
- job inspection/manual retry and developer batch diagnostics;
- notification history/read state and replayable event stream;
- manual Obsidian export and application shutdown.

All data routes require bearer authentication. CORS accepts same-origin or the
single configured/desktop origin. Desktop projections replace internal errors
and identifiers with safe codes and user-oriented fields.

SSE first reads durable history after `Last-Event-ID`, subscribes to the live
event bus, and sends keepalive traffic. Event IDs disambiguate events sharing the
same millisecond, so reconnect does not skip them.

## Shutdown

Tray Quit, the shutdown endpoint, OS signal in `serve`, or a fatal runner error
cancels the root context. Source intake stops, native resources close once,
workers finish/cancel their handler and release leases, HTTP performs bounded
shutdown, the managed model terminates, app-run state is persisted, event
subscribers close, and then SQLite/log files close.

`Runtime.Close` is idempotent but must be called after `Run` finishes. Desktop's
shutdown hook enforces that ordering.
