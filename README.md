<p align="center">
  <img src="./assets/readme/hero.svg" width="720" alt="Two identical cute Dolly sheep each hugging a large database marked DB beneath the Dolly name, with a sparkling duplication arc between them.">
</p>

<h1 align="center">Dolly</h1>

<p align="center">
  Local-first PostgreSQL CLI and TUI for dumping, restoring, and cloning databases.<br>
  <a href="README.es.md">Español</a>
</p>

Choose your path:

| If you want to… | Start here |
|---|---|
| Clone your database first | [Quick start: clone](#quick-start-clone) — install, add a `.env`, run `dolly clone`. |
| Work interactively | `dolly tui` — connect, inspect schemas, dump, and clone from a real terminal. |
| Script dump or restore | `dolly dump`, `dolly restore`, and `dolly clone` — use a DSN or saved connection. |

`dolly tui` has no flags, requires a TTY, and reads `config.jsonc` from the current directory.

## Install

Installers download the matching GitHub Release asset and verify it against that release's `checksums.txt` before installing it.

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | sh
```

Pin a release:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_VERSION=0.3.5 sh
```

Default install path: `/usr/local/bin`. Set `DOLLY_INSTALL_DIR` to install elsewhere.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Pin a release:

```powershell
$env:DOLLY_VERSION="0.3.5"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Default install path: `%LOCALAPPDATA%\Programs\dolly\bin`; the installer adds it to your user `PATH`.

### Pinning and support policy

| Topic | Detail |
|---|---|
| Latest release | Installers default to the latest [GitHub Release](https://github.com/VicenteOlmos/dolly/releases). |
| Pin a version | Set `DOLLY_VERSION` (for example `0.3.5`) in the install command above. |
| SemVer tags | Release tags follow `vX.Y.Z`. Only the **latest release** receives security fixes. |
| Immutable assets | Tags and release archives are never overwritten — use a new patch tag for fixes. |
| Checksums | Each release ships `checksums.txt`; installers verify archives before install. |

### From source

```bash
go install github.com/VicenteOlmos/dolly/cmd/dolly@latest
# or from a checkout:
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
```

<!-- readme:quick-start-clone -->
## Quick start: clone

1. **Install Dolly** using the [Install](#install) steps above.
2. In your project directory, create a `.env` file with a compatible connection. Use `DB_URL` or discrete `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, and `DB_PASSWORD` variables:

```bash
DB_URL='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
# or discrete vars:
# DB_HOST=localhost
# DB_PORT=5432
# DB_NAME=mydb
# DB_USER=user
# DB_PASSWORD=pass
```

3. Run:

```bash
dolly clone
```

Dolly discovers `.env` in the **current working directory** when resolving the source database. On Unix, if the file has broad permissions (group/other readable), Dolly emits one warning and continues **without changing** bytes, mode, owner, or timestamps on that file.

For scripted clone with config defaults:

```bash
dolly clone -ff
```

Optional: `dolly config init` writes `config.jsonc` for target URL, clone naming, strategies, and other defaults.

<!-- readme:security:dotenv-advisory -->
Owner-only permissions (for example `chmod 600 .env`) are recommended for secret files. Dolly does not require or modify permissions on external `.env` files it discovers.
<!-- /readme:security:dotenv-advisory -->

## More workflows

Create optional local configuration, then choose the interactive or scriptable route.

```bash
dolly config init
dolly tui
```

For dump and restore without cloning, pass a PostgreSQL DSN:

```bash
export DB='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
dolly dump --dsn "$DB" --output ./dolly_dump
dolly dump --dsn "$DB" --schemas app,public --output ./dolly_dump
dolly dump list --output ./dolly_dump
dolly restore --dsn "$DB" --input ./dolly_dump/1 --on-conflict skip
```

<!-- situation-guidance:start -->

Dolly does not inspect your database size or network conditions, and it does not auto-tune. Treat sizes and speeds as qualitative—hardware, schema shape, row width, and latency all affect outcomes.

**Not sure what to pick?** Run `dolly tui` for guided choices. On the CLI, omitting optimization flags keeps safe serial defaults (`workers=1`, transactional restore).

| Situation | Recommendation | Why | Tradeoff |
|---|---|---|---|
| <!-- situation:safe-default --> Unsure / want the safest path | `dolly dump --dsn "$DB" --output ./dolly_dump` → `dolly restore --dsn "$TARGET_DB" --input ./dolly_dump/1` | One worker by default; restore stays transactional and atomic | Slower than parallel modes on large databases |
| <!-- situation:small-database --> Small database, straightforward copy | `dolly dump --dsn "$DB" --output ./dolly_dump` | Full dump with minimal flags | Gets slower as data grows |
| <!-- situation:large-stable --> Very large named tables where resumability matters more than speed | `dolly dump ... --chunk-table public.large_table --workers 1` | PK or eligible unique-key plans use resumable keyset chunks | Tables without a safe key use a warned, non-resumable normal stream |
| <!-- situation:large-unreliable --> Large data, slow or unreliable link | `dolly dump ... --slow-connection --workers 1` | Safe per-table keys get checkpoints; no-safe-key tables still complete | Fallback tables are not resumable; mode is non-transactional and incompatible with subset and parallel dump |
| <!-- situation:maximum-dump-speed --> Large database, stable connection, maximum dump throughput | `dolly dump ... --workers "$WORKERS"` | Shared consistent snapshot across parallel table workers | Tune empirically within 1–16; needs `max_open_conns >= workers+1`; excludes slow/chunk/subset/`--no-transaction` |
| <!-- situation:maximum-restore-speed --> Maximum restore throughput | **ADVANCED — NON-ATOMIC** `dolly restore ... --workers "$WORKERS" --no-transaction --yes --ack-partial-state` | Parallel table restore after acknowledging partial-state risk | No atomic rollback; `on-conflict` must be `error`; cannot use `--replace`, `--trust-schema-sql`, skip, or upsert |
| <!-- situation:representative-sample --> Dev/test sample, not a full copy | `dolly dump ... --percent "$PERCENT" --max-rows-per-table "$ROW_CAP"` | Recent-root selection plus FK closure | Not statistically representative; closure may exceed the target percent |
| <!-- situation:same-instance-clone --> Fastest clone on the same instance | `dolly clone --strategy template` | Template database copy on one PostgreSQL server | Source must have no active connections; unsanitized |
| <!-- situation:cross-server-large-clone --> Large single-database cross-server copy | `dolly clone --strategy logical-stream` | Logical stream for large remote copies | Unsanitized; not a physical cluster copy |

`$WORKERS`, `$PERCENT`, and `$ROW_CAP` are operator-chosen values—Dolly does not set them automatically.

See `dolly dump --help`, `dolly restore --help`, and `dolly clone --help` for flags. Copyable recipes remain in [Common workflows and limits](#common-workflows-and-limits).

<!-- situation-guidance:end -->

### Choose a mode

| If you need… | Use | Do not use when |
|---|---|---|
| An exact table subset | `--include-table` / `--exclude-table` (or selector files) | You need FK-closure percent sampling (`--percent` / `--seed-file`) |
| Resumable export of large named tables | `--chunk-table` with `workers=1` | The table lacks a safe PK/unique key and you require resume, or you need parallel dump workers |
| A consistent parallel export | `--workers N` (shared snapshot) | `--no-transaction`, chunk/slow modes, or subset policies |
| Faster acknowledged restore | `--workers N` with `--no-transaction --yes --ack-partial-state` | You need atomic rollback, `--replace`, skip/upsert, or `--trust-schema-sql` |
| Atomic rollback on restore failure | Default serial restore (`workers=1`) | You need FK-level concurrency |

Copyable recipes for each mode are in [Common workflows and limits](#common-workflows-and-limits). Full flag catalogs: `dolly dump --help`, `dolly restore --help`, and [config.example.jsonc](config.example.jsonc).

## What Dolly can do

| Command | Purpose |
|---|---|
| `dolly tui` | Interactive cockpit for connecting, dumping, and cloning. |
| `dolly dump` | Export data to numbered NDJSON dump directories. Schema scope: `--schemas` (comma-separated) overrides saved connection profile schemas, then `dump.schemas` in config, then `public`. Refuses when the effective schema scope has no tables. |
| `dolly dump --percent N` | Subset dump: recent roots plus FK closure; output can exceed `N%`. Empty schema scope fails closed; nonempty scope with no eligible percent roots reports a candidate-root diagnostic. |
| `dolly dump list` | List local dump history without a database connection. |
| `dolly restore` | Load a Dolly dump into PostgreSQL. Refuses zero-table dumps before any database mutation. |
| `dolly clone` | Clone with `schema-replay`, `template`, `logical-stream`, or `physical-backup`. |
| `dolly config` | Create or inspect `config.jsonc` with `init` and `show`. |
| `dolly update` | Install the latest stable GitHub release (`--check` verifies without replacing; Windows defers replacement to a hidden helper). |
| `dolly version` | Print build version. |

Run `dolly <command> --help` for command-specific flags.

**TUI and CLI restore differ:** the TUI restores from Dolly dump history. To restore an arbitrary directory, use `dolly restore --input <dir>`.

When `pg_dump` is on `PATH`, Dolly captures `schema.sql` and sanitizes it for cross-version restore compatibility. Restore never executes that SQL unless you explicitly pass `--trust-schema-sql` for reviewed artifacts.

Trusted schema replay runs outside the restore transaction, so acknowledge both conditions explicitly:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --trust-schema-sql --no-transaction --yes
```

## Common workflows and limits

Six copyable recipes below. Each uses the same scan pattern: **Use when**, **Command**, **Result/artifacts**, and **Constraint/warning**. See also the [mode decision table](#choose-a-mode), `dolly dump --help`, `dolly restore --help`, and [config.example.jsonc](config.example.jsonc).

### Direct exact selectors

**Use when** you need a narrow dump of specific tables by exact `schema.table` name.

**Command**

```bash
dolly dump --dsn "$DB" --output ./dolly_dump \
  --include-table public.users --exclude-table public.audit_log
```

**Result/artifacts** numbered `{output}/{n}/` with NDJSON per table, `metadata.json` selection provenance (credential-free), and optional `schema.sql` when `pg_dump` is on `PATH`.

**Constraint/warning** includes narrow scope; excludes win over includes. Globs, CSV, and unqualified names are rejected. Unmatched includes fail before output; unmatched excludes become warnings in metadata. Subset modes (`--percent`, `--seed-file`) are incompatible on the same run. Shared-snapshot parallel dump (`--workers N`) can export included tables when chunk/slow/subset/`--no-transaction` are off.

### Selector files

**Use when** the same include/exclude list is reused across runs or is too long for repeated flags.

**Command**

```bash
dolly dump --dsn "$DB" --output ./dolly_dump \
  --include-table-file tables.include.txt \
  --exclude-table-file tables.exclude.txt
```

**Result/artifacts** same numbered dump layout and `metadata.json` provenance as direct selectors.

**Constraint/warning** files are newline-delimited; `#` comments and blank lines are ignored. Same exact `schema.table` grammar, include-narrow/exclude-win precedence, and validation rules as direct flags. Config equivalents: `dump.include_table_files` / `dump.exclude_table_files` (CLI flags replace config when set).

### Selective keyset chunk and resume

**Use when** named tables are large or the connection is unstable and you need checkpointed, resumable streaming.

**Command**

```bash
dolly dump --dsn "$DB" --output ./dolly_dump \
  --chunk-table public.orders --chunk-table public.events
```

**Result/artifacts** numbered `{output}/{n}/` with per-table NDJSON, `metadata.json` chunk provenance, transient checkpoint files under the run directory during export, and final metadata published only after completion.

**Constraint/warning** each requested table uses its primary key when present, otherwise an eligible simple or composite `UNIQUE NOT NULL` B-tree key. A table without a safe key completes through a qualified, non-resumable normal-stream fallback and creates no checkpoint. Unmatched chunk selectors fail before output. Resume requires the same source, selection, chunk policy, and strategy fingerprint; changed plans fail closed and preserve the interrupted candidate. Rejects `workers > 1` and subset modes (`--percent`, `--seed-file`). `--slow-connection` applies the same per-table planning to every selected table.

### Shared-snapshot parallel dump

**Use when** you need faster full or selected-table export with point-in-time consistency across tables.

**Command**

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --workers 4
```

**Result/artifacts** tables export concurrently from one read-only repeatable-read snapshot into numbered `{output}/{n}/`; `metadata.json` is written last on success only.

**Constraint/warning** `--workers` (or `dump.workers`, default `1`, max `16`) requires `db.max_open_conns >= workers+1`. Rejects `--no-transaction`, `--slow-connection`, chunk selectors, and subset modes. Unpublished run artifacts are cleaned on failure; coordinator monitors snapshot liveness during parallel export.

### Acknowledged parallel restore

**Use when** you accept non-atomic, FK-level concurrency for faster restore into a trusted or disposable target.

**Command**

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 \
  --workers 4 --no-transaction --yes --ack-partial-state
```

**Result/artifacts** tables restore by FK dependency level with bounded workers; sequences synchronize after all table data commits. On full success, `.dolly-restore-partial-state.json` is removed from the input directory.

**Constraint/warning** non-atomic: each table commits independently; no global rollback. `--workers` (or `restore.workers`, default `1`, max `16`) requires `--no-transaction`, `--yes`, `--ack-partial-state`, conflict policy `error` (default), and a resolvable DSN. Rejects `--replace`, `--trust-schema-sql`, and skip/upsert. Manifest defaults to `{input_dir}/.dolly-restore-partial-state.json` (override with `--partial-state-file` or `restore.partial_state_file`); retained on failure for inspection. Acknowledgement is CLI-only and never stored in config. Not available from TUI history without these CLI flags.

### Safe end-to-end combinations

**Use when** you need a documented path from selective dump through restore without mixing incompatible modes.

**Command** (exact selection + optional chunk, then default atomic restore)

```bash
dolly dump --dsn "$DB" --output ./dolly_dump \
  --include-table public.users --include-table public.orders \
  --chunk-table public.orders
dolly restore --dsn "$DB" --input ./dolly_dump/1
```

**Command** (snapshot-parallel dump, then explicit parallel restore)

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --workers 4
dolly restore --dsn "$DB" --input ./dolly_dump/1 \
  --workers 4 --no-transaction --yes --ack-partial-state
```

**Result/artifacts** dump writes `{output}/{n}/` with `metadata.json`, NDJSON, optional `schema.sql`, and checkpoint state only while chunk/slow export is in progress. Parallel restore may leave `.dolly-restore-partial-state.json` until full success.

**Constraint/warning** keep `workers=1` for chunk/selective dumps; use default serial restore when atomic rollback matters. When includes narrow scope, each `--chunk-table` must name a table in the include set. Do not combine parallel dump with chunk/slow/subset/`--no-transaction`. Do not combine parallel restore with replace, trusted schema, or skip/upsert policies.

### Smaller local dump

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --percent 10 --max-rows-per-table 1000
```

`--percent` conflicts with `--seed-file` and `--slow-connection`. FK closure can make a subset dump larger than the requested percentage.

### Faster bulk restore — advanced

Default restore runs in one transaction. For trusted empty targets or very large loads:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --no-transaction --yes
```

Serial `--no-transaction` mode can leave partial progress if it fails mid-way. Parallel restore (`--workers > 1`) always requires `--ack-partial-state` and writes a manifest until full success. Prefer the default when you need atomic rollback.

### Clone strategies

<!-- readme:fidelity:schema-replay -->
Default `schema-replay` clone recreates schema and object definitions (including **trigger** and **materialized-view** definitions), restores regular **table data** and **sequence** state, and excludes owners, **ACL**s, and **cluster-global** roles and tablespaces. **Materialized-view** contents are **not cloned** (definitions only). Cloned **triggers may fire** during restore.
<!-- /readme:fidelity:schema-replay -->

| Strategy | When | Sanitization |
|---|---|---|
| `schema-replay` | Default cross-server or development clone | Supported |
| `template` | Same PostgreSQL instance; fastest | No |
| `logical-stream` | Large cross-server logical copy | No |
| `physical-backup` | Whole cluster directory copy | No |

`physical-backup` uses `pg_basebackup`, requires replication privileges, and copies the entire cluster data directory rather than one database. Read [physical backup](docs/physical-backup.md) before using it.

Non-profile `clone -ff` keeps your dotenv or shell `DB_URL` as-is (query params such as `sslmode` stay intact; config may add only `statement_timeout`). When preflight cannot reach the source, the error includes a redacted connection detail before the generic network hint — passwords and full raw DSNs are not printed.

## Safety

Treat Dolly like a database administration tool:

- `restore --replace` truncates target tables before insert.
- `restore --no-transaction --yes` can leave partial table state.
- Sanitization is pattern-based and only applies to `dump` and `schema-replay`; it is not a compliance guarantee.
- `template`, `logical-stream`, and `physical-backup` copy unsanitized row data.

Before using production or production-like data, use a least-privilege role, keep DSNs and dumps out of Git, confirm destructive targets are disposable, validate sanitization manually, and rehearse in staging. See [security](docs/security.md) and [physical backup](docs/physical-backup.md).

## Configuration and automation

`dolly config init` writes `config.jsonc`. See [config.example.jsonc](config.example.jsonc) for the complete template.

Saved connections are off by default. Enable them explicitly:

```jsonc
{
  "save_connections": true,
  "connections": {
    "scope": "xdg",   // or "project"
    "encrypt": true    // set DOLLY_CONNECTIONS_KEY (32-byte standard base64)
  }
}
```

Then CLI commands can use `--connection <name>` instead of `--dsn`. Project-scoped stores are convenient but easier to commit by accident; encrypted stores need `DOLLY_CONNECTIONS_KEY`, and losing that key loses access to encrypted profiles.

`dump`, `restore`, `clone`, `version`, and `update` accept `--json`:

- Exit 0: success JSON on **stdout**.
- Exit 1: `{"ok":false,"command":"...","error":"..."}` on **stderr**.
- `clone --json` requires `-ff` for non-interactive use.
- `dump list --json` returns an array of history records instead of the command envelope.

```bash
result=$(dolly dump --dsn "$DB" --output ./out --json 2>err.json) || { cat err.json; exit 1; }
echo "$result"
```

Check for updates without replacing:

```bash
dolly update --check --json
```

## Development

Requires Go 1.26.3+ (match `go.mod`) and PostgreSQL 16 client tools on `PATH` for schema capture and clone strategies.

Run a local PostgreSQL round trip from a source checkout:

```bash
docker compose up -d
export DOLLY_TEST_PG_DSN='postgres://dolly:dolly@127.0.0.1:5433/dolly?sslmode=disable'
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
./bin/dolly dump --dsn "$DOLLY_TEST_PG_DSN" --output ./dolly_dump
./bin/dolly restore --dsn "$DOLLY_TEST_PG_DSN" --input ./dolly_dump/1 --on-conflict skip
```

```bash
go test ./...
go vet ./...
make preflight
make test-integration   # needs DOLLY_TEST_PG_DSN
```

Release notes: [docs/release.md](docs/release.md) · [CHANGELOG.md](CHANGELOG.md)

Report security issues privately via [GitHub private vulnerability reporting](https://github.com/VicenteOlmos/dolly/security/advisories/new) — see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
