# Apply Progress: Fix Public Release Blockers

**Mode**: Standard (Strict TDD disabled)

## Completed Tasks

- [x] 1.1 Preserve existing-target rejection and successful backup behavior.
- [x] 1.2 Partial backup/validation failure retention coverage.
- [x] 1.3 Logical schema precedence and cancellation-safe cleanup coverage.
- [x] 2.1 Remove physical target cleanup/identity logic and report retained targets.
- [x] 2.2 Keep bounded schema-replay cleanup and schema-option seam.
- [x] 3.1 Complete dump cross-schema and deterministic artifact tests.
- [x] 3.2 Complete logical filter forwarding and bounded cleanup deadline-expiration test.
- [x] 3.3 Keep PostgreSQL external-FK and successful replace-load test source.
- [x] 4.1 Run focused, repository, vet, build, and diff checks.

## Final Task Evidence

- [x] 4.2 `go clean -testcache` then `make test-integration` ran without `-short` against the authorized disposable local PostgreSQL database (exit 0).
- [x] 4.3 Refreshed integration evidence and task checkboxes after the passing local lifecycle.

## Final Verify-Blocker Remediation

- `TestSchemaReplayStrategyCleanupDeadlinePreservesPrimaryError` injects a 1 ms cleanup bound, blocks `dropDatabaseFunc` on `<-ctx.Done()`, observes `context.DeadlineExceeded`, preserves the replay error, and verifies the cleanup warning.
- `go test -count=1 -v ./internal/clone -run '^(TestSchemaReplayStrategyCleanupSurvivesCancellation|TestSchemaReplayStrategyCleanupDeadlinePreservesPrimaryError)$'` — exit 0, PASS (2 tests).
- `go test ./...` — exit 0, PASS (1210 tests, 15 packages); `go vet ./...` — exit 0; `go build -buildvcs=false ./...` — exit 0; `git diff --check` — exit 0.

## Work Unit Evidence

| Work unit | Focused test command and result | Runtime harness | Rollback boundary |
|---|---|---|---|
| Clone remediation | `go test -count=1 -v ./internal/clone -run '^(TestSchemaReplayStrategyCleanupSurvivesCancellation|TestSchemaReplayStrategyCleanupDeadlinePreservesPrimaryError)$'` — exit 0, PASS (2 tests) | N/A — deterministic unit seam blocks cleanup on `ctx.Done()` until injected 1 ms bound expires; PostgreSQL is not involved | `internal/clone/strategy_schema_replay.go`, `strategy_test.go` |
| Dump artifact evidence | `go test ./internal/dump/...` — exit 0, PASS (163 tests, 1 package) | N/A — filesystem/sqlmock boundary | `internal/db/models.go`, `internal/dump/{dump,metadata,stream}.go`, `dump_test.go` |
| Restore integration source | `go test ./internal/restore/...` — exit 0, PASS (60 tests, 1 package) | In-memory passwordless Unix-socket DSN: `go clean -testcache; make test-integration` without `-short` — exit 0; 4 packages passed; raw log SHA-256 `5f6ec9999a0818acd4c89044a761954df73bfc6fc130f3d229d8f3a3560a16cb` | `internal/restore/restore_integration_test.go` |
| Dump integration assertion correction | `go test ./internal/dump/...` — exit 0, PASS (163 tests, 1 package) | `go test -short -tags=integration ./internal/dump` — exit 0, PASS (163 tests, 1 package); no live PostgreSQL run, so task 4.2 remains pending | `internal/dump/dump_integration_test.go` metadata-declared path assertions only |
| Final local gate | `go test ./...` — exit 0, PASS (1210 tests, 15 packages); `go vet ./...` — exit 0; `go build -buildvcs=false ./...` — exit 0; `git diff --check` — exit 0 | Local PostgreSQL runtime harness passed; no independent conventional verify run (parent-owned) | Revert only release-blocker source and test files listed above |

## Delivery

- Delivery: single PR, maintainer-approved `size:exception`
- Review lineage: `review-8f06992f77494abf`
- Current tracked diff: 664 additions + 106 deletions = 770 changed lines, within approved 800-line `size:exception` budget.
- Integration runtime execution passed; parent-owned conventional verify remains next.

## Remediation Coverage

- Physical backup rejects an existing caller target before `pg_basebackup`; later failures retain the target and require explicit operator cleanup. No identity or recursive cleanup remains.
- Dump tests write two same-named cross-schema tables to distinct metadata-declared files and assert deterministic complete-dump paths.
- Clone tests table-drive dump selection and restore fallback; one deterministic cleanup test blocks on `ctx.Done()`, reaches `DeadlineExceeded`, preserves the primary error, and verifies the cleanup warning.
- Integration source contains external-FK rejection and successful replace row-loading tests. Runtime execution passed against the disposable local database.
- Four dump integration tests now resolve artifact paths from emitted table `data_file` metadata through `tableDataPath`; legacy-reader coverage is unchanged.

## Disposable Database Run Evidence

- Preflight via `/var/run/postgresql` as `vicho`: `rolsuper=false`, `rolcreaterole=false`, `rolreplication=false`, `rolbypassrls=false`, `rolcreatedb=true`; owned database count before mutation was `0`.
- A cleanup trap was installed before creation. It validates the literal database name, terminates only its connections, drops only `dolly_it_public_blockers`, and checks owned database count after cleanup.
- Creation proof: `current_database()=dolly_it_public_blockers`, `current_user=vicho`, database owner `vicho`.
- Command: `go clean -testcache`, then `make test-integration` without `-short`; the passwordless Unix-socket DSN existed only in the command environment and was never printed or written to a file.
- Result: exit `0`; package results: `internal/testutil/pgintegration`, `internal/db`, `internal/dump`, and `internal/restore` all passed. Raw stdout/stderr was written directly to `/tmp/opencode/dolly-integration-final-raw.log` with mode `0600`; SHA-256 `5f6ec9999a0818acd4c89044a761954df73bfc6fc130f3d229d8f3a3560a16cb`.
- Raw-log safety proof: neither literal `DOLLY_TEST_PG_DSN=` nor the PostgreSQL URI prefix is present.
- Cleanup proof: trap reported exact-database cleanup PASS; owned database count after cleanup was `0`. No role was altered or dropped.
