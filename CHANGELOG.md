# Changelog

All notable operator-facing changes to Dolly are documented here.

## Unreleased

### Runtime safety

- **Updater rollback** — Windows helper revalidates target and candidate after parent exit; trusts only verified backups; joins rollback failures and preserves recovery artifacts (#185).
- **TUI database runtime config** — TUI sessions honor configured PostgreSQL statement timeout and custom pool limits with fail-closed redacted errors (#186).
- **Run-scoped clone pool config** — Clone pool size travels through `clone.Options` with per-run serialization so sequential and concurrent runs cannot leak global bridge state (#187).
- **TUI clone runtime config** — TUI clone applies configured timeouts and permission-cache enablement, path, and TTL before side effects (#188).

### Release integrity

- **Stable exact-main tag enforcement** — Shared `scripts/validate-release-tag.sh` accepts only stable `vX.Y.Z` tags whose commit matches protected `main`; PR CI and Release workflow share the same admission contract (#189).

## [0.3.3](https://github.com/VicenteOlmos/dolly/releases/tag/v0.3.3) — 2026-08-03

### Release integrity

- **Immutable fix-forward** — v0.3.3 republishes verified v0.3.2 functionality as a fresh immutable release; public v0.3.2 remains available unchanged.

## [0.3.2](https://github.com/VicenteOlmos/dolly/releases/tag/v0.3.2) — 2026-08-03

### Runtime safety

- **Clone cancellation** — `dolly clone` cancels execution and source analysis on Ctrl-C and SIGTERM (#122).

### Self-update

- **`dolly update`** — installs the latest stable GitHub release after checksum verification; `dolly update --check` verifies without replacing; Windows defers replacement to a hidden helper when the executable is locked (#142).

## [0.3.1](https://github.com/VicenteOlmos/dolly/releases/tag/v0.3.1) — 2026-07-31

### Runtime safety

- **External dotenv files** — discovered dotenv files are read without changing permissions or metadata; broad Unix group/other permissions emit a constant redacted warning and loading continues (#114).

### Onboarding

- **Clone-first quick start** — bilingual README guidance makes install → current-directory database variables → `dolly clone` the primary onboarding path, with accurate default `schema-replay` fidelity boundaries (#115).

## [0.3.0](https://github.com/VicenteOlmos/dolly/releases/tag/v0.3.0) — 2026-07-30

### Runtime safety

- **Local secret files** — rejects world-readable `config.jsonc` and `.env` before loading credentials; tightens owner-only permissions when possible (#55).
- **Permission cache** — serializes concurrent clone preflight cache updates; persists cache atomically with portable file locking (#55).
- **DSN parsing** — preserves configured statement timeouts for libpq keyword and URI connection strings (#55).
- **Command timeouts** — enforces bounded timeouts on connection commands (#55).
- **TUI worker delivery** — cancelled dump, restore, and clone jobs exit without blocked sends; progress backpressure drops safely under saturated channels (#55).

### Clone safety

- **Schema scope** — explicit `--schemas` overrides saved profile defaults; selected schemas apply to schema-replay and logical-stream planning (#56).
- **Template SkipCreate** — rejects template clone when `clone.skip_create` is set (#56).
- **Physical backup targets** — accepts preflight-approved empty target directories; rejects non-empty targets; forwards TLS parameters to `pg_basebackup` (#56).
- **Sequence monotonicity** — schema-replay restores sequences once; logical-stream preserves monotonic sequence values after copied rows (#56).
- **TUI clone targets** — refreshes target connection when strategy changes; warns when sanitization cannot apply to the selected strategy (#56).

### Dump and restore safety

- **Multi-schema dump** — selects multiple schemas in dump scope (#57).
- **Empty artifact guard** — fails closed when dump would produce empty output (#57).
- **Deterministic subsets** — capped child-table subset closure is repeatable across runs (#57).
- **Sequence capture and sync** — fails closed on sequence capture errors; sequence synchronization never moves values backward (#57).
- **Parallel dump artifacts** — preserves prior parallel dump directory contents on publish failure (#57).
- **Partial-state manifest** — retains `.dolly-restore-partial-state.json` across parallel restore retries (#57).
- **Schema capture** — publishes `schema.sql` via atomic replacement; orphaned temps isolated on interruption (#57).
- **Trusted schema replay** — replays trusted `schema.sql` in a single transaction with rollback on statement error or cancellation (#57).

### Verification

- Windows amd64 build and test-compile coverage for runtime, clone, and dump/restore safety paths (#55, #56, #57).
- PostgreSQL 16 integration coverage for permission cache concurrency, clone strategy contracts, parallel artifact preservation, partial-state retention, atomic schema capture, and transactional schema replay (#55, #56, #57).

## [0.2.0](https://github.com/VicenteOlmos/dolly/releases/tag/v0.2.0) — 2026-07-28

### Added

- **Operator documentation** — README recognition-first mode decision table and six copyable recipes (exact selectors, selector files, keyset chunk/resume, shared-snapshot parallel dump, acknowledged parallel restore, safe end-to-end combinations) with expected artifacts (`metadata.json`, checkpoints, optional `schema.sql`, partial-state manifest), compatibility boundaries, and safety warnings.
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
