# AGENTS.md

Dolly is a local-first PostgreSQL CLI/TUI (Go) for dumping, restoring, and cloning databases. See `README.md` and `CONTRIBUTING.md` for the full command reference and contributor workflow.

## Cursor Cloud specific instructions

Standard build/lint/test/run commands live in `README.md`, `CONTRIBUTING.md`, and the `Makefile` — use those. The notes below are only the non-obvious, environment-specific caveats.

### PostgreSQL for integration tests / running the app

- The `README`/`docker-compose.yml` dev flow uses Docker to run `postgres:16-alpine` on host port `5433`. Docker is **not** installed in this VM. Instead, PostgreSQL 16 is installed natively (via `apt`) and its cluster is configured to listen on port **5433** to match `DOLLY_TEST_PG_DSN`. The `dolly` role (password `dolly`) and `dolly` database exist, with `internal/testutil/pgintegration/fixtures.sql` loaded.
- The native Postgres cluster does **not** auto-start on VM boot (no systemd). Start it before running integration tests or the app:
  ```bash
  sudo pg_ctlcluster 16 main start   # check with: sudo pg_lsclusters
  ```
- Non-obvious gotcha: clone integration tests (`internal/clone`) require the `dolly` role to have the `CREATEDB` attribute explicitly. A pure `SUPERUSER` role reports `rolcreatedb=false` in the catalog, which the clone preflight rejects. The role is set with `ALTER ROLE dolly WITH SUPERUSER CREATEDB CREATEROLE LOGIN;`. If clone tests fail with "lacks CREATEDB", re-apply that grant.
- The integration DSN used everywhere:
  ```bash
  export DOLLY_TEST_PG_DSN='postgres://dolly:dolly@127.0.0.1:5433/dolly?sslmode=disable'
  ```
- Run integration tests with `make test-integration` (or `go test -tags=integration -p 1 -count=1 ./...`). They must run serially (`-p 1`) because they share the one database.

### Notes

- `dolly tui` requires a real TTY; it will not run in a plain piped shell. Use a PTY (e.g. tmux) or the opt-in smoke: `make test-tui-pty-smoke`.
- `gofmt -l .` currently reports several pre-existing unformatted files in `internal/`; this reflects repo state, not your changes.
- `go` (1.26.x, matching `go.mod`) is preinstalled; `psql`/`pg_dump`/`pg_restore`/`pg_basebackup` (PostgreSQL 16 client tools) are installed and on `PATH`, which Dolly uses for schema capture and clone strategies.
