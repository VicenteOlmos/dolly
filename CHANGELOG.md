# Changelog

All notable operator-facing changes to Dolly are documented here.

## Unreleased

- **Docs** public-release readiness: release-first publication ordering (private tag/assets before visibility), SemVer/latest-only support policy, immutable published assets, private security reporting, pre-public vs post-public recovery (no force-push rollback), and contributor preflight expectations in README, CONTRIBUTING, SECURITY, and `docs/release.md`.

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
