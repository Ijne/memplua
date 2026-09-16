# Privacy and security model

KnowledgeCrawler processes potentially sensitive speech and notes. The current
design is local-first, but local-first is not equivalent to zero risk.

## Data that remains local by design

- audio samples are held in memory and are not intentionally written to disk;
- Silero, Whisper, and the managed llama-server run locally;
- REST and model URLs are validated as loopback addresses;
- SQLite, logs, configuration, tokens, and exports live in user-selected local
  directories;
- no telemetry or cloud synchronization is implemented.

If `models.managed=false`, the configured external process must still be on a
loopback URL. Contributors must not relax that validation without a new security
design and explicit user consent.

## Persisted sensitive data

The SQLite database contains transcripts as chunks, model responses, conspects,
review decisions, canonical knowledge, provenance, and user events. Obsidian
exports duplicate approved canonical content as Markdown. Backups and filesystem
indexers may copy either location.

Developer logs contain operational metadata and errors, but should not contain
transcript, prompt, or response bodies by default. Error strings from third-party
libraries still require review before public bug reports are posted.

There is not yet a retention/deletion UI. Users must stop the application before
backing up, moving, or deleting the database. Secure deletion on SSDs is outside
the application's guarantees.

## Local API trust boundary

- listener: loopback only;
- authentication: constant-time bearer-token comparison;
- desktop credential: random per-process value, memory only;
- browser credential: generated token file, copied manually, kept in tab
  `sessionStorage`;
- CORS: same origin plus at most one configured origin;
- headers: CSP, no-referrer, no-sniff, and frame denial for embedded web UI.

Loopback authentication protects against unrelated web pages and accidental
local access, not against malware running as the same OS user. The API does not
provide multi-user authorization.

## Model and native-code trust

Model files, llama-server, whisper.cpp libraries, and ONNX Runtime are executable
or parser attack surfaces supplied by the user. Obtain them from trusted sources
and verify hashes. KnowledgeCrawler currently does not download or verify them.

Prompts are data sent to the local model. Model output is untrusted and passes
strict JSON parsing, evidence validation, deterministic matching, taxonomy
review, human decisions, and transactional validation before canonical storage.

## Export safety

The Obsidian exporter sanitizes filenames, disambiguates collisions with stable
IDs, writes atomically, and deletes only files listed in its manifest. Pointing
multiple KnowledgeCrawler instances at the same export directory is unsupported.

## Reporting a vulnerability

Do not open a public issue containing transcripts, tokens, database files,
absolute user paths, or exploitable details. Follow [SECURITY.md](../SECURITY.md).

## Known gaps before a public release

- choose and document supported Windows versions and dependency patch policy;
- publish verified model/runtime download instructions and checksums;
- add a user-facing retention and data-removal flow;
- threat-model autostart and installer/update signing;
- audit API request-size/rate limits and database file permissions;
- add secret scanning and dependency vulnerability checks in CI;
- document whether crash dumps can contain transient audio or credentials.
