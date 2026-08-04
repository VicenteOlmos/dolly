# Roadmap

Evidence-led status for shipped foundations and pending product outcomes. See `README.md`, `CHANGELOG.md`, and `docs/release.md` for operator detail.

## Status snapshot

| Area | Status | Notes |
|------|--------|-------|
| TUI (connect, schema, dump, history restore, clone, config) | Shipped | Interactive cockpit; no scheduling controls |
| CLI PostgreSQL dump, restore, clone | Shipped | NDJSON dumps, strategies, selection, chunking, slow-connection |
| Configuration and connections | Shipped | `config.jsonc`, saved profiles, `--connection`, `--json` |
| Self-update and install safety | Shipped | Checksum-verified install; `dolly update` / `--check` |
| Release integrity gates | Shipped | Immutable SemVer tags, exact-main admission, preflight |
| v0.3.4 (runtime safety + release integrity) | Shipped | Operational hardening only — not milestone completion |
| Slow-connection mode v1–v3 | Shipped | Keyset chunks, checkpoint/resume, retry, composite PKs |
| Scheduling / cron integration | **Pending** | Not shipped |
| TUI scheduled-job controls | **Pending** | Not shipped |
| MySQL engine support | **Pending** | PostgreSQL only today |
| Continuous incremental selected-table sync | **Pending** | Not shipped |

## Shipped foundations

### TUI

- `dolly tui` — interactive connect, schema inspection, dump, dump-history restore, clone, and configuration from a real terminal (TTY required).
- Reads `config.jsonc` from the current directory; no CLI flags on the TUI entrypoint.
- Restore from local dump history in the TUI; arbitrary directories use `dolly restore --input`.

### CLI, configuration, and data workflows

- **Dump** — numbered NDJSON dump directories; schema scope; table include/exclude selectors; percent sampling with FK closure; parallel workers with shared snapshot; keyset chunk and slow-connection modes; `dump list` for local history without a DB connection.
- **Restore** — load Dolly dumps into PostgreSQL; refuses zero-table dumps; transactional default; optional parallel non-atomic restore with explicit acknowledgements.
- **Clone** — `schema-replay`, `template`, `logical-stream`, and `physical-backup` strategies; `.env` discovery; `dolly clone -ff` for non-interactive use.
- **Config** — `dolly config init` / `show`; project-scoped or encrypted connection stores; CLI `--connection` profiles.
- **Update** — `dolly update` installs latest stable release after checksum verification; `--check` verifies without replacing (Windows defers locked-binary replacement to a helper).
- **JSON** — `dump`, `restore`, `clone`, `version`, and `update` support `--json` envelopes on stdout/stderr.

PostgreSQL is the only supported engine. Capabilities above are bounded to documented CLI/TUI workflows — not scheduling, MySQL, or continuous incremental sync.

### Release and operational safety

- Installers verify release archives against per-release `checksums.txt` before install; SemVer tags are immutable; only the latest release receives security fixes.
- Maintainer gates: `make preflight`, shared `scripts/validate-release-tag.sh` exact-main admission for stable `vX.Y.Z` tags, fail-closed release workflow (see `docs/release.md`).
- **v0.3.4** — runtime safety (updater rollback, TUI/clone pool and timeout config, run-scoped clone pool serialization) and release integrity (stable exact-main tag enforcement). Does **not** complete scheduling, MySQL, or continuous incremental sync milestones.

## Completed: slow-connection mode for PostgreSQL replicas

First iteration (v1):

- ✅ `dolly dump --slow-connection` — implemented.
- ✅ Stream tables in primary-key chunks instead of one long table query.
- ✅ Use keyset pagination (`WHERE pk > $last ORDER BY pk LIMIT n`), not `LIMIT/OFFSET`.
- ✅ Fail clearly for tables without a single-column primary key.

Tradeoff: this mode favors connection survivability over one globally consistent snapshot. It is intended for replica exports over slow links, not for exact point-in-time backups.

Completed follow-ons:

- ✅ Configurable chunk size — v3 (`--chunk-size`, `dump.slow_chunk_size`).
- ✅ Per-table checkpoint metadata and resume support — v2.
- ✅ Optional retry/backoff for transient network failures — v3 (`--retry-max`, `--retry-base`; broad retry, idempotent chunks).
- ✅ Support for composite/multiple primary keys — v3 (row-tuple keyset pagination, checkpoint format migration via discard).

## Future milestones (pending)

### Scheduling and TUI

- **Pending** — Integrate cronjob support: track scheduled jobs and/or trigger them directly from the TUI.

### Database engines

- **Pending** — MySQL as the next supported engine.

### Incremental sync

- **Pending** — Scheduled sync for selected tables as a continuous replica.
- **Pending** — Apply only the necessary inserts and deletes so each run stays fast.
