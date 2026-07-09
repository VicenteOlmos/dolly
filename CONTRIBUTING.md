# Contributing to Dolly

Dolly is a PostgreSQL CLI/TUI for dump, restore, and clone operations.

## Getting started

```bash
# Start local PostgreSQL (required for integration tests)
docker compose up -d

# Set the test DSN
export DOLLY_TEST_PG_DSN='postgres://dolly:dolly@127.0.0.1:5433/dolly?sslmode=disable'

# Build
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
```

## Testing

- **Unit tests**: `go test -short ./...` — no Docker required.
- **Integration tests**: `make test-integration` — needs Docker and `DOLLY_TEST_PG_DSN`.
- **TUI PTY smoke**: `make test-tui-pty-smoke` — opt-in Unix-only smoke that launches the real TUI in a terminal and quits it.
- **Restore coverage**: `make test-cover-restore`.

## Quality

```bash
go vet ./...
gofmt -l .
make preflight   # runs check-install-script + test + vet + build
```

## The `--json` agent contract

When `--json` is passed, commands write a JSON envelope to stdout on success
(`{"ok":true,...}`) and `{"ok":false,...}` to stderr on failure. See the
"Agent / machine-readable mode" section in README.md for the contract.
If you change the JSON output shape, update tests in `cmd/dolly/*_test.go`
that assert the contract.

## Code style

- **Ponytail**: the shortest working diff wins. No unrequested abstractions.
- Use `ponytail:` comments for deliberate shortcuts with a known ceiling.
- Match existing patterns in the codebase.

## Pull requests

Run `make preflight` before submitting.
