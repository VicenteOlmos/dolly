# Warning-cleanup Verification Report: dolly-subset-dag

**Verified**: 2026-06-01  
**Mode**: Standard verification; Strict TDD disabled per Engram `sdd-init/dolly` (#794)  
**Artifact mode**: hybrid  
**Focus**: Post-verify warning cleanup for CLI subset wiring, DSN-gated integration documentation, unexported doc comments, and repo regressions.

## Verification Report

**Change**: dolly-subset-dag warning cleanup  
**Version**: N/A  
**Mode**: Standard

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 26 |
| Tasks complete | 26 |
| Tasks incomplete | 0 |

### Build & Tests Execution

**Build**: ✅ Passed

```text
/usr/bin/go build ./...
PASS (no output)
```

**Tests**: ✅ Passed / ⚠️ live integration skipped because `DOLLY_TEST_PG_DSN` is unset

```text
/usr/bin/go test ./... -count=1
ok  dolly/cmd/dolly
ok  dolly/internal/db
ok  dolly/internal/dump
ok  dolly/internal/restore
ok  dolly/internal/testutil/pgintegration
ok  dolly/internal/tui

/usr/bin/go test ./cmd/dolly/... ./internal/dump/... -count=1 -v
PASS; includes TestBuildDumpOptionsSeedFileReachesDump,
TestBuildDumpOptionsLimitOverrides,
TestBuildDumpOptionsWithoutSeedFileIsFullDump,
TestBuildDumpOptionsInvalidSeedFilePropagatesError,
TestValidateSeedsTyped, TestPlanSubsetParentSeed,
TestPlanSubsetNullableFKSkip, TestVerifyClosureIntegrityDirect,
TestStreamTableFilteredEmptyResult, TestPlanSubsetTextPKIdentity.

env -u DOLLY_TEST_PG_DSN /usr/bin/go test -p 1 -tags=integration ./... -count=1
PASS across all packages.

env -u DOLLY_TEST_PG_DSN /usr/bin/go test -p 1 -tags=integration ./internal/dump/... ./internal/restore/... -count=1 -v
PASS with integration tests explicitly skipped via: set DOLLY_TEST_PG_DSN to run integration tests.
```

**Vet**: ✅ Passed

```text
/usr/bin/go vet ./...
PASS (no output)
```

**Coverage**: ➖ Not collected; no coverage threshold defined for this verification.

### Spec Compliance Matrix

| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| CLI Subset Flags | Seed file triggers subset dump and limit flags apply to same config | `cmd/dolly/dump_test.go > TestBuildDumpOptionsSeedFileReachesDump`, `TestBuildDumpOptionsLimitOverrides` | ✅ COMPLIANT |
| CLI Subset Flags | Missing seed file is full dump | `cmd/dolly/dump_test.go > TestBuildDumpOptionsWithoutSeedFileIsFullDump` | ✅ COMPLIANT |
| Seed Input Validation | Invalid seed file fails before dump | `cmd/dolly/dump_test.go > TestBuildDumpOptionsInvalidSeedFilePropagatesError` | ✅ COMPLIANT |
| RowPredicate Model | Typed literal validation | `internal/dump/predicate_test.go > TestValidateSeedsTyped` | ✅ COMPLIANT |
| FK Closure Planning | Parent seed pulls dependent rows | `internal/dump/subset_test.go > TestPlanSubsetParentSeed` | ✅ COMPLIANT |
| FK Closure Planning | Nullable FK references skipped | `internal/dump/subset_test.go > TestPlanSubsetNullableFKSkip` | ✅ COMPLIANT |
| Closure Integrity | Missing parent table fails fast | `internal/dump/subset_test.go > TestVerifyClosureIntegrityDirect` | ✅ COMPLIANT |
| Subset Row Streaming | Empty included table still gets file | `internal/dump/subset_test.go > TestStreamTableFilteredEmptyResult` | ✅ COMPLIANT |
| Integration Validation on Fixture DAG | Live subset dump and restore fixture behavior | integration tests exist; unset-DSN run passed skip gate; live run not executed because `DOLLY_TEST_PG_DSN` is unset | ⚠️ PARTIAL |

**Compliance summary**: 8/9 directly compliant at runtime; 1/9 environment-gated and explicitly documented.

### Correctness (Static Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| CLI seed-file/limit wiring seam | ✅ Implemented | `cmd/dolly/dump.go` extracts `buildDumpOptions`; it parses `dump.ParseSeedFile`, applies defaults and flag overrides, then appends `dump.WithSubset(cfg)`. Tests inspect resulting options through `dump.InspectOptions`. |
| Full dump default | ✅ Implemented | `buildDumpOptions` returns no subset option when `SeedFile == ""`; test asserts `InspectOptions(opts...) == nil`. |
| Environment-gated integration note | ✅ Implemented | `INTEGRATION_DSN_NOTE.md` states DSN is unset, gives exact export + `go test -p 1 -tags=integration ./internal/dump/... -count=1 -v` command, and explains default tests must not require PostgreSQL. |
| Unexported doc comments in touched `internal/dump` files | ✅ Clean | `compiledWhere`, `expandPKChunks`, and `fkEdge` no longer have doc comments. Remaining comments in inspected touched files are on exported declarations or build tags. |
| Regression safety | ✅ Clean | Full tests, targeted tests, integration skip path, build, and vet all pass. |

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| Dump remains full-schema by default | ✅ Yes | No subset option is emitted without `--seed-file`. |
| Subset mode uses explicit `WithSubset(cfg)` | ✅ Yes | `buildDumpOptions` only appends `dump.WithSubset(cfg)` after successful seed-file parsing. |
| Default test suite does not require PostgreSQL | ✅ Yes | `go test ./... -count=1` passes without DSN; integration helpers skip when DSN is missing. |
| Warning cleanup avoids fake live integration | ✅ Yes | Live run was not claimed; artifact documents exact environment gate and command. |

### Issues Found

**CRITICAL**: None.

**WARNING**:

1. Live PostgreSQL integration did not execute because `DOLLY_TEST_PG_DSN` is unset. This is documented and consistent with the project rule that default tests must not require PostgreSQL.

**SUGGESTION**:

1. Before archive/release, run the documented live command once with a real DSN if live PostgreSQL assurance is required.

### Verdict

**PASS WITH WARNINGS**

The warning cleanup is verified: CLI wiring now has a real seam test, DSN-gated integration is documented honestly with an exact command, unexported doc-comment warnings are cleaned, and build/test/vet show no regressions. The only remaining warning is environmental: live PostgreSQL integration was not executed because no DSN is available.
