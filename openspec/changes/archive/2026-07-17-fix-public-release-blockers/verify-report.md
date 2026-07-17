```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:4ef9714ab4ca6e7a0273afc33185a711b84de7072e07aa913ad10718ab3c0931
verdict: pass
blockers: 0
critical_findings: 0
requirements: 8/8
scenarios: 25/25
test_command: go test ./...
test_exit_code: 0
test_output_hash: sha256:cba62356d2bcbb3830672ea94a806564f529f8895e17a7267481fa03abf03f06
build_command: go build -buildvcs=false ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: `fix-public-release-blockers`  
**Version**: N/A  
**Mode**: Conventional SDD / Standard verification  
**Verdict**: **PASS**  
**Archive readiness**: **READY**

### Executive Summary

All 8 current requirements, 25 scenarios, and 11 checked tasks are independently complete. Uncached focused tests, the full Go suite, vet, build, diff validation, and an isolated real PostgreSQL integration lifecycle all passed. No blocker or critical finding remains.

### Completeness

| Metric | Verified |
|---|---:|
| Requirements | 8/8 complete |
| Scenarios | 25/25 compliant |
| Architecture decisions | 6/6 followed |
| Tasks | 11/11 complete; 0 pending |

Strict TDD is disabled. Verification read current proposal, four specifications, design, tasks, apply-progress, prior verify report, full Engram artifacts, changed source/tests, and actual tracked diff. CodeGraph preceded source inspection.

### OpenSpec and Engram Parity

| Artifact | OpenSpec | Engram | Result |
|---|---|---|---|
| Proposal | `proposal.md` | `#3213` | Semantic parity |
| Specifications | Four current spec files; 8 requirements and 25 scenarios | `#3214` combined spec | Semantic parity |
| Design | `design.md` | `#3216` | Same six active decisions |
| Tasks | `tasks.md`; 11/11 checked | `#3224`; 11/11 complete | Same completion and acceptance state; OpenSpec carries expanded work-unit detail |
| Apply progress | `apply-progress.md` | `#3233` | Same completed work and passing cleanup/integration evidence |
| Verify | This report | `sdd/fix-public-release-blockers/verify` | Persisted from this exact report and read back after write |

Active OpenSpec and Engram contracts both require retaining Dolly-created failed physical-backup targets and prohibit recursive target deletion. The parenthetical “Previously” sentence in the delta spec is historical context, not active policy. Engram decisions `#3289` and `#3298` confirm retention. Historical observation `#3360` incorrectly described `#3214` as stale; direct full retrieval of current `#3214` disproves that note. Production clone source contains no target `RemoveAll`, identity check, or recursive-deletion path.

### Command Evidence

All logs contain exact stdout/stderr bytes. Focused and integration logs are mode `0600`.

| Command | Exit | Result | Output SHA-256 |
|---|---:|---|---|
| `go test -count=1 -v ./internal/clone -run '^(TestReplicationStrategyExecute|TestReplicationStrategyRetainsPartialTargetOnFailure|TestReplicationStrategyRetainsPartialTargetOnValidationFailure|TestReplicationStrategyRejectsCallerOwnedDirectoryBeforeRun|TestCopyStreamStrategyExecute|TestCopyStreamStrategyForwardsSchemaOptions|TestSchemasFromOptionsPrefersDumpOpts|TestSchemaReplayStrategyCleanupSurvivesCancellation|TestSchemaReplayStrategyCleanupDeadlinePreservesPrimaryError)$'` | 0 | PASS — 9 top-level tests and 2 filter subtests | `850bbf60e20c080fdeced1cc3cc47f232fd6526dd871480a18a809eeb25c7506` |
| `go test -count=1 -v ./internal/dump -run '^(TestDumpWithSchemasMulti|TestDumpEmptyTable|TestAssignDataFilesIsCollisionFreeAndDeterministic|TestDumpCompleteDataFilesAreDeterministic)$'` | 0 | PASS — 4 tests | `34809b4e22a09be3f4ce989c37fee4844d66b346edc631dc1c44c84523e87a31` |
| `go test -count=1 -v ./internal/restore -run '^(TestVerifyNDJSONFiles|TestVerifyNDJSONFilesRejectsUnsafeDeclaredPaths|TestRestoreRejectsMissingOrUnsafeArtifactBeforeSQL|TestRestoreFullFlow|TestRestoreRejectsReplaceWithoutTransactionBeforeMutation|TestRestoreMissingSchemaDefaultRejectsBeforeSchemaApply|TestRestoreMissingSchemaWithoutTransactionAppliesSchema|TestRestoreDuplicateKeyRollsBack|TestRestoreReplaceTruncatesChildrenBeforeParents|TestRestoreSequencesFromMetadata|TestSyncSequencesToData.*)$'` | 0 | PASS — 13 top-level tests and 12 path subtests | `661200e168dfdd1fde65e1ccf5c1629d3e334740eb7fcd3944ae5904c91a81fb` |
| `go test ./...` | 0 | PASS — 15 packages | `cba62356d2bcbb3830672ea94a806564f529f8895e17a7267481fa03abf03f06` |
| `go vet ./...` | 0 | PASS — empty output | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `go build -buildvcs=false ./...` | 0 | PASS — empty output | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `git diff --check` | 0 | PASS — empty output | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `go clean -testcache` | 0 | PASS — empty output | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `make test-integration` | 0 | PASS — 4 packages, no `-short` | `c63585042dfc23501464206131ea63cba0103b0f9c7069e7a06a4e4a342b2b4c` |

Coverage command/threshold: not declared for this conventional change.

### Independent PostgreSQL Lifecycle Proof

| Gate | Evidence | Result |
|---|---|---|
| Socket identity | `/var/run/postgresql`; `current_user=vicho` | PASS |
| Restricted role flags before | `vicho|false|false|true|false|false` for superuser, createrole, createdb, replication, bypassrls | PASS — only CREATEDB enabled |
| Isolation before | Databases owned by `vicho`: `0`; exact target count: `0` | PASS |
| Cleanup protection | Literal-name cleanup trap installed before creation | PASS |
| Exact creation | `dolly_verify_final_public_blockers|vicho|vicho`; owned database count during run: `1` | PASS |
| DSN handling | Passwordless local-socket DSN existed only in process memory; never printed or written | PASS |
| Uncached execution | `go clean -testcache` exit `0`, then `make test-integration` without `-short` | PASS |
| Raw evidence | `/tmp/opencode/dolly-final-integration-raw.log`; direct stdout/stderr redirection; mode `0600`; 370 bytes | PASS |
| Package results | `internal/testutil/pgintegration`, `internal/db`, `internal/dump`, `internal/restore` | PASS |
| Exact cleanup | Connections terminated only for exact target; dropped only exact target | PASS |
| Isolation after | Exact target count: `0`; databases owned by `vicho`: `0` | PASS |
| Role unchanged | `vicho|false|false|true|false|false` after cleanup | PASS |

Integration raw-log SHA-256: `c63585042dfc23501464206131ea63cba0103b0f9c7069e7a06a4e4a342b2b4c`.

### Requirement and Scenario Compliance Matrix

| # | Requirement | Scenario | Passing runtime evidence | Result |
|---:|---|---|---|---|
| 1 | pg_basebackup execution | Existing target is rejected before backup | `TestReplicationStrategyRejectsCallerOwnedDirectoryBeforeRun` | ✅ COMPLIANT |
| 2 | pg_basebackup execution | Dolly-created target is retained after backup failure | `TestReplicationStrategyRetainsPartialTargetOnFailure` | ✅ COMPLIANT |
| 3 | pg_basebackup execution | Dolly-created target is retained after validation failure | `TestReplicationStrategyRetainsPartialTargetOnValidationFailure` | ✅ COMPLIANT |
| 4 | pg_basebackup execution | Successful base backup | `TestReplicationStrategyExecute` | ✅ COMPLIANT |
| 5 | Filtered Logical-Stream Enumeration | Schema filter limits stream | `TestCopyStreamStrategyForwardsSchemaOptions/selected_dump_schemas` | ✅ COMPLIANT |
| 6 | Filtered Logical-Stream Enumeration | Precedence remains stable | `TestSchemasFromOptionsPrefersDumpOpts`; `TestCopyStreamStrategyForwardsSchemaOptions/restore_fallback` | ✅ COMPLIANT |
| 7 | Filtered Logical-Stream Enumeration | No filter enumerates all | `TestCopyStreamStrategyExecute` | ✅ COMPLIANT |
| 8 | Cancellation-Safe Schema-Replay Cleanup | Cleanup runs after cancellation | `TestSchemaReplayStrategyCleanupSurvivesCancellation` | ✅ COMPLIANT |
| 9 | Cancellation-Safe Schema-Replay Cleanup | Cleanup remains bounded | `TestSchemaReplayStrategyCleanupDeadlinePreservesPrimaryError` blocks on `ctx.Done()` until injected 1 ms deadline and observes `context.DeadlineExceeded` | ✅ COMPLIANT |
| 10 | Cancellation-Safe Schema-Replay Cleanup | Primary error remains authoritative | Both cleanup tests preserve replay error; deadline test verifies warning | ✅ COMPLIANT |
| 11 | Dump Artifact Generation | Same table names do not collide | `TestDumpWithSchemasMulti`; `TestAssignDataFilesIsCollisionFreeAndDeterministic` | ✅ COMPLIANT |
| 12 | Dump Artifact Generation | Empty tables are represented | `TestDumpEmptyTable` | ✅ COMPLIANT |
| 13 | Dump Artifact Generation | Legacy-independent output is deterministic | `TestDumpCompleteDataFilesAreDeterministic` | ✅ COMPLIANT |
| 14 | Restore Artifact Input | Safe metadata path restores | `TestRestoreFullFlow`; real PostgreSQL restore package | ✅ COMPLIANT |
| 15 | Restore Artifact Input | Unsafe artifact references fail before mutation | `TestVerifyNDJSONFilesRejectsUnsafeDeclaredPaths`; `TestRestoreRejectsMissingOrUnsafeArtifactBeforeSQL` | ✅ COMPLIANT |
| 16 | Restore Artifact Input | Legacy metadata remains readable | `TestVerifyNDJSONFiles` and full restore suite | ✅ COMPLIANT |
| 17 | Restore Artifact Input | Missing artifact fails early | `TestRestoreRejectsMissingOrUnsafeArtifactBeforeSQL/missing.ndjson` | ✅ COMPLIANT |
| 18 | Restore Artifact Input | Validation precedes all data mutation | Zero-SQL expectations in restore artifact failure tests | ✅ COMPLIANT |
| 19 | Conflict Policies | Replace scopes mutation | `TestRestoreReplaceTruncatesChildrenBeforeParents`; real PostgreSQL external-FK case | ✅ COMPLIANT |
| 20 | Conflict Policies | External dependency rejects replace | `TestIntegrationRestoreReplaceRejectsExternalForeignKey` in passing real PostgreSQL package | ✅ COMPLIANT |
| 21 | Conflict Policies | Replace loads selected rows | `TestIntegrationRestoreReplaceReloadsData` in passing real PostgreSQL package | ✅ COMPLIANT |
| 22 | Transactional Atomicity | Default missing schema fails closed | `TestRestoreMissingSchemaDefaultRejectsBeforeSchemaApply` | ✅ COMPLIANT |
| 23 | Transactional Atomicity | Explicit non-transactional schema application | `TestRestoreMissingSchemaWithoutTransactionAppliesSchema` | ✅ COMPLIANT |
| 24 | Transactional Atomicity | Successful transactional restore commits once | `TestRestoreFullFlow`; `TestIntegrationRestoreReplaceRollsBackAfterLaterLoadFailure` | ✅ COMPLIANT |
| 25 | Replace Transaction Boundary | Replace without transaction fails closed | `TestRestoreRejectsReplaceWithoutTransactionBeforeMutation` | ✅ COMPLIANT |

**Compliance summary**: 25/25 scenarios compliant.

### Correctness and Design Coherence

| Requirement / decision | Static evidence | Status |
|---|---|---|
| Retain failed physical target | Atomic `os.Mkdir`; no production recursive target deletion; errors preserve cause, path, and cleanup duty | ✅ COMPLETE |
| Logical filter precedence | Dump schema filter wins, restore filter falls back, unfiltered path lists all schemas | ✅ COMPLETE |
| Bounded cleanup | Independent `context.WithTimeout(context.Background(), 10*time.Second)` plus runtime expiration proof | ✅ COMPLETE |
| Metadata-declared data files | Lowercase hex UTF-8 schema/table path assigned for complete and subset dumps | ✅ COMPLETE |
| Restore input safety | All declared/legacy paths validated before schema load, truncate, or insert | ✅ COMPLETE |
| Scoped replace | One transaction-bound metadata-table-only `TRUNCATE`, no `CASCADE`; PostgreSQL rejects external FKs atomically | ✅ COMPLETE |
| Transaction boundary | Default schema replay fails closed; explicit non-transactional mode may apply schema; replace+no-transaction rejected | ✅ COMPLETE |

### Task Verification

All 11 task checkboxes are supported by current source and fresh runtime evidence. Task 3.2 is now complete: the new deadline test exercises actual timeout expiration, observes cancellation, preserves the primary error, and verifies warning output. Tasks 4.2 and 4.3 are supported by the independent exact-database lifecycle above.

### Scope Integrity

Tracked implementation/test diff is confined to 16 intended files under `internal/clone`, `internal/db`, `internal/dump`, and `internal/restore`: 664 additions plus 106 deletions, 770 changed lines. This remains within the approved 800-line size exception. Exact tracked diff SHA-256: `c4a109225fb39ba730472183a426e2628959e6da2f6f46ac045fdc0d81b73f72`. Unrelated untracked OpenSpec/browser/session paths were observed and not modified.

### Limitations

- Integration output is intentionally non-verbose; inspected named integration test source maps to the passing uncached `internal/restore` integration package.
- No coverage threshold is declared.
- One discarded read-only PostgreSQL preflight attempt exited `80` because verifier SQL quoting was malformed. It failed before trap installation, database creation, DSN construction, or mutation. The corrected full lifecycle above is the authoritative run.
- Historical Engram discoveries remain as audit history; active proposal/spec/design/tasks/apply/verify artifacts and current source carry no superseded deletion policy.

### Issues

**CRITICAL**: None.  
**WARNING**: None.  
**SUGGESTION**: None.

### Canonical Evidence Revision Preimage

SHA-256 of the exact newline-terminated block below is the envelope `evidence_revision`:

```text
format=dolly-conventional-sdd-evidence-v2
proposal_sha256=4beb067d4ab359b8c17d410dbc3482ab37d85b752a8dbc549e76498c12ac117c
clone_spec_sha256=188f2241b70ac9a4851ebb207aca661e2a9364d6e5bd6896bb972ff6e6c67d42
logical_spec_sha256=3d84f871f760b39301c99ab561df0f55289eba6c132e4cc01ba9a731345270d6
dump_spec_sha256=cfdd683646c4ced6dbca201d18c1f9e9a5b18794a71af51a99ab0067c25f37a5
restore_spec_sha256=7f857fb4c5d05650a6dee83b0348c760c9933b83c3558b66c0e9cd41bf8f0b02
design_sha256=bdbf0022e2f34eb3df102122612a1b25042daf0b97631fcc6d5649384754e926
tasks_sha256=fdde3e94d0f91b08c1889876a2a904d0bf4140a1d925720ef70f9d7c7104ed8d
apply_progress_sha256=96a021e3969b81a515270a42eced6cce7acea488a569db22c4c6448e37f5656d
engram_observations=3213,3214,3216,3224,3233,3270,3289,3298,3360
tracked_diff_sha256=c4a109225fb39ba730472183a426e2628959e6da2f6f46ac045fdc0d81b73f72
tracked_status_sha256=9aec857381bb88e8d96a5e451d91c3154e2b8370346538b0b69bca854e09af8d
focused_clone_sha256=850bbf60e20c080fdeced1cc3cc47f232fd6526dd871480a18a809eeb25c7506
focused_dump_sha256=34809b4e22a09be3f4ce989c37fee4844d66b346edc631dc1c44c84523e87a31
focused_restore_sha256=661200e168dfdd1fde65e1ccf5c1629d3e334740eb7fcd3944ae5904c91a81fb
go_test_sha256=cba62356d2bcbb3830672ea94a806564f529f8895e17a7267481fa03abf03f06
go_vet_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
go_build_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
diff_check_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
go_clean_testcache_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
integration_sha256=c63585042dfc23501464206131ea63cba0103b0f9c7069e7a06a4e4a342b2b4c
role_flags_before=vicho|false|false|true|false|false
owned_databases_before=0
created_identity=dolly_verify_final_public_blockers|vicho|vicho
owned_databases_during=1
owned_databases_after=0
role_flags_after=vicho|false|false|true|false|false
```

### Verdict

**PASS** — 8/8 requirements, 25/25 scenarios, and 11/11 tasks are complete. Blockers: 0. Archive readiness: **READY**.
