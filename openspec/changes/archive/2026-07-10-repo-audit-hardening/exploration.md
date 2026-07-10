## Exploration: repo-audit-hardening

### Current State

**CI / integration testing**

- `.github/workflows/ci.yml` defines a `postgres-integration` job on `ubuntu-latest` with `postgres:16-alpine` (port 5433), sets `DOLLY_TEST_PG_DSN`, and runs `go test -tags=integration -p 1 -count=1 ./...`. There is **no** step to install PostgreSQL client binaries (`pg_dump`, `psql`, `pg_basebackup`).
- `.github/workflows/release.yml` mirrors the same service/env and runs the same integration command before building release assets — **same client-tool gap**.
- `internal/clone/clone_integration_test.go` already renames `skipIfPgDumpMajorMismatch` → `requirePgDumpMajorMatch` and uses `t.Fatal` (not `t.Skip`) when `DOLLY_TEST_PG_DSN` is set but `pg_dump` is missing, `--version` fails, or client/server majors differ. DSN absence still skips cleanly.
- Local dev uses `docker-compose.yml` (`postgres:16-alpine` on 5433) and `make test-integration`; docs in `docs/release.md` document the DSN workflow.
- Integration coverage already in tree (out of scope for *new* work but relevant context): `internal/schemacapture/capture_integration_test.go`, `internal/restore/restore_integration_test.go` (`TestIntegrationLoadTableCopy`), unified redacted subprocess errors in `internal/clone/exec.go` (`commandFailed` → `connections.RedactMessage`).

**Destructive / unsanitized guardrails**

| Audit item | Status | Evidence |
|------------|--------|----------|
| Fail-closed pg_dump when DSN set | **Done** (working tree) | `requirePgDumpMajorMatch` in `clone_integration_test.go` |
| Redacted clone subprocess diagnostics | **Done** (working tree) | `internal/clone/exec.go:commandFailed` |
| Reject non-empty `physical-backup` target dir | **Done** | `internal/clone/preflight.go:validateReplicationTargetDir` + `runReplicationPreflight` |
| Strong warning for unsanitized clones | **Missing** (runtime) | Docs/help only: `cmd/dolly/help.go` (sanitization note), `README.md`; TUI strategy descriptions in `internal/tui/clone_strategy.go` mention sanitization only on `schema-replay` |
| Show target DB before `--replace` | **Partial** | CLI: `--replace` requires `--yes` (`cmd/dolly/restore.go:78-79`, `cmd/dolly/clone.go:363-366`) but **does not print** target database/DSN before proceeding. TUI clone confirm shows redacted target DSN (`internal/tui/app.go:451-454`). TUI restore confirm shows dump path only, **not** target DB (`internal/tui/app.go:782-783`) |
| Warn on `SkipCreate` partial-state risk | **Missing** | `clone.SkipCreate` flows through CLI/TUI/config; preflight handles permissions but no user-facing warning |
| Warn on `WithoutTransaction` partial-state risk | **Partial** | CLI blocks `--no-transaction` without `--yes` with explicit message (`restore.go:84-85`). TUI restore never exposes `WithoutTransaction` (`internal/tui/restore_run.go`). No stderr info line at run start |

**Existing confirm/warn patterns to reuse (do not invent new frameworks)**

- CLI `--yes` gates: `parseRestoreFlags`, `runCloneExecute` (`clone.replace`)
- TUI modals: `mountCloneConfirmModal`, `mountRestoreConfirmModal`, `modalState` in `internal/tui/modal.go`; specs in `openspec/specs/tui-confirmation-modal/spec.md`
- Stderr warnings: `warning:` prefix in `cmd/dolly/dump.go`, `internal/clone/strategy_replication.go`
- Stderr info: `info: clone source schemas:` in `cmd/dolly/clone.go:logCloneSchemas`
- DSN redaction: `connections.RedactMessage` (TUI modals, `exec.go`)
- DB name extraction: `databaseFromDSN` in `cmd/dolly/dump.go` (used in JSON results)

### Affected Areas

- `.github/workflows/ci.yml` — add matching PG 16 client install before integration tests
- `.github/workflows/release.yml` — parity with CI (same install step)
- `cmd/dolly/restore.go` — optional pre-run target DB info when destructive flags confirmed
- `cmd/dolly/clone.go` — unsanitized-clone warning, SkipCreate warning, target DB info when `clone.replace`
- `internal/tui/app.go` — restore confirm modal body should include redacted target connection (mirror clone)
- `internal/tui/restore_run.go` — only if TUI gains `no-transaction` later (not required for minimal slice)
- `cmd/dolly/restore_test.go`, `cmd/dolly/clone_test.go` — table-driven flag/guard tests (existing pattern)
- `openspec/specs/dolly-cli/spec.md`, `openspec/specs/postgres-integration-testing/spec.md` — delta specs in propose/spec phase

### Approaches

#### 1. Minimal CI fix — `apt install postgresql-client-16`

- **Description:** Add one step before integration tests in both workflows: `sudo apt-get update && sudo apt-get install -y postgresql-client-16`. Optionally verify with `pg_dump --version`.
- **Pros:** Smallest diff; matches `postgres:16-alpine` service; satisfies `requirePgDumpMajorMatch` and preflight `requireClientTools`; no workflow restructuring
- **Cons:** Depends on Ubuntu package availability on `ubuntu-latest` (Noble ships `postgresql-client-16`); if runner image changes, may need PGDG repo fallback
- **Effort:** Low

#### 2. PGDG apt repository for client tools

- **Description:** Add official PostgreSQL apt repo, install `postgresql-client-16`.
- **Pros:** Version-pinned across Ubuntu releases; closer to production DBA practice
- **Cons:** More workflow lines, apt key/setup, slower CI; overkill if Noble default package suffices
- **Effort:** Medium

#### 3. Guardrails via stderr + existing modal patterns only

- **Description:**
  - **Unsanitized:** At clone start (`runCloneExecute`), emit one `warning:` when `!cfg.Sanitization.Enabled` **or** strategy ∈ `{template, logical-stream, physical-backup}` (template/stream never sanitize; physical-backup is cluster-level).
  - **Replace target visibility:** Before destructive restore/clone, `info: target database: <name>` (and redacted DSN in TUI modal body).
  - **SkipCreate:** `warning:` that failure may leave partial state on existing target DB.
  - **WithoutTransaction:** Keep existing `--yes` gate; optionally add one `info:` line when confirmed (CLI only today).
- **Pros:** Reuses established patterns; no new confirmation framework; small test surface (`restore_test.go` / `clone_test.go` style)
- **Cons:** Warnings are not blocking (by design); TUI still uses modal for replace, not for unsanitized/SkipCreate unless extended
- **Effort:** Low–Medium

#### 4. Extend TUI modals for all risky clone modes

- **Description:** New confirmation gates for unsanitized strategies and SkipCreate (like `modalCloneConfirm` for replace).
- **Pros:** Strongest UX parity with destructive replace flow
- **Cons:** More TUI state, golden updates, broader than audit “strong warning” ask; higher review budget
- **Effort:** Medium–High

### Recommendation

**Ship Approach 1 + Approach 3** as the smallest viable slice for this change:

1. **CI/release:** Add `postgresql-client-16` install to `postgres-integration` and `release` jobs in the same relative position (after checkout/setup-go, before `go test -tags=integration`). Treat as **mandatory companion** to `requirePgDumpMajorMatch` — without it, CI will fail closed (intended) but red until install lands.
2. **Guardrails:** Add stderr `warning:` / `info:` lines in `cmd/dolly/clone.go` and `cmd/dolly/restore.go` using existing helpers; extend TUI restore confirm body to include redacted target DSN (clone modal already does). Do **not** add a new confirmation framework.
3. **Tests:** Table-driven CLI tests asserting warning substrings and unchanged `--yes` requirements; optional golden tweak for restore confirm if body changes.

Defer Approach 4 (extra TUI modals) unless propose phase expands scope.

**Options comparison**

| Approach | Pros | Cons | Complexity |
|----------|------|------|------------|
| A1: apt client-16 | Tiny diff, matches PG16 service | Ubuntu package drift risk | Low |
| A2: PGDG repo | Robust versioning | Heavier CI | Medium |
| A3: stderr + modal tweak | Reuses patterns, small diff | Warnings non-blocking | Low–Med |
| A4: full TUI modals | Strongest UX | Large TUI/golden churn | Med–High |

### Risks

- **CI breakage until paired:** `requirePgDumpMajorMatch` fatals without matching client; client install must land in the same change (or immediately after).
- **Ubuntu package name:** If `ubuntu-latest` ever lacks `postgresql-client-16`, fallback to PGDG or pin runner image — document in design.
- **Warning fatigue:** Multiple stderr lines per run; keep to one line per concern.
- **400-line budget:** CI + CLI guardrails + tests should stay well under default PR budget; avoid TUI modal expansion in v1.

### Ready for Proposal

**Yes.** Propose scoped delivery: (1) workflow client install with release parity, (2) CLI/TUI guardrail messages reusing `--yes` and modal patterns, (3) focused tests. Items 1–3 from audit backlog (fail-closed, schemacapture, COPY, exec redaction) are already in tree — verify only, do not re-implement.
