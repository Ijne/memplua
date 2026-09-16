# Security policy

## Supported versions

KnowledgeCrawler has not made a stable public release. Security fixes currently
target the latest main branch only. This policy must be replaced with a version
support table when releases begin.

## Reporting a vulnerability

Please report vulnerabilities privately to the repository maintainer through a
private GitHub Security Advisory after the repository is published. Until that
channel exists, contact the maintainer through a non-public channel documented
on the repository profile.

Do not include real transcripts, model responses, API tokens, databases, logs,
absolute home-directory paths, or vault contents. Use synthetic reproductions.

A useful report contains:

- affected commit/version and Windows version;
- impact and required attacker position;
- minimal reproduction with synthetic data;
- whether desktop, `serve`, native audio, models, SQLite, or export is involved;
- suggested mitigation, if known.

The maintainer should acknowledge a report, assess severity, prepare a fix and
regression test privately when necessary, then coordinate disclosure. No fixed
response-time SLA is promised before the first stable release.

## Security boundaries

- API and managed model endpoints are loopback-only.
- API data routes require a bearer credential.
- Model output is untrusted and cannot bypass review/transaction validation.
- The SQLite database and Obsidian export may contain sensitive user knowledge.
- Third-party native binaries and model files are user-supplied and must be
  obtained from trusted sources.

See [docs/privacy-security.md](docs/privacy-security.md) for the detailed threat
model and known gaps.
