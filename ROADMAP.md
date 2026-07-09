# Roadmap

## Dump module improvements

### Slow connection mode for PostgreSQL replicas

First iteration (implemented v1):

- ✅ `dolly dump --slow-connection` — implemented.
- ✅ Stream tables in primary-key chunks instead of one long table query.
- ✅ Use keyset pagination (`WHERE pk > $last ORDER BY pk LIMIT n`), not `LIMIT/OFFSET`.
- ✅ Fail clearly for tables without a single-column primary key.

Tradeoff: this mode favors connection survivability over one globally consistent snapshot. It is intended for replica exports over slow links, not for exact point-in-time backups.

Future improvements:

- ✅ Configurable chunk size — implemented v3 (`--chunk-size`, `dump.slow_chunk_size`).
- ✅ Per-table checkpoint metadata and resume support — implemented v2.
- ✅ Optional retry/backoff for transient network failures — implemented v3 (`--retry-max`, `--retry-base`; broad retry, idempotent chunks).
- ✅ Support for composite/multiple primary keys — implemented v3 (row-tuple keyset pagination, checkpoint format migration via discard).

## Scheduling and TUI

- Integrate cronjob support — track scheduled jobs and/or trigger them directly from the TUI.

## Database engines

- MySQL as the next supported engine.

## Incremental sync

- Scheduled sync for selected tables as a continuous replica.
- Apply only the necessary inserts and deletes so each run stays fast.
