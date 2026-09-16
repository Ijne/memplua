# Open-source publication checklist

Documentation is only one part of publication readiness. Complete this list
before making the repository public.

## Blocking decisions

- [ ] Select an OSI-approved license and add `LICENSE`.
- [ ] Decide whether external contributions require a DCO, CLA, or neither.
- [ ] Configure a private vulnerability-reporting channel.
- [ ] Define supported Windows, Go, Node, Wails, and native dependency versions.
- [ ] Decide which model/runtime distributions can be recommended and under
      which licenses.

## Repository hygiene

- [ ] Audit Git history, not only the working tree, for tokens, personal paths,
      transcripts, databases, model files, and proprietary datasets.
- [ ] Verify `.gitignore` covers runtime data, `config.toml`, logs, models,
      checkpoints, datasets, exports, native binaries, and build artifacts.
- [ ] Run secret scanning and dependency/license scanning.
- [ ] Remove or replace personal screenshots and absolute paths in fixtures.
- [ ] Confirm every checked-in asset has a compatible license and attribution.
- [ ] Ensure generated desktop assets are reproducible from frontend source or
      document why they are committed.

## Automation

- [ ] Add CI for Go test, race, vet, frontend typecheck/unit tests, and OpenAPI
      validation.
- [ ] Add a Windows native integration job with pinned trusted dependencies.
- [ ] Add dependency update and vulnerability alert policy.
- [ ] Produce signed, checksummed release artifacts.
- [ ] Add issue and pull-request templates.

## Product and privacy

- [ ] Add first-run consent and clear recording indicators.
- [ ] Document data locations, retention, backup, and deletion in the UI.
- [ ] Add actionable model installation guidance with verified hashes.
- [ ] Complete soak/restart/overload tests for multiple sources and unavailable
      LLM conditions.
- [ ] Threat-model installer, updater, autostart, crash dumps, and local API.
- [ ] Review all user-facing RU/EN strings and accessibility behavior.

## Documentation maintenance

- [ ] Keep `api/openapi.yaml` synchronized with handlers and frontend clients.
- [ ] Require docs updates for configuration, migrations, prompts, and package
      ownership changes.
- [ ] Publish release notes with schema/config compatibility information.
- [ ] Replace the pre-release status and security placeholders after release.
