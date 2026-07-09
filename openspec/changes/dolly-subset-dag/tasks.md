# Tasks: dolly-subset-dag

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 950–1,250 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 → PR2 → PR3 → PR4 (stacked to main) |
| Delivery strategy | auto-chain |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Predicate model + FK graph + unit tests | PR1 | `predicate.go`, `graph.go`, `predicate_test.go`; no dump wiring |
| 2 | BFS planner + closure limits + planner tests | PR2 | `subset.go`, `subset_test.go`; depends on PR1 |
| 3 | Filtered stream + dump branch + metadata manifest | PR3 | `stream.go`, `dump.go`, `metadata.go`, `order.go`; tests with sqlmock |
| 4 | CLI flags + integration + restore smoke | PR4 | `cmd/dolly/dump.go`, `dump_integration_test.go`; `//go:build integration` |

## Phase 1: Foundation (predicate + graph)

- [x] 1.1 Create `internal/dump/predicate.go`: `PredicateOp`, `RowPredicate`, `SubsetLimits`, `SubsetConfig`; validate `eq`/`in`/`is_null` against `[]db.Table` (unknown table/column/op → error before I/O)
- [x] 1.2 Create `internal/dump/predicate_test.go`: table-driven valid/invalid seeds; SQL-injection literals compile to `$N` only (spec: Seed values cannot inject SQL)
- [x] 1.3 Create `internal/dump/graph.go`: build `childToParents` / `parentToChildren` from `[]db.Table`; error on non-`public`/external FK targets
- [x] 1.4 Add `WithSubset(cfg SubsetConfig) Option` on `internal/dump` (store on dump options; nil = full dump default)

## Phase 2: Closure planner

- [x] 2.1 Create `internal/dump/subset.go`: `planSubset(ctx, q, tables, cfg) → planResult` with visited `(table, pk)` dedup; reject composite PK tables at plan time
- [x] 2.2 Implement bidirectional BFS: seed `SELECT` PKs; child hop via `fk = ANY($1)`; parent hop from FK cols (skip NULL); honor `ctx.Done()`
- [x] 2.3 Enforce `MaxDepth`, `MaxTables`, `MaxRows` (global v1), typed limit errors; fail on missing parent not in plan (spec: Closure integrity)
- [x] 2.4 Create `internal/dump/subset_test.go`: in-memory fixture DAG (departments→employees); parent/child seed directions; nullable FK skip; cap exceeded cases

## Phase 3: Streaming, orchestration, metadata

- [x] 3.1 Modify `internal/dump/stream.go`: `streamTableFiltered` with templates `= $1`, `= ANY($1)`, `IS NULL`; merge predicates per table with `OR`; chunk `IN` by `MaxInListSize`
- [x] 3.2 Modify `internal/dump/dump.go`: when subset set → `planSubset` → `SortTables` on included only → `streamTableFiltered` per table; full path unchanged
- [x] 3.3 Modify `internal/dump/metadata.go`: `SubsetManifest` + `Metadata.Subset`; seeds, limits, tables, `rows_exported`; omit on full dump
- [x] 3.4 Optional `internal/dump/order.go`: reuse `graph.go` helpers if it reduces duplication (no ordering behavior change for full dump)
- [x] 3.5 Unit-test filtered stream SQL/args via sqlmock in `subset_test.go` or dedicated stream tests

## Phase 4: CLI

- [x] 4.1 Parse `--seed-file` JSON (`seeds`, `limits`) in `cmd/dolly/dump.go`; map `--max-depth`, `--max-tables`, `--max-rows`, `--max-in-list-size` to `SubsetLimits` (document defaults in help)
- [x] 4.2 Wire subset mode when `--seed-file` set → `dump.WithSubset(cfg)`; without flag → full dump; help states full is default

## Phase 5: Integration verification

- [x] 5.1 Extend `internal/dump/dump_integration_test.go` (`//go:build integration`): seed one `employees` row → only closure `.ndjson` files; empty included table file present
- [x] 5.2 Integration: `MaxTables` exceeded → error, no promoted complete output; restore subset dump on empty DB via existing restore (no restore code changes)
- [x] 5.3 Run `go test ./...` and document `--no-transaction` note for large closures in CLI help or dump output hint

## Post-Verification Fixes (dolly-subset-dag)

- [x] Typed seed validation: `ValidateSeeds` now checks literal values against `db.Column.DataType` for common PostgreSQL types (integer, text/varchar, bool, uuid, numeric, timestamp). Added `TestValidateSeedsTyped`.
- [x] PK identity preservation: replaced stringification/re-parse with type-aware `pkKey` and `pkSet`; text PKs like `"001"` no longer collide with `int64(1)`. Added `TestPlanSubsetTextPKIdentity`.
- [x] Closure integrity refined: `verifyClosureIntegrity` now uses `requiredParents` tracked during BFS so nullable-FK skips do not falsely trigger integrity failures. Added `TestVerifyClosureIntegrityDirect` and `TestPlanSubsetNullableFKSkip`.
- [x] Scenario coverage added: parent-seed closure (`TestPlanSubsetParentSeed`), nullable FK skip, closure integrity direct test, text PK identity, CLI limit flags (`TestParseDumpFlags` subset case), empty filtered stream (`TestStreamTableFilteredEmptyResult`).
- [x] Coding-rule cleanup: removed inline comments from `internal/dump/subset.go`, `subset_test.go`, and `dump.go:15`.

## Post-Verify Warning Cleanup (dolly-subset-dag)

- [x] CLI seam test: extracted `buildDumpOptions` in `cmd/dolly/dump.go`; added `TestBuildDumpOptionsSeedFileReachesDump`, `TestBuildDumpOptionsLimitOverrides`, `TestBuildDumpOptionsWithoutSeedFileIsFullDump`, `TestBuildDumpOptionsInvalidSeedFilePropagatesError` to prove `--seed-file` and limit overrides reach `dump.WithSubset(cfg)` without opening PostgreSQL.
- [x] Live PostgreSQL integration status: `DOLLY_TEST_PG_DSN` is unset; created `INTEGRATION_DSN_NOTE.md` documenting the environment-gated skip, the exact command to run with a DSN, and the rationale that default tests must not require PostgreSQL.
- [x] Unexported doc comments removed: `compiledWhere` in `predicate.go`, `expandPKChunks` in `subset.go`, `fkEdge` in `graph.go`.
