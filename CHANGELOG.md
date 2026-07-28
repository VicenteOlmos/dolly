# Changelog

All notable operator-facing changes to Dolly are documented here.

## Unreleased

- **Docs** recognition-first operator workflows in README (mode decision table, six copyable recipes) and pending `0.2.0` release notes below.

## 0.2.0

Pending — not tagged or published yet. Merge the docs PR, complete SDD archive, then tag protected `main` as `v0.2.0` per [docs/release.md](docs/release.md).

### Added

- **Exact table selection** — repeatable `--include-table` / `--exclude-table` and newline-delimited `--include-table-file` / `--exclude-table-file` selectors; include-narrow/exclude-win planning; credential-free provenance in `metadata.json` (#25, #27, #29).
- **Selective keyset chunking** — `--chunk-table` / `--chunk-table-file` stream named tables with PK-based checkpoints and resume; provenance-safe directory reuse (#31, #32).
- **Parallel table dump** — `--workers` / `dump.workers` (default `1`, max `16`) export tables from one read-only repeatable-read snapshot; metadata published last on success only (#34, #35).
- **Parallel table restore** — `--workers` / `restore.workers` (default `1`, max `16`) restore FK dependency levels concurrently; sequences synchronize after table data (#37, #38, #39).

### Safety

- Chunk and slow-connection modes reject parallel dump workers (`workers > 1`).
- Parallel dump rejects `--no-transaction`, subset modes, and chunk/slow policies; requires `db.max_open_conns >= workers+1`.
- Parallel restore requires `--no-transaction --yes --ack-partial-state`, conflict policy `error`, and rejects `--replace`, `--trust-schema-sql`, and skip/upsert.
- Parallel restore is non-atomic (per-table commits); `.dolly-restore-partial-state.json` is retained on failure and removed only after full success.
- Parallel dump coordinator monitors snapshot exporter liveness and cancels workers on coordinator failure (#41).

### Verification

- PG16 integration coverage for exact selection, chunk/resume, shared-snapshot parallel dump, and acknowledged parallel restore.

## 0.1.1 — 2026-07-20

- **Fixed** release workflow action pinning and shell-safe release tags.
- **Fixed** sequence synchronization for empty restored tables, physical backup target directories, Windows locks, installer replacement, dump validation, and TUI dump help.
- **Added** root security reporting policy.

## 0.1.0 — 2026-07-10

First public release.

### Installer and release

- **Added** curl/PowerShell installers for GitHub Releases with OS/architecture detection and checksum verification.
- **Added** tag-driven Release workflow that builds six platform archives plus `checksums.txt` after vet, race, installer, and Postgres integration gates.
- **Added** MIT license and public module path `github.com/VicenteOlmos/dolly`.

### Safety and CI

- **Added** fail-closed live clone integration when `pg_dump` is missing or major-mismatched after `DOLLY_TEST_PG_DSN` opt-in; CI/release install PostgreSQL 16 client tools.
- **Added** redacted clone subprocess stderr on failure; success no longer streams raw stderr.
- **Added** CLI warnings for unsanitized clones and `skip_create` partial-state risk; `info: target database:` before destructive replace paths.
- **Added** TUI restore confirm modal shows redacted target DSN (clone parity).
- **Added** live integration coverage for schema capture and restore COPY.

### Progress reporting

- **Added** numeric progress events for `dump`, `restore`, and `clone` with table-level granularity.
- **Added** TUI progress bar with percentage and ETA; CLI stderr progress bar on TTY.
- **Deprecated** `clone.Options.ProgressFn func(string)` in favor of `clone.Options.ProgressEvent`.

### Strategy taxonomy

- **Removed** `production-scale` alias; unknown strategies return a canonical error.
- **Clarified** logical single-DB strategies (`template`, `schema-replay`, `logical-stream`) vs physical cluster (`physical-backup`).
- **Documented** sanitization contract: only `schema-replay` redacts row data.

### TUI

- Opens on Connection (no welcome screen); letter-key profile actions; confirmation modals for destructive actions; editable text fields; spinner feedback; split help layout.

### Docs

- Public README, database safety docs, and local release-readiness checklist.
