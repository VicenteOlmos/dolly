# Proposal: Fix Public Release Blockers

## Intent

Remove six confirmed safety hazards before public release. Preserve command names and option signatures; prefer safe failure over partial or collateral mutation.

## Scope

### In Scope
1. Reject any existing physical-backup target before subprocess execution. If `pg_basebackup` or post-backup validation fails, retain the partial Dolly-created target and return an error naming it for explicit operator cleanup; never recursively delete the target path.
2. Replace per-table `TRUNCATE ... CASCADE` with one transaction-bound, metadata-table-only truncate without `CASCADE`; fail closed on external FK dependencies.
3. In default transactional restore, reject missing-schema auto-application: external `psql` cannot join a `database/sql` transaction. Permit it only in explicit non-transactional mode. Full atomic schema execution is deferred; publication requires this fail-closed boundary.
4. Run schema-replay database cleanup with an independent bounded context after cancellation.
5. Write `data/<hex-utf8(schema)>.<hex-utf8(table)>.ndjson`; table metadata declares `data_file`. Restore uses that relative path, rejects unsafe paths, and falls back to `<table>.ndjson` only when absent.
6. Apply `SchemasFromOptions` to logical-stream enumeration, preserving dump-first, restore-fallback precedence; enumerate all only without filters.

### Out of Scope
- Transactional arbitrary `schema.sql` execution, dump migration, UI redesign, unrelated findings.
- Old binaries reading new schema-aware dumps.

## Capabilities

### New Capabilities
- `logical-clone-safety`: filtered logical-stream execution and cancellation-safe schema-replay cleanup.

### Modified Capabilities
- `clone-production-scale`: preflight target rejection and non-destructive failed-backup retention.
- `postgres-dump-streaming`: collision-free, metadata-declared data paths.
- `postgres-restore-streaming`: safe paths, scoped replace, legacy fallback, and honest transaction boundaries.

## Approach

Use existing option, metadata, transaction, and cleanup seams with focused unit and opt-in PostgreSQL tests. Physical backup failures retain their target and identify it in the returned error. This trades automatic disk cleanup for eliminating path-based recursive-deletion races and potential caller-owned data loss. The other five blocker approaches remain unchanged. Deliver one PR, forecast 350–650 authored lines; a full schema executor or forecast above 800 requires proposal revision.

## Affected Areas

| Area | Impact |
|---|---|
| `internal/clone/` | Modified: blockers 1, 4, 6 |
| `internal/dump/`, `internal/restore/` | Modified: blockers 2, 3, 5 |
| `openspec/specs/` | Modified/new behavioral contracts |

## Backward Compatibility and Acceptance Boundaries

Public names/options remain stable. Failed physical backups now require explicit operator cleanup before retry; successful backups are unchanged, and existing targets remain rejected before subprocess execution. New readers accept legacy metadata; missing or unsafe paths fail before mutation. Default transactional failures leave schema and data unchanged. Explicit non-transactional mode may retain partial effects.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| FK truncation or schema compatibility failure | Medium | Fail closed; integration-test PostgreSQL behavior |
| Format/path regression | Medium | Deterministic encoding, containment checks, legacy fixtures |
| Partial backup consumes disk or is mistaken for complete | Medium | Return an error naming the retained path; require explicit inspection and cleanup |

## Rollback Plan

Revert the release-blocker commit and keep publication blocked. Do not restore path-based recursive cleanup unless a race-free mechanism proves it cannot delete caller-owned or replacement data. Retained partial targets remain available for explicit operator cleanup; legacy dumps remain untouched/readable.

## Dependencies

- Existing Go/PostgreSQL tooling; `DOLLY_TEST_PG_DSN` for integration proof.

## Success Criteria

- [ ] Failed physical backup or validation retains its Dolly-created target, and the returned error identifies that path for explicit cleanup.
- [ ] Existing physical-backup targets are rejected before subprocess execution.
- [ ] The other five blocker outcomes remain unchanged and pass focused regression tests.
- [ ] `go test ./...` and applicable PostgreSQL integration tests pass.
- [ ] Single PR stays within 800 authored changed lines, or stops for explicit replanning.
