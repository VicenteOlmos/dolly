# Proposal: Repo Audit Hardening

## Intent

Close gaps from the repo audit: CI/release integration tests need PostgreSQL 16 client tools to match fail-closed `requirePgDumpMajorMatch`, and destructive clone/restore paths need clearer operator guardrails without new confirmation frameworks. Prior audit items (fail-closed pg_dump, schemacapture/COPY integration, redacted exec errors) are already in the working tree — verify only, do not re-implement.

## Scope

### In Scope
- Install `postgresql-client-16` in `.github/workflows/ci.yml` (`postgres-integration`) and `.github/workflows/release.yml` before integration tests
- CLI guardrails reusing existing `warning:` / `info:` / `--yes` patterns:
  - `warning:` when clone runs without sanitization (disabled or strategy never sanitizes)
  - `info: target database:` before destructive restore/clone `--replace` paths
  - `warning:` for `SkipCreate` partial-state risk
- TUI restore confirm body shows redacted target DSN (mirror clone confirm in `internal/tui/app.go`)
- Table-driven tests in `cmd/dolly/restore_test.go` and `cmd/dolly/clone_test.go` for new stderr behavior

### Out of Scope
- Extra TUI confirm modals for unsanitized/SkipCreate (Approach A4)
- `config.example` sync, dead-code cleanup, TUI `app.go` split
- Re-implementing fail-closed integration, schemacapture, COPY, or `exec.go` redaction

## Capabilities

### New Capabilities
None

### Modified Capabilities
- `dolly-cli`: Destructive-operation stderr guardrails (`warning:` / `info:`) and table-driven CLI tests; existing `--yes` gates unchanged
- `postgres-integration-testing`: CI/release jobs MUST install PG 16 client before `-tags=integration` when DSN is set
- `dolly-tui`: History restore confirmation modal MUST show redacted target connection alongside dump path

## Approach

**A1 + A3** from exploration — smallest viable slice:

1. **Workflows:** `sudo apt-get update && sudo apt-get install -y postgresql-client-16` after checkout/setup-go, before `go test -tags=integration`. Same step in CI and release for parity.
2. **CLI:** One stderr line per concern in `cmd/dolly/clone.go` / `restore.go` using established prefixes; `databaseFromDSN` + `connections.RedactMessage` where needed.
3. **TUI:** Extend `mountRestoreConfirmModal` body only — no new modal types.
4. **Tests:** Table-driven substring assertions; no live DB.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `.github/workflows/ci.yml` | Modified | PG 16 client install step |
| `.github/workflows/release.yml` | Modified | Matching client install |
| `cmd/dolly/clone.go` | Modified | Unsanitized, SkipCreate, replace info |
| `cmd/dolly/restore.go` | Modified | Target DB info on destructive paths |
| `internal/tui/app.go` | Modified | Restore confirm redacted DSN |
| `cmd/dolly/*_test.go` | Modified | Guardrail table tests |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| CI fails until client install lands | High | Ship workflow + test changes together |
| `postgresql-client-16` absent on runner | Low | Document PGDG fallback in design if Noble drifts |
| Warning fatigue | Low | One line per concern max |

## Rollback Plan

Revert workflow install steps and guardrail stderr/TUI lines. `--yes` gates remain; behavior returns to pre-audit (quieter, CI may fail closed without client). No schema or data migrations.

## Dependencies

- Ubuntu `ubuntu-latest` provides `postgresql-client-16` (Noble)
- Working-tree prerequisites: `requirePgDumpMajorMatch`, schemacapture/restore integration, `exec.go` redaction

## Success Criteria

- [ ] CI and release `postgres-integration` jobs pass with `DOLLY_TEST_PG_DSN` set
- [ ] Clone without sanitization emits `warning:`; `SkipCreate` emits partial-state `warning:`
- [ ] Confirmed `--replace` restore/clone emits `info: target database:` before side effects
- [ ] TUI restore confirm shows redacted target DSN
- [ ] New table-driven CLI tests pass without PostgreSQL
- [ ] Diff stays under 400-line review budget
