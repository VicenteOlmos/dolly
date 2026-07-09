# Security and database safety

Dolly handles database credentials, dump artifacts, and clone workflows. Use this checklist before pointing it at production or production-like data.

## Quick production checklist

- [ ] Use a least-privilege PostgreSQL role for the operation.
- [ ] Confirm the target database is disposable before `restore --replace` or clone overwrite workflows.
- [ ] Keep DSNs, passwords, dumps, and saved connection files out of Git.
- [ ] Enable encrypted saved connections when profiles contain reusable secrets.
- [ ] Validate sanitization results manually before sharing dumps outside a trusted boundary.
- [ ] Prefer a staging rehearsal before touching production.

## Credentials and secrets

Prefer environment variables, `.env`, or a local config that is excluded from Git. Avoid pasting production DSNs into shell history when possible.

Dolly can read DSNs directly (`--dsn`), from config/env resolution, or from saved connections. Every path can expose secrets through local files, terminal scrollback, shell history, process inspection, or logs if handled carelessly.

## Saved connections

Saved connections are disabled by default. When enabled, store location is controlled by `connections.scope`, `connections.path`, and `connections.encrypt` in `config.jsonc`.

- Project-scoped stores are convenient, but easier to commit by accident.
- XDG-scoped stores reduce repository risk, but still live on disk.
- Encrypted stores require `DOLLY_CONNECTIONS_KEY`, a 32-byte standard base64 key. Losing the key means losing access to encrypted profiles.

## Destructive restore and replace

`dolly restore --replace` truncates target tables before inserting dump data. That is useful for disposable databases and dangerous everywhere else.

Safer alternatives:

- `--on-conflict error` stops on the first conflicting row.
- `--on-conflict skip` leaves existing rows unchanged.
- `--on-conflict upsert` overwrites conflicting rows with dump values.

Default restore is atomic. `--no-transaction --yes` is an advanced opt-in mode that commits per table and can leave partial progress after failure. Use it only when partial progress is acceptable and you have a recovery plan.

## Sanitization limits

Sanitization is best-effort, pattern-based redaction for known sensitive column names and text values. It reduces accidental exposure; it does not prove that a dump is safe to publish or share.

Current strategy coverage:

| Workflow | Sanitization |
|----------|--------------|
| `dump` | Applies when `sanitization.enabled` is true. |
| `clone --strategy schema-replay` | Applies through the dump/restore path. |
| `clone --strategy logical-stream` | Does not redact row data today. |
| `clone --strategy template` | Does not redact row data. |
| `clone --strategy physical-backup` | Cannot redact; it copies the physical cluster directory. |

## Clone strategy risks

Logical single-database strategies (`template`, `schema-replay`, `logical-stream`) and physical cluster cloning (`physical-backup`) have different blast radiuses.

- `template` is fast on the same PostgreSQL instance, but copies the database as-is.
- `schema-replay` is more portable and can use sanitization, but still writes real data to the target.
- `logical-stream` is faster for some large copies, but bypasses sanitization today.
- `physical-backup` uses `pg_basebackup`, requires replication privileges, and copies the entire cluster data directory, not just one database.

See [physical-backup.md](physical-backup.md) before using physical clone workflows.
