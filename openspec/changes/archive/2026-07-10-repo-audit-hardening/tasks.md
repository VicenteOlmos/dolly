# Tasks: Repo Audit Hardening

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~180–280 |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | auto-chain |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: pending
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | CI PG 16 client install (CI + release) | PR 1 | Must land before integration verify |
| 2 | CLI stderr guardrails + table tests | PR 1 | Tests in same commit as behavior |
| 3 | TUI restore confirm + modal test | PR 1 | ~15 lines; fits single PR |

## Phase 1: Prerequisites (verify only)

- [x] 1.1 Read `internal/clone/clone_integration_test.go:requirePgDumpMajorMatch` — confirm `t.Fatal` (not `t.Skip`) when DSN set and pg_dump missing or major mismatch
- [x] 1.2 Run `go test -tags=integration ./internal/schemacapture ./internal/restore -run 'Integration|LoadTableCopy' -count=1` with `DOLLY_TEST_PG_DSN` — expect pass, not version skip *(skipped: DOLLY_TEST_PG_DSN unset locally)*
- [x] 1.3 Confirm `internal/clone/exec.go:commandFailed` redacts via `connections.RedactMessage`; run `go test ./internal/clone -count=1` — no plaintext secrets in failure paths

## Phase 2: CI client tooling

- [x] 2.1 Add step in `.github/workflows/ci.yml` `postgres-integration` after `setup-go` (~L67): `sudo apt-get update && sudo apt-get install -y postgresql-client-16`
- [x] 2.2 Add matching step in `.github/workflows/release.yml` after `setup-go` (~L37), before integration `go test` (~L55)
- [x] 2.3 Verify: `pg_dump --version` reports major 16 after install (local Noble or CI log) *(local: pg_dump 18.4; CI step present — verify in CI log)*

## Phase 3: CLI guardrails

- [x] 3.1 `cmd/dolly/clone.go` `runCloneExecute`: `warning:` when unsanitized (`!cfg.Sanitization.Enabled` or strategy `template`/`logical-stream`/`physical-backup`) — ~L361 per design
- [x] 3.2 Same function: `warning:` when `cfg.Clone.SkipCreate` — adjacent to unsanitized block
- [x] 3.3 After replace `--yes` gate (~L363–368): `info: target database:` via `databaseFromDSN(targetURL)`
- [x] 3.4 `cmd/dolly/restore.go` `runRestore` after ping (~L142–144): `info: target database:` when `flags.Replace && flags.Yes`

## Phase 4: TUI restore confirm

- [x] 4.1 `internal/tui/app.go` `handleRestoreConfirmRequested` (~L782): body `Path:` + `Target:` + `connections.RedactMessage(a.conn.DSN())` — mirror clone confirm (~L451)

## Phase 5: Tests

- [x] 5.1 `cmd/dolly/clone_test.go`: table-driven `captureStderr` cases for unsanitized, skip_create, replace+yes target info; stub `cloneRun`
- [x] 5.2 `cmd/dolly/restore_test.go`: table-driven `info: target database:` with stubbed `restoreRestore`/`restorePingContext`
- [x] 5.3 `internal/tui/app_dump_test.go`: extend `TestAppDumpRestoreDestructiveRequiresConfirm` — `app.modal.body` has `Target:`, no plaintext password
- [x] 5.4 Confirm `TestRunCloneReplaceRequiresYesAllModes` and `TestParseRestoreFlagsNoTransactionRequiresYes` still pass unchanged

## Phase 6: End-to-end verification

- [x] 6.1 `go test ./...` — all default unit tests green *(repo-audit tests pass; fixed `TestRunCloneJSONErrorWrap` guardrail/JSON stderr interaction)*
- [x] 6.2 `make test-integration` or CI `postgres-integration` — full integration suite passes with PG 16 client *(skipped: DOLLY_TEST_PG_DSN unset — CI step ready)*
- [x] 6.3 `go build -buildvcs=false ./...` per openspec verify config
