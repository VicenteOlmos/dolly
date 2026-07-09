# Physical Backup Clone Strategies

For large or continuously-written databases, built-in dump/restore strategies may be too slow or cause downtime. Dolly supports automated physical cloning via `pg_basebackup` (`physical-backup` strategy), plus manual logical replication workflows.

## Canonical strategy names

Clone strategies split into two families:

- **Logical single-DB** (`template`, `schema-replay`, `logical-stream`) — operate on a single database within an existing PostgreSQL cluster.
- **Physical cluster** (`physical-backup`) — copies the entire PostgreSQL data directory via `pg_basebackup`.

| Canonical | Aliases | Behavior |
|-----------|---------|----------|
| `logical-stream` | `copy-stream`, `streaming-copy` | In-process COPY streaming between databases |
| `physical-backup` | `replication` | `pg_basebackup` physical cluster clone |

All aliases remain accepted in config, CLI flags, and the TUI.

## Quick decision guide

| Scenario | Recommended approach | Downtime | Complexity |
|----------|---------------------|----------|------------|
| Same PostgreSQL host, small DB (< 1 GB) | `dolly clone --strategy template` | Brief | Low |
| Cross-host, moderate size, batch workload | `dolly clone --strategy schema-replay` | Minutes | Low |
| Cross-host, large size, need speed | `dolly clone --strategy physical-backup` | Brief | Medium |
| Near-zero downtime, continuous sync | Logical replication (manual) | None | High |

## Automated pg_basebackup (`physical-backup`)

Dolly wraps `pg_basebackup` when you select the `physical-backup` strategy (alias: `replication`). The result is a **PostgreSQL data directory** (full cluster copy), not a single database on an existing server.

```bash
dolly clone -ff \
  --strategy physical-backup \
  --target-dir /path/to/empty/data \
```

Or set `clone.target_dir` in `config.jsonc` and run `dolly clone -ff --strategy physical-backup`.

### Preflight prerequisites

| Check | Requirement | Remediation |
|-------|-------------|-------------|
| Source reachability | Source DSN connects | Fix host, port, credentials, network |
| `wal_level` | `replica` or `logical` | Set `wal_level = replica` and restart PostgreSQL |
| `max_wal_senders` | `>= 2` | Increase `max_wal_senders` and reload |
| Role privilege | `REPLICATION` or superuser | `ALTER ROLE ... WITH REPLICATION` |
| Client tools | `pg_basebackup` on PATH | Install PostgreSQL client tools matching source major |
| Target directory | Empty or non-existent path | Provide a writable empty directory for `-D` |

Preflight does **not** connect to a target database — there is none until the new instance starts.

### What dolly does

1. Run `pg_basebackup -h <host> -p <port> -U <user> -D <target-dir> -Fp -Xs -P -v`
2. Write `<target-dir>/postgresql.auto.conf` with `primary_conninfo` (includes `application_name=dolly_clone`)
3. Create `<target-dir>/standby.signal` so the clone starts as a streaming replica
4. Print next-steps: `pg_ctl -D <target-dir> start`, `pg_isready`

### Recovery configuration

After a successful base backup, dolly writes:

```
primary_conninfo = 'host=... port=... user=... password=... application_name=dolly_clone'
```

into `postgresql.auto.conf`, and touches `standby.signal`. Start the new instance with:

```bash
pg_ctl -D /path/to/empty/data start
pg_isready -h localhost -p <port>
```

### Caveats

- Copies the **entire cluster** (all databases), not a single database
- Requires disk space equal to the source cluster size on the target machine
- Cross-major version replication is not supported by `pg_basebackup`
- v1 does not check available disk space before copying
- Interactive target-directory prompt is deferred; use `-ff` with `--target-dir` or `clone.target_dir`

## Logical replication (manual)

Best for: selective tables, near-real-time sync, cross-version migration.

1. On the publisher: `CREATE PUBLICATION my_pub FOR TABLE users, app.events;`
2. On the subscriber: `CREATE SUBSCRIPTION my_sub CONNECTION 'host=publisher ...' PUBLICATION my_pub;`
3. Monitor lag with `pg_stat_subscription`.

Caveats: does not replicate DDL, sequences, or large objects by default. Requires PostgreSQL 10+.

## When to use other dolly built-in strategies

- `template`: same-instance clones with no external tools.
- `schema-replay`: dump/restore across instances when downtime is acceptable.
- `logical-stream` (aliases: `copy-stream`, `streaming-copy`): streaming COPY for cases where `pg_dump` overhead is undesirable. Recommended for large single-database cross-server clones.

These built-ins are simpler but are not designed for multi-terabyte or 24/7 production loads.

## Sanitization

| Strategy | Row sanitization | Why |
|----------|-----------------|-----|
| `schema-replay` | Yes — dump pipeline applies `SanitizeByPattern` to NDJSON rows | Dump engine has a logical row hook |
| `logical-stream` | No (today) | pgx `COPY TO/FROM` bypasses the dump pipeline |
| `template` | No | `CREATE DATABASE … TEMPLATE` is byte-level on source |
| `physical-backup` | No — physically impossible | `pg_basebackup` copies the data directory; no logical row hook |
