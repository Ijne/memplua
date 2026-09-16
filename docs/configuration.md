# Configuration reference

KnowledgeCrawler has one typed TOML configuration. The authoritative defaults
and validation rules are in `internal/config/config.go`; an annotated complete
file is available as `config.example.toml`.

## Precedence and paths

Values are composed in this order:

1. compiled defaults;
2. TOML file;
3. `KNOWLEDGECRAWLER_*` environment variables;
4. command-line overrides (`--config`, `--data-dir`, and `--listen` where
   supported).

The default config path is `%AppData%\KnowledgeCrawler\config.toml` on Windows.
If that file does not exist, defaults are still usable, but model paths must be
configured before native/model features become available.

`data_dir` is resolved relative to the configuration file. Other relative
managed paths are resolved below `data_dir`, except paths beginning with `..`,
which are resolved from the config directory. The manager writes portable
relative paths when possible.

Unknown TOML keys and malformed environment values are errors. The deprecated
root `language` field maps to `conspect.language`. Deprecated floating-window
keys are accepted only for compatibility and do not restore the old algorithm.

## `[ui]`

| Key | Meaning | Values/default |
|---|---|---|
| `language` | desktop display language | `ru` or `en`; new installs follow Windows locale |
| `theme` | desktop theme | `system`, `light`, or `dark`; default `system` |
| `always_on_top` | keep compact widget above normal windows | default `true` |
| `start_with_windows` | register Wails autostart | default `false` |

## `[conspect]`

`language` controls the language requested from the extraction model. It is read
for each new extraction, so changing it does not require restart. Existing
conspects and canonical data are not translated.

## `[api]`

| Key | Meaning |
|---|---|
| `listen` | loopback host and port, default `127.0.0.1:7331` |
| `token` | optional inline token; avoid committing it |
| `token_file` | generated/read token for browser `serve` mode |
| `allowed_origin` | one optional CORS origin |

Non-loopback listeners are rejected. Desktop mode replaces configured API
credentials in memory with a random per-process credential and does not write it
to TOML or browser storage.

## `[models]`

| Key | Meaning |
|---|---|
| `managed` | start and supervise `llama-server` when true |
| `llama_binary` | local server executable |
| `llm_model` | GGUF model passed to the managed server |
| `llm_url` | loopback base URL; must include managed server port |
| `parallel` | llama-server parallel request slots and client concurrency |
| `server_args` | additional server arguments not owned by the application |
| `whisper_model` | local Whisper model |
| `silero_model` | local Silero ONNX model |
| `onnx_runtime` | ONNX Runtime shared library |
| `context_size` | llama-server context length |
| `gpu_layers` | layers requested on GPU; `0` means CPU |
| `startup_timeout` | readiness deadline for managed server |
| `request_timeout` | complete extraction deadline |
| `max_tokens` | maximum completion tokens; client also applies a safety bound |
| `temperature` | generation temperature from `0` through `2` |

The application owns model, host, port, context, GPU-layer, and parallel flags.
Those flags are rejected inside `server_args` to prevent contradictory launches.
With `managed=false`, KnowledgeCrawler never starts or stops the external model
process, but still waits for its health before inference.

## `[audio]`

| Key | Meaning |
|---|---|
| `tick_interval` | capture poll interval |
| `segment_length` | maximum in-memory segment before transcription |
| `transcription_timeout` | per-segment Whisper deadline |
| `vad_window` | samples evaluated by each Silero call |
| `vad_threshold` | probability threshold in `[0,1]` |

Smaller ticks reduce latency but increase wakeups. Raw audio is not retained.

## `[storage]`

`database` names the new versioned SQLite database, default `knowledge.db`.
Choose a new file when experimenting with incompatible development schemas. The
application never imports the old prototype database automatically.

## `[pipeline]`

| Key | Meaning |
|---|---|
| `conspect_max_duration` | maximum focus span before batch close |
| `conspect_idle_timeout` | close after no new text |
| `batch_sweep_interval` | coordinator deadline scan interval |
| `context_tail_chars` | bounded previous text supplied as non-evidence context |
| `similarity_threshold` | minimum deterministic match score `[0,1]` |
| `similarity_limit` | maximum candidates, currently validated `1..5` |
| `processing_limit` | durable queue pressure limit for source intake |
| `review_limit` | pending review limit before more extraction pauses |
| `workers` | concurrent durable workers |
| `max_attempts` | attempts before a job becomes manually retryable `failed` |
| `lease` | exclusive job ownership duration |
| `poll_interval` | idle worker polling interval |

## `[logging]`

`level` is `debug`, `info`, `warn`, or `error`. `directory` is the rotating-log
directory. `json` enables structured files, `console` enables readable stderr,
and `max_size_mb` / `backup_files` control rotation.

Do not enable transcript or prompt logging in contributions. Raw/normalized
responses already have a controlled diagnostic location in SQLite.

## `[export]`

`obsidian_directory` is the managed projection directory. `auto=true` queues an
export after a graph revision; manual exports remain available regardless.

## Environment variables

The supported names are the fields above prefixed with `KNOWLEDGECRAWLER_`.
Notable mappings include `DATA_DIR`, `DATABASE`, `UI_LANGUAGE`,
`CONSPECT_LANGUAGE`, `API_LISTEN`, `LLAMA_BINARY`, `LLM_MODEL`, `LLM_URL`,
`WHISPER_MODEL`, `SILERO_MODEL`, `ONNX_RUNTIME`, `MODEL_*`, `AUDIO_*`,
`CONSPECT_*`, `PROCESSING_LIMIT`, `REVIEW_LIMIT`, `WORKERS`, `MAX_ATTEMPTS`,
`JOB_LEASE`, `POLL_INTERVAL`, `LOG_*`, `OBSIDIAN_DIR`, and `AUTO_EXPORT`.

`KNOWLEDGECRAWLER_LLAMA_SERVER_ARGS` is a JSON string array. Booleans use Go
boolean syntax; durations use Go duration syntax such as `500ms`, `45s`, or
`10m`.

## Live settings and restart

The settings API accepts only the `SettingsPatch` allowlist. It validates and
writes the entire TOML atomically. UI language, theme, conspect language, and
some presentation/export settings apply live. Model executable/runtime options,
native model paths, worker topology, and similar construction-time values are
reported in `restart_keys` and take effect after restart.
