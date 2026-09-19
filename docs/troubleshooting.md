# Troubleshooting

Start with:

```powershell
./memplua.exe doctor --config ./config.toml
```

Then inspect the newest rotating log under the configured logging directory.
Do not post the database, token, transcripts, or complete logs publicly without
reviewing them for private data.

## Desktop does not appear reliably

- Launching with no subcommand starts `desktop`; `serve` opens a browser panel
  instead.
- A second desktop launch activates the existing single instance.
- Check Task Manager/tray before assuming no instance exists.
- A port conflict is detected before SQLite opens. Stop the other instance or
  set another loopback `api.listen`/`--listen` value.
- Current desktop builds wait for `/healthz` and create secondary WebViews lazily.
  Rebuild if using an older executable.

## `bind: Only one usage of each socket address`

Another process owns the configured API port. Find and stop the other
memplua/headless process, or choose another loopback port. Do not work
around this by binding a public network interface.

## Browser panel asks for a token

This is expected for `serve`. Read the file path printed at startup and paste
its content into the contributor panel. Desktop never asks the user for this
token; its per-process credential is automatic.

## Source is unavailable

Run `doctor`. Native audio needs a binary built with `-tags native`, plus valid
Whisper model, Silero model, ONNX Runtime, and runtime DLL/library paths. A
`-CoreOnly` build deliberately exposes microphone and loopback as unavailable.

## WASAPI capture fails or receives no signal

- Verify microphone permission in Windows privacy settings.
- Verify the selected default capture/render device is active.
- Loopback captures the default render endpoint; play audible content through
  that endpoint.
- Bluetooth and device format changes can invalidate an active client; stop and
  restart the source after changing devices.
- Inspect `audio capture started`, segment peak, VAD probability, transcription,
  and stored-chunk log events.

An untranslated numeric HRESULT comes from the Windows audio stack. Include the
code, device type, and sanitized nearby log fields in a bug report.

## llama-server configuration required

Check `models.llama_binary` and `models.llm_model` paths relative to the config
and data directory rules. For managed mode, `llm_url` must be loopback HTTP with
an explicit port. `server_args` cannot repeat flags owned by memplua.

If GPU offload is unavailable, set `gpu_layers = 0` or install a compatible
llama-server build. memplua reports configuration failure as a user
event while keeping settings/review available.

## “Organizing ideas” never finishes

- Check model health and the configured request timeout.
- Ensure `max_tokens` is appropriate for the model/context.
- Inspect failed/retry jobs and the batch diagnostic response in developer UI.
- A review-limit pause is intentional; resolve/apply existing review first.
- A stalled request asks the supervisor to restart the managed server; the
  durable job then retries without losing chunks.

## No review appears after transcription

The collecting batch may still be waiting for maximum duration or idle timeout.
Stop/finalize the source to close it immediately. Then check, in order:

1. chunk stored;
2. analysis batch closed;
3. extraction job state;
4. raw/normalized extraction and error;
5. generated conspects;
6. preparation job state.

## Review saves but cannot apply

Every entity and every unknown taxonomy value must be resolved. A thought cannot
reference a rejected term. If another conspect changed a selected canonical
target, apply reports a version conflict and refreshes the matches; review the
affected items again.

## Obsidian export problems

- Verify `export.obsidian_directory` is writable.
- Stop another exporter targeting the same directory.
- Only files in `.memplua-manifest.json` are owned; unrelated files are
  intentionally untouched.
- Export is downstream of graph commit. Retry the export job or use
  `memplua export --target obsidian`; do not re-apply the conspect.

## Config reports an unknown key

Configuration is strict. Remove misspellings and replace the old
`pipeline.window_size` / `pipeline.overlap` setup with the current batch keys in
`config.example.toml`. Keep a backup before hand-editing.

## Database/schema error

Do not point a current build at an experimental or prototype database. Choose a
new `storage.database` filename. Never delete the user's old database as part of
automatic recovery. For a reproducible bug, copy the file only after stopping
the application and sanitize its sensitive contents before sharing.
