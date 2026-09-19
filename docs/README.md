# Documentation map

This directory describes the code as it exists today. Planned behavior is
called out explicitly and must not be inferred from the current contracts.

## Choose a starting point

| Reader | Start here | Then read |
|---|---|---|
| User testing the application | [Runtime](runtime.md) | [Configuration](configuration.md), [Troubleshooting](troubleshooting.md) |
| Go contributor | [Architecture](architecture.md) | [Pipeline](pipeline.md), [Code reference](code-reference.md) |
| UI contributor | [Runtime](runtime.md#desktop-ui) | [OpenAPI](../api/openapi.yaml), [desktop README](../frontend/desktop/README.md) |
| Storage contributor | [Data model](data-model.md) | [Pipeline](pipeline.md), ADRs 0001/0002/0005 |
| Native/runtime contributor | [Runtime](runtime.md) | [Development](development.md), [Troubleshooting](troubleshooting.md) |
| Security reviewer | [Privacy and security](privacy-security.md) | [Data model](data-model.md), ADR 0004 |

## Documents

- [Architecture](architecture.md) — dependency direction, package ownership, and invariants.
- [Pipeline](pipeline.md) — every state transition from capture to export.
- [Data model](data-model.md) — canonical graph, processing data, review data, jobs, events, and migrations.
- [Configuration](configuration.md) — precedence, paths, every TOML section, and restart behavior.
- [Runtime](runtime.md) — commands, startup, desktop windows, API/SSE, models, and shutdown.
- [Windows installer](windows-installer.md) — per-user packaging, offline models, updates, and release prerequisites.
- [Code reference](code-reference.md) — package-by-package guide to important types and functions.
- [Development](development.md) — setup, build tags, tests, prompts, migrations, and contribution workflow.
- [Troubleshooting](troubleshooting.md) — common startup, audio, LLM, pipeline, and export failures.
- [Privacy and security](privacy-security.md) — trust boundaries, retention, secrets, and known limits.
- [Architecture decision records](adr/) — why durable SQLite, operation review, Term/Thought, local API, and analysis batches were chosen.

## Source-of-truth hierarchy

When documentation and implementation differ, use this order and fix the lower
priority source in the same change:

1. database constraints and transactional code;
2. Go domain contracts and tests;
3. `api/openapi.yaml` for transport shape;
4. ADRs for accepted architectural decisions;
5. explanatory documents in this directory;
6. README examples.

Configuration defaults come from `internal/config.Default`, not from a local
`config.toml`. The checked-in `config.example.toml` is an annotated example.
