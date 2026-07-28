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
| Work interactively | `dolly tui` — connect, inspect schemas, dump, and clone from a real terminal. |
| Script a workflow | `dolly dump`, `dolly restore`, and `dolly clone` — use a DSN or saved connection. |

`dolly tui` has no flags, requires a TTY, and reads `config.jsonc` from the current directory.

## Install

Installers download the matching GitHub Release asset and verify it against that release's `checksums.txt` before installing it.

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | sh
```

Pin a release:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_VERSION=0.1.1 sh
```

Default install path: `/usr/local/bin`. Set `DOLLY_INSTALL_DIR` to install elsewhere.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Pin a release:

```powershell
$env:DOLLY_VERSION="0.1.1"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Default install path: `%LOCALAPPDATA%\Programs\dolly\bin`; the installer adds it to your user `PATH`.

### Pinning and support policy

| Topic | Detail |
|---|---|
| Latest release | Installers default to the latest [GitHub Release](https://github.com/VicenteOlmos/dolly/releases). |
| Pin a version | Set `DOLLY_VERSION` (for example `0.1.1`) in the install command above. |
| SemVer tags | Release tags follow `vX.Y.Z`. Only the **latest release** receives security fixes. |
| Immutable assets | Tags and release archives are never overwritten — use a new patch tag for fixes. |
| Checksums | Each release ships `checksums.txt`; installers verify archives before install. |

### From source

```bash
go install github.com/VicenteOlmos/dolly/cmd/dolly@latest
# or from a checkout:
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
```

## First workflow

Create optional local configuration, then choose the interactive or scriptable route.

```bash
dolly config init
dolly tui
```

For a CLI workflow, pass a PostgreSQL DSN:

```bash
export DB='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
dolly dump --dsn "$DB" --output ./dolly_dump
dolly dump list --output ./dolly_dump
dolly restore --dsn "$DB" --input ./dolly_dump/1 --on-conflict skip
```

## What Dolly can do

| Command | Purpose |
|---|---|
| `dolly tui` | Interactive cockpit for connecting, dumping, and cloning. |
| `dolly dump` | Export data to numbered NDJSON dump directories. |
| `dolly dump --percent N` | Subset dump: recent roots plus FK closure; output can exceed `N%`. |
| `dolly dump list` | List local dump history without a database connection. |
| `dolly restore` | Load a Dolly dump into PostgreSQL. |
| `dolly clone` | Clone with `schema-replay`, `template`, `logical-stream`, or `physical-backup`. |
| `dolly config` | Create or inspect `config.jsonc` with `init` and `show`. |
| `dolly version` | Print build version. |

Run `dolly <command> --help` for command-specific flags.

**TUI and CLI restore differ:** the TUI restores from Dolly dump history. To restore an arbitrary directory, use `dolly restore --input <dir>`.

When `pg_dump` is on `PATH`, Dolly captures `schema.sql` and sanitizes it for cross-version restore compatibility. Restore never executes that SQL unless you explicitly pass `--trust-schema-sql` for reviewed artifacts.

Trusted schema replay runs outside the restore transaction, so acknowledge both conditions explicitly:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --trust-schema-sql --no-transaction --yes
```

## Common workflows and limits

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

This mode can leave partial progress if it fails mid-way. Prefer the default when you need atomic rollback.

### Clone strategies

| Strategy | When | Sanitization |
|---|---|---|
| `schema-replay` | Default cross-server or development clone | Supported |
| `template` | Same PostgreSQL instance; fastest | No |
| `logical-stream` | Large cross-server logical copy | No |
| `physical-backup` | Whole cluster directory copy | No |

`physical-backup` uses `pg_basebackup`, requires replication privileges, and copies the entire cluster data directory rather than one database. Read [physical backup](docs/physical-backup.md) before using it.

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

`dump`, `restore`, `clone`, and `version` accept `--json`:

- Exit 0: success JSON on **stdout**.
- Exit 1: `{"ok":false,"command":"...","error":"..."}` on **stderr**.
- `clone --json` requires `-ff` for non-interactive use.
- `dump list --json` returns an array of history records instead of the command envelope.

```bash
result=$(dolly dump --dsn "$DB" --output ./out --json 2>err.json) || { cat err.json; exit 1; }
echo "$result"
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
