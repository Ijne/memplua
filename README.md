# memplua

memplua is a Windows-first, local-first application that turns live
speech and submitted text into a user-approved knowledge graph.

The application captures microphone or system-loopback audio, transcribes it
locally, groups non-overlapping text chunks into analysis batches, and asks a
local LLM to extract topical conspects. Nothing enters the canonical graph until
the user reviews every proposed term and thought and applies the complete
conspect in one transaction.

> Project status: **Alpha v2 (`0.2.0-alpha.2`)**. The storage and review safety
> boundaries, Windows desktop UI, online installer, and verified model setup are
> implemented. Long-running Windows release hardening is still in progress.

## Why the review boundary matters

```text
microphone / loopback / manual text
                 |
                 v
        durable text chunks
                 |
                 v
      non-overlapping analysis batch
                 |
                 v
       local LLM extraction
                 |
                 v
      one or more draft conspects
                 |
                 v
 deterministic matching + user review
                 |
                 v
       atomic canonical graph update
                 |
                 v
      Obsidian and future projections
```

The LLM cannot directly create, update, or delete canonical knowledge. It only
produces candidates with source-chunk provenance. Deletion is not an
LLM-generated operation.

## Current capabilities

- simultaneous named microphone and loopback sessions;
- local Silero VAD and Whisper transcription;
- managed or external llama.cpp-compatible inference server;
- durable SQLite batches, leases, retries, recovery, and backpressure;
- term/thought extraction with evidence provenance and universal tags;
- per-entity create, update, keep-existing, reject, and split review;
- transactional application with version-conflict rematching;
- canonical graph queries and an Obsidian exporter with an ownership manifest;
- Windows desktop UI, developer web panel, REST API, and replayable SSE events;
- RU/EN UI and conspect language settings.

## Install on Windows

Build or download the `memplua-0.2.0-alpha.2-windows-x64-setup.exe` release
artifact and run it as the current user. The compact installer downloads the
local CPU inference stack from its official publishers, verifies every artifact,
and then launches memplua. The first installation downloads about 2.8 GB.

See [Windows installer](docs/windows-installer.md) for packaging and diagnostic
details.

## Build from source

Requirements:

- Windows 10/11;
- Go 1.25.1 or newer;
- Node.js 22.12 or newer for rebuilding the desktop frontend;
- local llama-server, GGUF, Whisper, Silero, and ONNX Runtime files;
- a built external `whisper.cpp` tree for native audio builds.

Copy and edit the example configuration:

```powershell
Copy-Item config.example.toml config.toml
```

Build and start the desktop application:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File ./scripts/dev/build-desktop.ps1 `
  -Native -WhisperRoot C:/path/to/whisper.cpp

./memplua.exe --config ./config.toml
```

The desktop UI does not ask for an API key. It receives a per-process local
session credential through the Wails bridge. The `serve` developer panel uses a
token file because it runs in a normal browser.

For a pure-Go build without audio capture:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File ./scripts/dev/build-desktop.ps1 -CoreOnly
```

## Commands

```text
memplua [desktop]  Start the Windows desktop application (default).
memplua serve      Start the headless runtime and developer web panel.
memplua doctor     Validate directories, models, and native assets.
memplua export     Export the current canonical graph to Obsidian.
memplua version    Print the build version.
```

All commands accept `--config`. Run a command with `-h` for its exact flags.

## Documentation

Start with the [documentation map](docs/README.md).

- [Architecture and package boundaries](docs/architecture.md)
- [End-to-end processing and recovery](docs/pipeline.md)
- [Domain model and SQLite ownership](docs/data-model.md)
- [Configuration reference](docs/configuration.md)
- [Runtime, desktop, API, and shutdown](docs/runtime.md)
- [Code reference](docs/code-reference.md)
- [Development and testing](docs/development.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Privacy and security model](docs/privacy-security.md)
- [Windows installer and verified model setup](docs/windows-installer.md)
- [REST/SSE OpenAPI contract](api/openapi.yaml)
- [Architecture decisions](docs/adr/)
- [Release notes](CHANGELOG.md)
- [Русское введение](README.ru.md)

## Development checks

```powershell
go test ./...
go test -race ./...
go vet ./...

npm.cmd --prefix frontend/desktop ci
npm.cmd --prefix frontend/desktop run typecheck
npm.cmd --prefix frontend/desktop test
```

Native audio and real model execution require a separate Windows integration
run; ordinary Go tests do not require model files or CGO libraries.

## Repository policy

- Model weights, runtime data, transcripts, databases, datasets, checkpoints,
  and generated vaults must not be committed.
- The legacy prototype database and JSON files are never imported or deleted
  automatically.
- Production Go code must not import or execute contributor-only `research/`.
- Obsidian is a projection, not the canonical store.

See [CONTRIBUTING.md](CONTRIBUTING.md) before changing a domain boundary or
durability invariant.

## License

A public license has not yet been selected. Add an OSI-approved `LICENSE` file
before publishing the repository; until then, copyright law reserves all rights.
