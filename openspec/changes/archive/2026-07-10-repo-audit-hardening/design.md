# Design: Repo Audit Hardening

## Technical Approach

Ship **A1 + A3** from exploration: one `apt` client install in CI/release integration jobs, stderr guardrails in CLI clone/restore using existing `warning:`/`info:` prefixes, restore TUI confirm body parity with clone confirm, and table-driven CLI tests. Prerequisites already in tree (`requirePgDumpMajorMatch`, schemacapture/COPY integration, `exec.go` redaction) are **verify-only** — no redesign.

## Architecture Decisions

| Decision | Alternatives | Choice | Rationale |
|----------|--------------|--------|-----------|
| PG client install | PGDG apt repo (A2) | `apt-get install -y postgresql-client-16` after `setup-go`, before integration test (A1) | Smallest diff; Noble ships package; matches `postgres:16-alpine` service |
| PGDG fallback | None | Document only in this design + workflow comment | Only needed if `ubuntu-latest` drops package; avoid heavier CI until proven |
| Unsanitized warning | TUI modal (A4) | One `warning:` in `runCloneExecute` when `!cfg.Sanitization.Enabled` **or** strategy ∈ `{template, logical-stream, physical-backup}` | Audit asks for strong warning, not new confirm framework |
| Target visibility | Redacted full DSN on CLI | `info: target database: <name>` via `databaseFromDSN` | Matches proposal; TUI uses full redacted DSN in modal (clone pattern) |
| SkipCreate warning | Block without `--yes` | One `warning:` when `cfg.Clone.SkipCreate` | Preflight already checks permissions; warn partial-state only |
| Restore destructive info | Only `--replace` | Emit on `--replace --yes`; skip `--no-transaction` info (optional in explore) | Proposal scopes to replace; `--yes` gate already exists for no-transaction |
| Test surface | New test package | Table-driven in `clone_test.go` / `restore_test.go` + assert `app.modal.body` in `app_dump_test.go` | Reuses `captureStderr`, `cloneRun`/`restoreRestore` stubs |

## Data Flow

```
CI/release job
  checkout → setup-go → apt install postgresql-client-16 → go test -tags=integration
                              ↓
                    requirePgDumpMajorMatch (fatal if missing/mismatch)

CLI clone (runCloneExecute)
  --yes gate (replace) → [info: target database] → [warning: unsanitized] → [warning: skip_create] → cloneRun

CLI restore (runRestore)
  parse flags (--yes gates) → ping → [info: target database if replace+yes] → restoreRestore

TUI restore confirm
  restoreNeedsConfirm → mountRestoreConfirmModal(body: Path + Target:redacted DSN + policy)
```

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `.github/workflows/ci.yml` | Modify | Add named step after `setup-go`, before `go test -tags=integration` |
| `.github/workflows/release.yml` | Modify | Same step after `setup-go`, before integration test (before `go vet` race suite is fine — place immediately before integration `go test`) |
| `cmd/dolly/clone.go` | Modify | Guardrails in `runCloneExecute` (see insertion points below) |
| `cmd/dolly/restore.go` | Modify | `info:` after successful ping, before `restoreRestore` |
| `internal/tui/app.go` | Modify | `handleRestoreConfirmRequested` body (~L782) |
| `cmd/dolly/clone_test.go` | Modify | Table-driven stderr guardrail tests |
| `cmd/dolly/restore_test.go` | Modify | Table-driven `info: target database:` test |
| `internal/tui/app_dump_test.go` | Modify | Assert restore modal body contains redacted `Target:` |

### Insertion Points (verified)

**`cmd/dolly/clone.go` — `runCloneExecute`**

1. **Replace target info** — immediately after `cfg.Clone.Replace` / `flags.Yes` check passes (~L363–368), before `restoreOpts` append:
   ```go
   fmt.Fprintf(os.Stderr, "info: target database: %s\n", databaseFromDSN(targetURL))
   ```
2. **Unsanitized warning** — after `dumpOpts` built (~L361), before `clone.Options{...}`:
   ```go
   if !cfg.Sanitization.Enabled || strategy == "template" || strategy == "logical-stream" || strategy == "physical-backup" {
       fmt.Fprintf(os.Stderr, "warning: clone will copy unsanitized data (strategy=%s, sanitization=%v)\n", strategy, cfg.Sanitization.Enabled)
   }
   ```
3. **SkipCreate warning** — adjacent to unsanitized block:
   ```go
   if cfg.Clone.SkipCreate {
       fmt.Fprintf(os.Stderr, "warning: skip_create may leave partial state on the existing target database if the clone fails\n")
   }
   ```

**`cmd/dolly/restore.go` — `runRestore`**

After `restorePingContext` succeeds (~L142–144), before building `opts`:
```go
if flags.Replace && flags.Yes {
    fmt.Fprintf(os.Stderr, "info: target database: %s\n", databaseFromDSN(dsn))
}
```

**`internal/tui/app.go` — `handleRestoreConfirmRequested` (~L782)**

Mirror clone confirm (`L451–452`):
```go
body := fmt.Sprintf("Path: %s\nTarget: %s\n\nThis will %s.", msg.inputDir, connections.RedactMessage(a.conn.DSN()), policy)
```

## Interfaces / Contracts

No new exported APIs. Stderr contracts:

| Prefix | When | Example substring |
|--------|------|-------------------|
| `info: target database:` | Clone replace+yes; restore replace+yes | `info: target database: mydb` |
| `warning:` | Unsanitized clone; SkipCreate | `unsanitized`, `skip_create` |

`--yes` gates unchanged. JSON mode: guardrail lines still emit to stderr (same as `logCloneSchemas`).

## Testing Strategy

| Layer | What | Approach |
|-------|------|----------|
| Unit (CLI) | Guardrail stderr | Table-driven `t.Run`; stub `cloneRun`/`restoreRestore`/`restorePingContext`; `captureStderr`; substring asserts |
| Unit (CLI) | `--yes` unchanged | Existing `TestRunCloneReplaceRequiresYesAllModes`, `TestParseRestoreFlagsNoTransactionRequiresYes` — no change expected |
| TUI | Restore modal body | Extend `TestAppDumpRestoreDestructiveRequiresConfirm`: `app.modal.body` contains `Target:` and no raw password (pattern from `app_clone_test.go`) |
| Integration | Prerequisites | `go test -tags=integration` with DSN after workflow change; verify-only for `requirePgDumpMajorMatch`, `capture_integration_test.go`, `TestIntegrationLoadTableCopy`, `commandFailed` redaction |

## Migration / Rollout

No migration. Ship workflow + guardrails atomically (CI fails closed without client). **Rollback:** revert workflow step + stderr/TUI lines; `--yes` behavior unchanged.

### PGDG fallback (if `postgresql-client-16` missing)

```yaml
# ponytail: only if Noble package drifts — not in v1 diff
- run: |
    sudo apt-get update
    sudo apt-get install -y curl ca-certificates
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo gpg --dearmor -o /usr/share/keyrings/postgresql.gpg
    echo "deb [signed-by=/usr/share/keyrings/postgresql.gpg] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
    sudo apt-get update && sudo apt-get install -y postgresql-client-16
```

## Prerequisites (verify only)

| Item | Location | Status |
|------|----------|--------|
| Fail-closed pg_dump major match | `internal/clone/clone_integration_test.go:requirePgDumpMajorMatch` | Present — uses `t.Fatal` |
| Redacted subprocess errors | `internal/clone/exec.go:commandFailed` | Present — `connections.RedactMessage` |
| Schemacapture integration | `internal/schemacapture/capture_integration_test.go` | Present |
| COPY restore integration | `internal/restore/restore_integration_test.go:TestIntegrationLoadTableCopy` | Present |

## Open Questions

- [ ] None blocking — PGDG fallback deferred until CI proves Noble package gap.
