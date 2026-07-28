# AGENTS.md

Dolly is a local-first PostgreSQL CLI/TUI (Go) for dumping, restoring, and cloning databases. See `README.md` and `CONTRIBUTING.md` for the full command reference and contributor workflow.

## Build and test

Standard build, lint, test, and run commands live in `README.md`, `CONTRIBUTING.md`, and the `Makefile`.

### PostgreSQL for integration tests

- Integration tests require PostgreSQL 16 and `DOLLY_TEST_PG_DSN`. Use `docker compose up -d` and the examples in `CONTRIBUTING.md` for local setup.
- Run integration tests with `make test-integration` (or `go test -tags=integration -p 1 -count=1 ./...`). They must run serially (`-p 1`) because they share one database.
- Clone integration tests require a role with `CREATEDB`. If preflight reports "lacks CREATEDB", grant it on your test role before running clone tests.

### Notes

- `dolly tui` requires a real TTY; it will not run in a plain piped shell. Use a PTY (e.g. tmux) or the opt-in smoke: `make test-tui-pty-smoke`.
- Match the Go toolchain to `go.mod`.
- PostgreSQL client tools (`psql`, `pg_dump`, `pg_restore`, `pg_basebackup`) must be on `PATH` for schema capture and clone strategies.
