## Exploration: dolly-internal-db-postgres-schema

### Current State
The project has a basic structure with `internal/db` containing models and PostgreSQL schema extraction logic. However, it is incomplete:
- `go.mod` exists but lacks database driver dependencies.
- `internal/db/models.go` defines `Table`, `Column`, and `ForeignKey` structs.
- `internal/db/postgres.go` implements `LoadPostgresPublicSchema` using `information_schema`.
- No tests exist for the database logic.
- The main entry point `cmd/dolly/main.go` is missing.
- MySQL support is not yet implemented.

### Affected Areas
- `go.mod` — Needs `github.com/lib/pq` or `pgx`.
- `internal/db/` — Existing logic is a good start but needs verification and tests.
- `cmd/dolly/main.go` — Needs to be created to test the engine.

### Approaches
1. **Complete PostgreSQL Engine** — Add dependencies, write tests using a real or mocked DB, and ensure the extraction logic is robust.
   - Pros: Solidifies the core before moving to TUI.
   - Cons: Requires setup for testing (Docker or mocks).
   - Effort: Medium

2. **Add MySQL Support** — Implement similar logic for MySQL in `internal/db/mysql.go`.
   - Pros: Completes the DB engine scope.
   - Cons: Increases complexity before verifying PostgreSQL.
   - Effort: Medium

### Recommendation
I recommend **Approach 1**: Solidify the PostgreSQL engine first. We should add the necessary dependencies to `go.mod`, create a basic `main.go` to verify the extraction, and add unit tests.

### Risks
- **Dependency Management**: We need to choose between `lib/pq` and `pgx`. The user mentioned both.
- **Testing**: Testing database queries without a live database can be tricky (requires `sqlmock` or similar).

### Ready for Proposal
Yes.
