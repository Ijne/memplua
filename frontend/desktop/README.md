# KnowledgeCrawler desktop frontend

This directory contains the end-user React UI embedded by the Windows Wails
shell. It is independent from the contributor browser panel in `internal/ui`.

## Stack

- React 19 and TypeScript;
- Vite;
- TanStack Query for server state;
- i18next for RU/EN localization;
- CSS Modules plus shared design tokens;
- Wails v3 beta.20 for desktop-only capabilities;
- Vitest/Testing Library and Playwright.

The Go and JavaScript Wails runtimes must stay on the same version.

## Commands

From this directory:

```powershell
npm.cmd ci
npm.cmd run dev
npm.cmd run typecheck
npm.cmd test
npm.cmd run test:e2e
npm.cmd run build
```

`build` type-checks, generates/copies icon assets, and writes the production
bundle to `internal/desktop/assets` for Go embedding. The normal desktop build is
orchestrated by `scripts/dev/build-desktop.ps1` from the module root.

## Runtime boundaries

The frontend never opens SQLite or calls native audio/model libraries.

- Domain commands and queries use the authenticated loopback REST API.
- Live invalidation uses replayable SSE.
- Wails bindings are limited to bootstrap, native window control, autostart,
  file/folder pickers, and quit.

On startup `App.tsx` calls `Shell.Bootstrap`, configures the API client with the
per-process in-memory credential, applies locale/theme, probes `/api/v1/ui/state`,
and renders the logical window selected by `?window=`. The credential is never
placed in a URL, Vite environment, localStorage, or sessionStorage.

A normal browser preview cannot perform a secure desktop bootstrap, so it shows
the preview/connection screen. Tests install a narrow fake desktop bridge.

## Logical windows

| Window | Feature | Responsibility |
|---|---|---|
| `widget` | `features/widget` | source controls, processing/review/health indicators, settings and quit |
| `review` | `features/review` | candidate decisions, matching, tags, autosave, and final apply |
| `source` | `features/review/SourceText` | complete ordered focus text without technical chunk IDs |
| `settings` | `features/settings` | basic, model, and advanced typed settings |
| `graph` | `features/graph` | read-only canonical terms/thoughts and relationships |

Secondary native windows are created lazily by Go. Closing hides a window;
application termination is explicit through widget/tray Quit.

## Important modules

- `src/lib/api.ts` — connection state, bearer attachment, JSON errors.
- `src/lib/bridge.ts` — complete typed Wails capability surface.
- `src/lib/events.ts` — SSE parser, `Last-Event-ID`, reconnect backoff, cache invalidation.
- `src/lib/preferences.ts` — live theme/language synchronization.
- `src/lib/types.ts` — transport view types; keep aligned with OpenAPI.
- `src/features/review/autosave.ts` — serial decision writes so later edits do
  not overtake earlier requests.
- `src/features/review/ItemEditor.tsx` — safe structured editing; users do not
  edit raw JSON.
- `src/features/review/TagSelector.tsx` — existing controlled tag selection.
- `src/features/review/TaxonomyEditor.tsx` — unknown tag map/create/remove.

## Review UX invariants

- Show one user-editable term/thought at a time with clear incoming and canonical
  comparison.
- Never expose raw JSON, chunk IDs, job IDs, leases, or internal errors.
- Tags are selected through controlled menus; unknown values have explicit
  taxonomy decisions.
- A saved item decision does not imply graph mutation.
- Apply is enabled only when the complete conspect is valid and resolved.
- Navigation waits for autosave; failure keeps the current item visible.
- Source text is available on demand in a separate window.

## Events and caching

HTTP responses are cached by stable query keys. SSE does not carry full domain
state; it invalidates relevant keys. On disconnect the client retries with
bounded exponential backoff and the latest event ID. Durable history prevents
missing notifications across application restarts.

## Tests

Component tests cover autosave order, navigation blocking, apply behavior,
taxonomy, localization keys, and SSE reconnect. Playwright uses synthetic data
and checks RU/EN, light/dark, common DPI scales, keyboard behavior, and visual
snapshots.

Update snapshots only after reviewing an intentional UI change:

```powershell
npm.cmd run test:e2e -- --update-snapshots
```

The native smoke harness builds with `desktop,desktopdebug` and uses an isolated
profile. Production builds use `desktop,production` and do not expose the debug
browser channel.

## Adding UI behavior

1. Prefer an existing REST contract; otherwise update Go handlers and OpenAPI in
   the same change.
2. Add/adjust transport types and query keys.
3. Use localized text from RU and EN dictionaries.
4. Preserve safe desktop projections rather than rendering developer payloads.
5. Test loading, empty, success, validation, network failure, and retry states.
6. Verify keyboard access, focus, DPI, and both themes for visible changes.

See the repository [runtime documentation](../../docs/runtime.md) and
[code reference](../../docs/code-reference.md) for backend ownership.
