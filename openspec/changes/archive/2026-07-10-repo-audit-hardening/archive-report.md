# Archive Report: repo-audit-hardening

**Change**: repo-audit-hardening  
**Archived**: 2026-07-10  
**Verify verdict**: PASS WITH WARNINGS  
**Artifact store**: hybrid (OpenSpec filesystem + Engram)

## Summary

Repo audit hardening change completed the SDD cycle: CI/release PostgreSQL 16 client tooling, CLI destructive-operation stderr guardrails, TUI restore confirmation with redacted target connection, and table-driven guardrail tests. All 18 tasks complete. Delta specs merged into main specs; change folder moved to archive.

## Specs Synced

| Domain | Action | Details |
|--------|--------|---------|
| dolly-cli | Updated | 3 ADDED requirements: Destructive-operation stderr guardrails, Unchanged destructive confirmation gates, Table-driven guardrail CLI tests |
| dolly-tui | Updated | 1 MODIFIED requirement: Destructive history restore confirmation (added redacted target connection + 2 scenarios) |
| postgres-integration-testing | Updated | 1 ADDED requirement: CI and release PostgreSQL 16 client tooling (3 scenarios) |

## Source of Truth Updated

- `openspec/specs/dolly-cli/spec.md`
- `openspec/specs/dolly-tui/spec.md`
- `openspec/specs/postgres-integration-testing/spec.md`

## Archive Location

`openspec/changes/archive/2026-07-10-repo-audit-hardening/`

## Archive Contents

| Artifact | Status |
|----------|--------|
| exploration.md | ✅ |
| proposal.md | ✅ |
| design.md | ✅ |
| tasks.md | ✅ (18/18 complete) |
| verify-report.md | ✅ (PASS WITH WARNINGS) |
| specs/dolly-cli/spec.md | ✅ |
| specs/dolly-tui/spec.md | ✅ |
| specs/postgres-integration-testing/spec.md | ✅ |
| archive-report.md | ✅ |

## Verification Notes (from verify-report)

- **CRITICAL**: None for change scope
- **WARNING**: Broad `go test` package command failed on unrelated timeouts (`TestRunDumpRegistersHistory`, `TestCopyStreamStrategySchemaReplayFailure`); `DOLLY_TEST_PG_DSN` unset locally — integration gates not executed in verify environment
- **SUGGESTION**: Triage pre-existing test hangs for reliable local verify

## Engram Traceability

| Artifact | Observation ID |
|----------|------------------|
| explore | #2377 |
| proposal | #2379 |
| specs | #2381 |
| design | #2384 |
| tasks | #2386 |
| apply-progress | #2436 |
| verify-report | #2441 |

## SDD Cycle

Explore → Propose → Spec → Design → Tasks → Apply → Verify → **Archive** — complete.

Ready for next change.
