# Dolly

[Español](README.es.md)

Local-first PostgreSQL CLI and TUI for dumping, restoring, and cloning databases.

After install, the usual entry point is:

```bash
dolly tui
```

That opens the interactive cockpit (connect → schemas → dump/clone). No flags. Needs a real terminal (TTY). Config lives in `config.jsonc` in the current directory.

## Install

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | sh
```

Pin a version:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_VERSION=0.1.0 sh
```

Default install path: `/usr/local/bin` (override with `DOLLY_INSTALL_DIR`). Checksums are verified against the release `checksums.txt`.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Pin a version:

```powershell
$env:DOLLY_VERSION="0.1.0"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Default path: `%LOCALAPPDATA%\Programs\dolly\bin` (added to your user `PATH`).

### From source

```bash
go install github.com/VicenteOlmos/dolly/cmd/dolly@latest
# or from a checkout:
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
```

## First minutes

1. Install (above).
2. In a project directory, optionally create config:

   ```bash
   dolly config init
   ```

3. Start the TUI:

   ```bash
   dolly tui
   ```

4. Or use the CLI with a DSN:

   ```bash
   export DB='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
   dolly dump --dsn "$DB" --output ./dolly_dump
   dolly dump list --output ./dolly_dump
   dolly restore --dsn "$DB" --input ./dolly_dump/1 --on-conflict skip
   ```

## Commands

| Command | Purpose |
|---------|---------|
| `dolly tui` | Interactive cockpit (connect, dump, clone). |
| `dolly dump` | Export data to numbered NDJSON dump dirs. |
| `dolly dump --percent N` | Subset dump (recent roots + FK closure; size can exceed N%). |
| `dolly dump list` | List local dump history (no DB connection). |
| `dolly restore` | Load a dump into PostgreSQL. |
| `dolly clone` | Clone with a strategy (`schema-replay`, `template`, `logical-stream`, `physical-backup`). |
| `dolly config` | Create/inspect `config.jsonc` (`init`, `show`). |
| `dolly version` | Print build version. |

`dolly <command> --help` shows flags.

**TUI vs CLI restore:** the TUI restores from Dolly dump history. For an arbitrary directory, use `dolly restore --input <dir>`.

When `pg_dump` is on `PATH`, Dolly also captures `schema.sql` (sanitized for cross-version restore).

## Common recipes

### Smaller local dump

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --percent 10 --max-rows-per-table 1000
```

`--percent` conflicts with `--seed-file` and `--slow-connection`.

### Faster bulk restore (advanced)

Default restore is one transaction. For trusted empty targets / very large loads:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --no-transaction --yes
```

Partial progress is possible if it fails mid-way. Prefer the default when you need atomic rollback.

### Clone strategies

| Strategy | When | Sanitization |
|----------|------|----------------|
| `schema-replay` | Default cross-server / dev clone | Supported |
| `template` | Same Postgres instance, fastest | No |
| `logical-stream` | Large cross-server logical copy | No |
| `physical-backup` | Whole cluster directory copy | No |

## Configuration and saved connections

```bash
dolly config init   # writes config.jsonc
```

Saved connections are **off** by default. Enable them in `config.jsonc`:

```jsonc
{
  "save_connections": true,
  "connections": {
    "scope": "xdg",   // or "project"
    "encrypt": true   // set DOLLY_CONNECTIONS_KEY (32-byte standard base64)
  }
}
```

Then CLI commands can use `--connection <name>` instead of `--dsn`.

See `config.example.jsonc` for the full template.

## Agent / `--json` mode

`dump`, `restore`, `clone`, and `version` accept `--json`:

- Exit 0 → success JSON on **stdout**
- Exit 1 → `{"ok":false,"command":"...","error":"..."}` on **stderr**
- `clone --json` requires `-ff` (non-interactive)
- `dump list --json` uses a different shape (array of history records)

```bash
result=$(dolly dump --dsn "$DB" --output ./out --json 2>err.json) || { cat err.json; exit 1; }
echo "$result"
```

## Safety

Treat Dolly like a DB admin tool:

- `restore --replace` truncates target tables before insert.
- `restore --no-transaction --yes` can leave partial table state.
- Sanitization is pattern-based and only on dump / `schema-replay` — not a compliance guarantee.
- `template` and `physical-backup` copy unsanitized data.

More detail: [security](docs/security.md) · [physical backup](docs/physical-backup.md)

## Development

Dev Postgres + round trip from a source checkout:

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

## License

[MIT](LICENSE)
