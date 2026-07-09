# Changelog

All notable operator-facing changes to Dolly are documented here.

This project has no tagged public release yet. Keep upcoming work under `Unreleased` until a version is intentionally cut.

## Unreleased

### Installer readiness

- **Added** curl-based installer script for future GitHub Releases with OS/architecture detection, optional checksum verification, sudo-aware installation, and placeholder repository safety.
- **Documented** curl install usage and release checklist for the public repo VicenteOlmos/dolly.

### Initial local release readiness

- **Added** public-facing README structure with local build, quickstart, command overview, saved connection basics, safety links, and development commands.
- **Added** database safety documentation covering credentials, saved connections, destructive restore behavior, sanitization limits, clone strategy risks, and production safeguards.
- **Added** local release-readiness documentation for preflight checks, versioned local builds, first local commit review, and future public release steps.
- **Added** local Makefile helpers for release preflight and versioned binary builds using the public module path github.com/VicenteOlmos/dolly.
- **Added** MIT license for the project.

### Progress reporting

- **Added** numeric progress events for `dump`, `restore`, and `clone` operations with table-level granularity (phase, current, total, elapsed).
- **Added** TUI progress bar with percentage and ETA on dump, restore, and clone screens.
- **Added** CLI stderr progress bar: inline `\r` redraw on TTY, plain one-line events when redirected. Degrades gracefully with ETA placeholder until enough data points.
- **Deprecated** `clone.Options.ProgressFn func(string)` in favor of `clone.Options.ProgressEvent func(ProgressEvent)`. The old field is kept for one minor version.

### Strategy taxonomy cleanup

- **Removed** `production-scale` alias; `--strategy production-scale` now returns the canonical `unknown clone strategy` error.
- **Renamed** preflight matrix key `copy-stream` → `logical-stream`; test names updated accordingly.
- **Clarified** strategy families in CLI help, TUI prompt, config comments, and docs: logical single-DB (`template`, `schema-replay`, `logical-stream`) vs physical cluster (`physical-backup`).
- **Documented** sanitization contract: only `schema-replay` redacts row data; `physical-backup` cannot sanitize because it copies the physical cluster directory.

### TUI UX audit

- **Startup:** Welcome screen removed; the TUI opens directly on **Connection**.
- **Profiles:** F-key profile actions replaced with letter keys on the saved-list panel (`e` edit, `a` add, `r` rename, `d` delete, `t` test).
- **Safety:** Destructive actions (delete profile, quit with unsaved changes, cancel in-flight work) now require confirmation in a modal.
- **Inputs:** Text fields support cursor movement and editing (arrow keys, Home/End, insert/delete at cursor).
- **Password:** Masked passwords always display as eight asterisks when non-empty (fixed length).
- **Feedback:** In-progress connect, dump, and clone operations show a spinner.
- **Schema:** Next-step hint added on the schema screen.
- **Help & status:** Help is a split two-column layout; status bar trimmed to five segments or fewer.
