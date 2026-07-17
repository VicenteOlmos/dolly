# Tasks: Fix Public Release Blockers

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | 250–450 authored; user-approved single-PR exception remains below 800 |
| 400-line budget risk | High |
| Chained PRs recommended | No — existing size exception |
| Suggested split | Single correction PR |
| Delivery strategy | exception-ok |
| Chain strategy | size-exception |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: size-exception
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 1 | Remove target cleanup/identity logic; add focused clone and enumeration tests | PR 1 | `go test ./internal/clone/...` | `DOLLY_TEST_PG_DSN=... make test-integration` for final run | `internal/clone/*` and clone tests |
| 2 | Complete dump/restore unit evidence | PR 1 | `go test ./internal/dump/... ./internal/restore/...` | N/A — filesystem/sqlmock tests | `internal/db/models.go`, `internal/dump/*`, `internal/restore/*` |
| 3 | Prove PostgreSQL replace behavior and final gate | PR 1 | `go test ./...` | `DOLLY_TEST_PG_DSN=... make test-integration` without `-short` | integration tests and verification records |

## Phase 1: Remediation Contracts

- [x] 1.1 Preserve existing-target rejection and successful backup behavior.
- [x] 1.2 RED: assert failed `pg_basebackup`/validation retains partial target, names path, and never runs cleanup/identity deletion.
- [x] 1.3 RED: table-drive selected logical filter forwarding, restore fallback precedence, and canceled cleanup independent context, deadline, and primary-error precedence.

## Phase 2: Minimal Implementation

- [x] 2.1 In `internal/clone/strategy_replication.go`, remove automatic cleanup and ownership/identity logic; retain failed target and wrap error with path and explicit cleanup duty.
- [x] 2.2 Keep bounded independent schema-replay cleanup and `SchemasFromOptions` seam.

## Phase 3: Focused Evidence

- [x] 3.1 In dump tests, add end-to-end same-name cross-schema files/metadata and repeated complete-dump deterministic `data_file` assertions.
- [x] 3.2 In clone tests, assert selected schema slice and restore-option fallback are forwarded; prove cancellation cleanup deadline expiration preserves primary-error precedence and emits a warning.
- [x] 3.3 In `internal/restore/restore_integration_test.go`, add/retain PostgreSQL runtime cases for external-FK rejection and successful replace row loading.

## Phase 4: Final Verification

- [x] 4.1 Run focused tests, `go test ./...`, `go vet ./...`, and `go build -buildvcs=false ./...`.
- [x] 4.2 Require configured, reachable `DOLLY_TEST_PG_DSN`; run `make test-integration` without `-short`. Unset, unreachable, or compile-only execution cannot yield PASS.
- [x] 4.3 Update `apply-progress.md` evidence and checkboxes to match actual results; rerun conventional verify only after all nine missing/partial scenarios pass.
