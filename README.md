# Dolly

Dolly is a local-first PostgreSQL CLI for dumping, restoring, and cloning database data. It can run as a command-line tool or as an interactive TUI, with optional saved connections for repeated work.

> Dolly is a local-first PostgreSQL CLI. The public module path is `github.com/VicenteOlmos/dolly`. Build locally or use the curl installer below.

## Quickstart

Start the development PostgreSQL database:

```bash
docker compose up -d
export DOLLY_TEST_PG_DSN='postgres://dolly:dolly@127.0.0.1:5433/dolly?sslmode=disable'
```

Build and try a dump/restore round trip:

```bash
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
./bin/dolly dump --dsn "$DOLLY_TEST_PG_DSN" --output ./dolly_dump
./bin/dolly dump list --output ./dolly_dump
./bin/dolly restore --dsn "$DOLLY_TEST_PG_DSN" --input ./dolly_dump/1 --on-conflict skip
```

Reset the dev database when you need clean fixture data:

```bash
docker compose down -v
docker compose up -d
```

## Install

### Linux / macOS

Install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | sh
```

Install a specific version by setting `DOLLY_VERSION` with or without a leading `v`:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_REPO=VicenteOlmos/dolly DOLLY_VERSION=0.1.0 sh
```

### Windows

Install the latest release from PowerShell:

```powershell
irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Install a specific version:

```powershell
$env:DOLLY_VERSION="0.1.0"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Installer expectations:

- Requires a public GitHub repo and release assets.
- Downloads `dolly_<os>_<arch>.tar.gz` from GitHub Releases (Linux/macOS) or `dolly_windows_<arch>.zip` (Windows).
- Supports Linux, macOS, and Windows on `x86_64` and `arm64`.
- Linux/macOS installs to `/usr/local/bin` by default; Windows installs to `%LOCALAPPDATA%\Programs\dolly\bin`.
- Override install directory with `DOLLY_INSTALL_DIR`.
- Verifies `checksums.txt` when the release publishes a matching entry.
- Checksum verification is required even for the `latest` release. Set `DOLLY_ALLOW_UNVERIFIED=1` to bypass it (use with care).
- Defaults to `VicenteOlmos/dolly`; override with `DOLLY_REPO` if needed.

## Commands

| Command | Use it for | Example |
|---------|------------|---------|
| `dolly tui` | Interactive terminal cockpit for connecting, dumping, and cloning. | `./bin/dolly tui` |
| `dolly dump` | Export PostgreSQL data to numbered NDJSON dump directories. | `./bin/dolly dump --dsn "$DOLLY_TEST_PG_DSN" --output ./dolly_dump` |
| `dolly dump --percent N` | Subset dump: recent root-table rows, then FK-closure related rows; output can exceed N%. | `./bin/dolly dump --dsn "$DOLLY_TEST_PG_DSN" --output ./dolly_dump --percent 10` |
| `dolly dump list` | List local dump history without connecting to a database. | `./bin/dolly dump list --output ./dolly_dump` |
| `dolly restore` | Load a dump into PostgreSQL. | `./bin/dolly restore --dsn "$DOLLY_TEST_PG_DSN" --input ./dolly_dump/1` |
| `dolly clone` | Clone from a configured or prompted source using a selected strategy. | `./bin/dolly clone --help` |
| `dolly config` | Create or inspect `config.jsonc`. | `./bin/dolly config init` / `./bin/dolly config show` |
| `dolly version` | Print build version metadata. | `./bin/dolly version` |

Run `dolly <command> --help` for command-specific flags.

`schema.sql` is captured when `pg_dump` is available and sanitized for cross-version restore compatibility before Dolly applies it.

## Common recipes

### Small dev dump

Use a recent, FK-closed subset when you need realistic local data without a full production-sized dump:

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --percent 10 --max-rows-per-table 1000
```

`--percent` samples recent root-table rows first, then adds required related rows through FK closure. It conflicts with `--seed-file` and `--slow-connection`.

### Advanced bulk restore

Default restore is atomic. For trusted clean targets or very large restores, opt in to per-table commits; when the DSN and conflict policy permit, restore uses PostgreSQL COPY for faster bulk loads:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --no-transaction --yes
```

If failure recovery matters, keep the default transaction mode. `--no-transaction` is explicit opt-in because it can leave partial per-table progress.

### TUI restore scope

The TUI restores dumps from Dolly's dump history. To restore an arbitrary directory, use CLI `restore --input <dir>`.

### Clone strategy quick guide

| Strategy | Use when | Notes |
|----------|----------|-------|
| `schema-replay` | Default cross-server/dev clone | Supports sanitization; uses dump/restore path. |
| `template` | Same Postgres instance, fastest copy | Unsanitized; requires source DB to allow template clone. |
| `logical-stream` | Large cross-server logical copy | Unsanitized; streams table data. |
| `physical-backup` | Cluster-level physical replica | Unsanitized; copies the whole cluster directory. |

## Configuration and saved connections

Dolly reads `config.jsonc` from the current project. Create the default file with:

```bash
./bin/dolly config init
```

Saved connections are disabled by default. To enable them, set:

```jsonc
{
  "save_connections": true,
  "connections": {
    "scope": "xdg",
    "encrypt": true
  }
}
```

Connection storage options:

- `scope: "project"` stores profiles in the project directory.
- `scope: "xdg"` stores profiles under `$XDG_CONFIG_HOME/dolly/`.
- `connections.path` overrides the store path.
- `connections.encrypt: true` encrypts the store with AES-256-GCM; set `DOLLY_CONNECTIONS_KEY` to a 32-byte standard base64 key.

With saved connections enabled, `dump`, `restore`, and fast-forward `clone` can use `--connection <name>` instead of `--dsn`.

## Agent / machine-readable mode

Commands `dump`, `restore`, `clone`, and `version` accept `--json` for machine-readable output.

**Contract**:
- Exit 0 → success JSON result on **stdout**.
- Exit 1 → error JSON object on **stderr**, exit code 1.
- `--help` short-circuits before `--json` — no JSON emitted.
- Progress rendering is suppressed when `--json` is active.

**Success shapes**:

| Command    | Fields |
|------------|--------|
| `dump`     | `ok`, `command`, `output_dir`, `seq`, `source_database`, `schemas`, `table_count` |
| `restore`  | `ok`, `command`, `input_dir`, `target_database`, `schemas`, `table_count` |
| `clone`    | `ok`, `command`, `source_database`, `clone_name`, `strategy`, `target_dir`, `schemas` (requires `-ff`) |
| `version`  | `ok`, `command`, `version`, `commit`, `date` |

`schemas` is always a JSON array (never `null`).

**Error shape** (stderr, exit 1):

```json
{"ok": false, "command": "<cmd>", "error": "<message>"}
```

**Notes**:
- `clone --json` requires `-ff` (non-interactive fast-forward mode).
- `dump list --json` predates this contract and uses a different shape (JSON array of history records).

**Example for agents**:

```bash
result=$(dolly dump --dsn "$DB" --output ./out --json 2>err.json)
if [ $? -ne 0 ]; then
    # parse err.json for {"ok":false,"command":"dump","error":"..."}
    cat err.json
    exit 1
fi
# success: $result is the JSON object
echo "$result" | jq .
```

## Safety notes

Dolly works with real database credentials and can perform destructive operations. Treat it like a database admin tool, not a toy.

- `restore --replace` truncates target tables before inserting data.
- `restore --no-transaction --yes` is advanced opt-in for per-table commits; default restore is atomic.
- Sanitization is pattern-based and only applies to dump/schema-replay paths; it is not a compliance guarantee.
- `template` and `physical-backup` clone strategies copy unsanitized data. `physical-backup` copies the full cluster directory.

Read the safety docs before production use:

- [Security and database safety](docs/security.md)
- [Physical backup clone strategies](docs/physical-backup.md)
- [Local release readiness](docs/release.md)

## Development

Useful local commands:

```bash
go test ./...
go vet ./...
go build -buildvcs=false ./cmd/dolly
make preflight
make build-versioned VERSION=0.0.0-local
```

Optional integration tests require the Docker dev database and `DOLLY_TEST_PG_DSN`:

```bash
make test-integration
```

Restore package coverage baseline:

```bash
make test-cover-restore
```

## License

Dolly is available under the [MIT License](LICENSE).
