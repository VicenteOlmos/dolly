## Exploration: fix-public-release-blockers

### Current State
Six independent safety defects share existing seams and can be corrected without changing public command names or dump/restore option signatures:

1. `ReplicationStrategy.Execute` unconditionally removes `Options.TargetDir` after `pg_basebackup` or post-backup marker failure. This treats caller-owned directories as disposable.
2. `restore.truncateTables` issues one `TRUNCATE TABLE ... CASCADE` per metadata table. PostgreSQL may truncate FK dependents not present in the dump; reverse ordering does not prevent that scope expansion.
3. `restore.Restore` validates schema, optionally runs `schema.sql` through `psql`, then starts its SQL transaction. `psql` DDL therefore survives later restore failure.
4. `SchemaReplayStrategy.Execute` calls cleanup with the operation context. Once `PipeWithEnv`/restore returns a cancellation error, `DROP DATABASE` receives the canceled context and may never run. Copy-stream has a similar best-effort cleanup path, but its existing cleanup already uses `context.Background()` for early failures; the frozen finding specifically targets schema-replay.
5. Dump streamers write `<table>.ndjson.tmp` and `<table>.ndjson`; restore derives the same path from `table.Name`. Same-named tables in different schemas overwrite/collide. Metadata currently has no file-name field, so changing naming requires an explicit compatibility decision.
6. Copy-stream enumerates every schema (`listSchemaNamesFunc` then `loadSchemasFunc`) and ignores `opts.DumpOpts` schema filters. Schema-replay already preserves configured filters or derives all schemas; `SchemasFromOptions` also defines dump-first/restore-fallback precedence.

Relevant consumers: clone strategy dispatch/preflight (`internal/clone/strategy.go`, `preflight.go`), clone options and schema precedence (`clone.go`, `schemas.go`, CLI/TUI option construction), restore metadata/path validation and table loading (`restore.go`, `metadata.go`), dump metadata/history/CLI readers (`dump/metadata.go`, `cmd/dolly/dump.go`, `dumphistory`), and existing unit/integration tests in `internal/{clone,dump,restore}`.

### Affected Areas
- `internal/clone/strategy_replication.go` — protect pre-existing `TargetDir` during all failure cleanup.
- `internal/clone/strategy_schema_replay.go` — use a cleanup context independent of canceled work context.
- `internal/clone/strategy_copy_stream.go`, `internal/clone/schemas.go` — apply configured schema filters before source table enumeration and retain dump-first precedence.
- `internal/restore/conflict.go`, `internal/restore/restore.go` — replace scope-expanding CASCADE behavior and move schema SQL into the restore transaction boundary or make its failure atomic.
- `internal/dump/stream.go`, `internal/restore/metadata.go`, `internal/dump/metadata.go` — make artifact identity schema-aware and ensure readers use metadata rather than reconstructing legacy paths.
- `internal/clone/*_test.go`, `internal/dump/*_test.go`, `internal/restore/*_test.go` — focused regression coverage; integration tests for PostgreSQL locking/FK and transactional DDL behavior.
- `openspec/specs/postgres-dump-streaming/spec.md`, `openspec/specs/postgres-restore-streaming/spec.md`, `openspec/specs/clone-production-scale/spec.md` — likely source specs to update in later spec phase.

### Approaches
1. **Minimal safety guards with stable formats** — snapshot whether `TargetDir` existed before backup; only remove directories created by this operation; use `context.WithoutCancel`/fresh bounded cleanup context; pass `SchemasFromOptions` into copy-stream enumeration; replace CASCADE with explicit dump-table truncation; begin SQL transaction before schema validation/application where possible.
   - Pros: smallest code diff, preserves existing CLI/options and old dump path behavior for non-colliding dumps.
   - Cons: schema SQL invoked by external `psql` cannot truly join `database/sql` transaction; same-name legacy dump format still needs a collision policy.
   - Effort: Medium.

2. **Transactional restore redesign plus metadata-declared artifact paths** — apply schema SQL through a PostgreSQL connection/transaction, and add a `file` (or equivalent) field to each metadata table with schema-qualified, escaped filenames. Restore accepts new metadata paths and falls back to `<name>.ndjson` for old dumps.
   - Pros: actually atomic schema/data restore; permanently removes schema-name collisions; explicit forward format.
   - Cons: larger API/format change, more migration tests, and external `schema.sql` execution may need replacement or a second connection.
   - Effort: High.

3. **Reject unsafe cases only** — refuse replace when dump has multiple schemas or dependencies, refuse schema SQL with transactions, reject pre-existing target directories, and reject copy-stream filters until a broader redesign.
   - Pros: very small implementation.
   - Cons: does not fix supported workflows; release blockers remain as user-visible failures.
   - Effort: Low, but unacceptable as complete scope.

### Recommendation
Use approach 1 for blockers 1, 2, 4, and 6. For blocker 5, use the smallest backward-compatible format evolution: generate schema-qualified deterministic filenames and record each file in metadata; restore reads the recorded field and falls back to legacy name-only paths when field is absent. Do not silently attempt to infer collisions from files. For blocker 3, preserve the existing `schema.sql`/`psql` seam but ensure schema application happens before any data-side transaction and is compensated on restore failure, or explicitly narrow the guarantee if PostgreSQL transactional execution cannot be achieved through the current command runner. Preferred design phase should verify whether `psql` can execute against the same transaction; if not, replace only this path with SQL execution via `database/sql` rather than claiming atomicity.

Tests should be behavior-focused and table-driven: pre-existing versus newly created target directory; cleanup under canceled context; explicit truncation never naming non-metadata dependents; schema SQL ordering and failure state; two schemas sharing table name; legacy metadata fallback; copy-stream configured filters, restore fallback, and no-filter behavior. Use `t.TempDir()` for filesystem tests, go-sqlmock for SQL ordering/statements, and opt-in PostgreSQL integration tests (`DOLLY_TEST_PG_DSN`) for FK scope and transactional DDL. No migration of existing dump files is required if fallback remains; new metadata is backward-compatible for readers and old metadata remains readable.

Likely authored change size: 250–500 lines including focused tests and metadata compatibility, below the requested 800-line cap. A single PR is reviewable under the supplied `single-pr-default` strategy, but blocker 5 plus a true transactional schema redesign could exceed the practical review budget; split only if design confirms that larger redesign.

### Risks
- External `psql` cannot participate in an already-open `database/sql` transaction; a superficial reorder would not satisfy atomicity.
- Changing artifact filenames affects CLI history, restore, sanitize, and any users inspecting dump directories directly.
- Explicit truncation may fail on FK order or leave dependent rows when dependencies are outside scope; integration coverage must prove intended semantics.
- Cleanup must be bounded and independent from cancellation without hanging indefinitely; use a short timeout.
- Existing clone callers may supply `DumpOpts` and `RestoreOpts` with conflicting schema filters; preserve current dump-first precedence from `SchemasFromOptions`.

### Ready for Proposal
Yes. Proposal should freeze backward-compatibility rules for metadata filenames and define the exact atomicity guarantee for `schema.sql` before implementation. Keep all six in one PR only if schema SQL can use a small, testable transaction-safe path; otherwise split blocker 3 into a dedicated follow-up while shipping the other five.

## Verification Remediation Addendum

### Current blockers

The latest conventional report is FAIL: physical-backup cleanup remains a `Stat`/`SameFile`/`RemoveAll(path)` TOCTOU; four scenarios are partial and five lack runtime evidence; PostgreSQL replace integration has only compiled because `DOLLY_TEST_PG_DSN` is unset.

### Physical-backup decision

| Option | Assessment | Cross-platform result |
|---|---|---|
| No automatic recursive cleanup; retain partial target | **Recommended minimum**. Removes deletion race and cannot delete caller-owned/replacement paths. | Portable Go; leaves operator cleanup and partial data on failure.
| Unique temporary sibling then publish | Safer publication, but still needs cleanup of failed temp trees and adds rename/publish semantics. | Same-filesystem rename is broadly portable; Windows open-handle/rename behavior differs; more code than needed.
| OS-specific handle-based deletion | Strongest race resistance, but platform-specific syscall complexity and difficult portability/testing. | Unix and Windows implementations diverge; overengineered for release blocker.
| Narrow specification | Accept only if product explicitly permits partial targets. | Portable, but does not promise cleanup.

Amend `proposal.md` scope item 1, success criteria, and risks; `specs/clone-production-scale/spec.md` requirement/scenario to say failed targets are retained and caller-owned/replacement paths are never removed; `design.md` decision/data-flow/file-change/testing rows to remove ownership cleanup and test retention; `tasks.md` task 1.1/2.1/3.2 to remove cleanup-removal claims and add partial-target retention. Do not change unrelated specs.

### Smallest evidence set

Add one focused test per missing behavior, reusing existing seams: (1) dump writes two same-named cross-schema tables and asserts both metadata/files; (2) repeated complete dump asserts identical `data_file`; (3) logical-stream selected filter is forwarded; (4) restore-options fallback is forwarded when dump filters are empty; (5) schema-replay canceled context still invokes cleanup with a live bounded context; (6) cleanup deadline is observed; (7) cleanup failure preserves primary error and emits warning. Existing unit replace tests cover selected row loading only weakly; one integration test should cover external-FK rejection and one successful replace/load, so no extra sqlmock duplicate. If physical cleanup is narrowed, replace its removal test with partial-target retention.

### PostgreSQL path

Read-only discovery: `docker` and `podman` are installed, Docker daemon responds, no relevant PostgreSQL container is running, local `postgresql` service is active, `pg_isready` reports `/run/postgresql:5432` accepting connections, and `DOLLY_TEST_PG_DSN` is unset. Do not use the caller's local database implicitly. Conventional verify should require a user-provided disposable `DOLLY_TEST_PG_DSN`; likely local-socket DSN can be supplied if permissions/database are suitable. Then run `make test-integration` (or its exact tagged command) without `-short`; configured-but-unreachable DSN must fail.

### Recommendation

Amend proposal/spec/design/tasks before another apply. Apply the no-cleanup narrowing first, add only the seven focused evidence cases plus two live PostgreSQL scenarios, then rerun conventional verify. No archive or review authority work is needed.
