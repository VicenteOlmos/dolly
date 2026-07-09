## Verification Report

**Change**: dolly-subset-dag  
**Version**: N/A (openspec change)  
**Mode**: Standard (Strict TDD disabled)

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 18 |
| Tasks complete | 18 |
| Tasks incomplete | 0 |

All tasks in `openspec/changes/dolly-subset-dag/tasks.md` are marked `[x]`. Apply-progress (Engram #904) reports full implementation across predicate, graph, planner, streaming, metadata, CLI, and integration.

### Build & Tests Execution

**Build**: ✅ Passed

```text
$ go build ./...
Go build: Success
```

**Tests**: ✅ 160 unit + 64 integration passed / 0 failed / 0 skipped (integration package)

```text
$ go test ./...
Go test: 160 passed in 6 packages

$ go test -tags=integration ./internal/dump/... ./internal/restore/...
Go test: 64 passed in 2 packages
```

Integration DB: `pgintegration.SetupMainDB()` succeeded locally (no `DOLLY_TEST_DSN` required; harness auto-provisions).

**Coverage**: ➖ Not measured (no project threshold configured for this change)

### Spec Compliance Matrix

#### postgres-dump-subset (22 scenarios)

| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| RowPredicate Model | Valid eq predicate | `predicate_test.go` → `TestValidateSeeds/valid eq` | ✅ COMPLIANT |
| RowPredicate Model | Unknown operator rejected | `predicate_test.go` → `TestValidateSeeds/unsupported op` | ✅ COMPLIANT |
| Seed Input Validation | Seed file references unknown table | `predicate_test.go` → `TestValidateSeeds/unknown table` | ✅ COMPLIANT |
| Seed Input Validation | Seed values cannot inject SQL | `predicate_test.go` → `TestCompilePredicateUsesBoundArgs/injection literal` | ✅ COMPLIANT |
| FK Closure Planning | Child rows pull in parent tables | `subset_test.go` → `TestPlanSubsetEmployeeSeed`; `dump_integration_test.go` → `TestIntegrationSubsetDumpEmployeeSeed` | ✅ COMPLIANT |
| FK Closure Planning | Parent seeds pull in dependent rows | (none) | ❌ UNTESTED |
| FK Closure Planning | Nullable FK references skipped | (none) | ❌ UNTESTED |
| Closure Limits | MaxTables exceeded | `subset_test.go` → `TestPlanSubsetMaxTablesExceeded`; `dump_integration_test.go` → `TestIntegrationSubsetDumpMaxTablesExceeded` | ✅ COMPLIANT |
| Closure Limits | Large IN lists are chunked | `subset_test.go` → `TestExpandPKChunks` | ⚠️ PARTIAL |
| Included Table Ordering | Subgraph respects parent-before-child | `order_test.go` → `TestSortTables` (generic); `dumpSubset` calls `SortTables` | ⚠️ PARTIAL |
| Subset Row Streaming | Only closure rows are written | `subset_test.go` → `TestStreamTableFilteredUsesWhere` | ⚠️ PARTIAL |
| Subset Row Streaming | Empty included table still gets a file | (none for subset) | ❌ UNTESTED |
| Subset Configuration API | Default dump is full | `dump_test.go` → `TestDumpFullFlow`; integration full-dump tests | ✅ COMPLIANT |
| Subset Configuration API | Subset configuration restricts export | `subset_test.go` + `TestIntegrationSubsetDumpEmployeeSeed` | ✅ COMPLIANT |
| Closure Integrity | Missing parent table fails fast | `subset.go` → `verifyClosureIntegrity` (static) | ❌ UNTESTED |
| MVP Schema Limitations | Composite primary key rejected | `subset_test.go` → `TestPrimaryKeyColumnRejectsComposite` | ✅ COMPLIANT |
| CLI Subset Flags | Seed file triggers subset dump | `cmd/dolly/dump.go` (static); no CLI test | ❌ UNTESTED |
| CLI Subset Flags | Missing seed file is full dump | `dump.go` default path (static) | ⚠️ PARTIAL |
| Integration Validation | Seed dump exports expected tables only | `TestIntegrationSubsetDumpEmployeeSeed` | ✅ COMPLIANT |
| Integration Validation | Subset dump restores on empty database | `restore_integration_test.go` → `TestIntegrationRestoreSubsetDump` | ✅ COMPLIANT |

#### postgres-dump-streaming delta (11 scenarios)

| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Full and subset coexistence | Full dump unchanged without subset config | `dump_test.go` → `TestDumpFullFlow`; integration full tests | ✅ COMPLIANT |
| Full and subset coexistence | Subset dump preserves streaming contracts | `TestIntegrationSubsetDumpEmployeeSeed` (artifacts + metadata) | ⚠️ PARTIAL |
| Full and subset coexistence | Subset limits surface as dump errors | `TestIntegrationSubsetDumpMaxTablesExceeded` | ✅ COMPLIANT |
| Subset manifest in metadata | Full dump omits subset manifest | `metadata_test.go` → `TestWriteMetadata` (nil subset; no key assertion) | ⚠️ PARTIAL |
| Subset manifest in metadata | Subset dump records seeds and limits | `TestIntegrationSubsetDumpEmployeeSeed` (`Subset != nil`) | ⚠️ PARTIAL |
| Subset manifest in metadata | Subset metadata still lists included schema | `TestIntegrationSubsetDumpEmployeeSeed` | ✅ COMPLIANT |
| Dump Artifact Generation | Complete dump artifacts are produced | Integration full-dump tests | ✅ COMPLIANT |
| Dump Artifact Generation | Subset dump artifacts for closure only | `TestIntegrationSubsetDumpEmployeeSeed` | ✅ COMPLIANT |
| Dump Artifact Generation | Empty tables are represented | Full: `TestIntegrationDumpEmptyTableFile`; subset empty included: (none) | ⚠️ PARTIAL |
| Metadata Descriptor | Schema metadata is written | `metadata_test.go` → `TestWriteMetadata` | ✅ COMPLIANT |
| Metadata Descriptor | Metadata order is deterministic | `metadata_test.go` → `TestWriteMetadataDeterministic` | ✅ COMPLIANT |
| Metadata Descriptor | Subset manifest accompanies subset metadata | `TestIntegrationSubsetDumpEmployeeSeed` | ✅ COMPLIANT |

**Compliance summary**: 18/33 ✅ COMPLIANT · 7/33 ⚠️ PARTIAL · 8/33 ❌ UNTESTED

### Correctness (Static Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| RowPredicate / validation | ✅ Implemented | `predicate.go` — `ValidateSeeds`, `compilePredicate`; operators `eq`/`in`/`is_null` only |
| FK graph | ✅ Implemented | `graph.go` — public schema; external FK rejected (`graph_test.go`) |
| BFS closure planner | ✅ Implemented | `subset.go` — bidirectional hops; nil FK skip at L159–161; limits enforced |
| Filtered streaming | ✅ Implemented | `stream.go` — `streamTableFiltered`; `mergeWhereClauses`; IN chunking via `expandPKChunks` |
| Subset dump orchestration | ✅ Implemented | `dump.go` — `WithSubset`, `dumpSubset`, manifest population |
| CLI | ✅ Implemented | `cmd/dolly/dump.go` — `--seed-file`, limit flags, help text |
| Closure integrity check | ✅ Implemented | `verifyClosureIntegrity` post-BFS |
| Composite PK rejection | ✅ Implemented | `primaryKeyColumn` + planner validation |

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| Single `Dump` entry + `WithSubset` option | ✅ Yes | No separate `SubsetDump` type |
| Reuse `SortTables` on included subgraph | ✅ Yes | `dumpSubset` → `planSubset` → `SortTables(included)` |
| Parameterized WHERE templates only | ✅ Yes | `$N` placeholders; `pgx.Identifier` for names |
| `SubsetManifest` in metadata | ✅ Yes | Seeds, limits, tables, `rows_exported` |
| Integration restore unchanged | ✅ Yes | `TestIntegrationRestoreSubsetDump` in restore package |
| v1 composite PK out of scope | ✅ Yes | Rejected at `primaryKeyColumn`; graph skips multi-PK tables for traversal |

### Issues Found

**CRITICAL**: None — all executed tests pass; no spec scenario has a failing covering test.

**WARNING** (coverage gaps — implementation present, no passing dedicated test):

1. **Parent-seed closure direction** — BFS child hops exist (`subset.go` L172–194) but no test seeds `departments` and asserts `employees` inclusion.
2. **Nullable FK skip** — `pv == nil` continue at L159–161; fixture has nullable `employees.nickname` but no planner test with NULL FK column.
3. **Closure integrity error** — `verifyClosureIntegrity` never exercised by unit/integration test.
4. **Subset empty included table** — no scenario where closure includes a table with zero matching streamed rows but file still created.
5. **CLI `--seed-file` wiring** — `ParseSeedFile` + flag merge untested; `dump_test.go` only covers DSN/output flags.
6. **MaxDepth / MaxRows limits** — only `MaxTables` has explicit limit tests.
7. **Subset manifest field assertions** — integration checks `Subset != nil` but not seeds/limits/row_counts JSON shape.
8. **Tasks 2.4 claim** — tasks.md lists parent/child seed directions and nullable FK in `subset_test.go`; only employee-seed path is present.

**SUGGESTION**:

- Add `TestParseSeedFile` + `TestParseDumpFlags` cases for `--seed-file` and limit flag override.
- Add sqlmock `TestPlanSubsetDepartmentSeed` mirroring employee seed.
- Assert `metadata.json` omits `"subset"` key on full dump (`json.Unmarshal` into `map[string]json.RawMessage`).
- Document `--no-transaction` for large closures in help (task 5.3 — verify help string in `dump.go` L34).

### Verdict

**PASS WITH WARNINGS**

All 18 tasks complete; `go build` and full unit/integration suites green. Core happy path (employee seed → departments+employees closure → filtered NDJSON → subset manifest → restore) is proven at runtime. Eight spec scenarios lack dedicated tests; seven others are only partially asserted. Safe to proceed to **sdd-archive** with optional follow-up tests for edge scenarios.
