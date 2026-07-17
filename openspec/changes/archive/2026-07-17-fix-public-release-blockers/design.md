# Design: Fix Public Release Blockers

## Technical Approach

Patch existing seams without public-signature changes. Validate before mutation; default restore stays transactional, while `WithoutTransaction` permits partial effects.

## Architecture Decisions

| Option | Tradeoff | Decision and rationale |
|---|---|---|
| Retain failed physical target | Manual cleanup and disk use | Atomic `os.Mkdir` rejects existing targets. Remove ownership/identity/recursive cleanup. Failures retain target and wrap cause, path, and cleanup duty; success is unchanged. |
| One scoped truncate | External FKs fail replace | Run one transaction-bound metadata-table `TRUNCATE`, without `CASCADE`; PostgreSQL rejects omitted dependents atomically. |
| Reject transactional schema replay | No default auto-create | Missing schema plus `WithSchemaSQL` fails before `psql` in transactional mode; only `WithoutTransaction` permits it. |
| Bounded cleanup | Cleanup can time out | Schema-replay `DROP DATABASE` uses an independent 10-second context; primary error wins and cleanup failure warns. |
| Metadata-declared data files | New dumps need new readers | Optional `data_file` uses deterministic, collision-free lowercase hex UTF-8 schema/table identity. |
| Reuse schema precedence | No new option model | `SchemasFromOptions`: dump wins, restore falls back, all schemas only when unfiltered. |

## Data Flow

### Restore failure flow

```text
Caller -> Restore: metadata + options
Restore -> Resolver: resolve/validate every data_file
Resolver --> Restore: safe paths or error (no mutation)
Restore -> PostgreSQL: introspect/validate schema
alt missing schema + transactional + WithSchemaSQL
  Restore --> Caller: fail closed (psql not invoked)
else missing schema + explicit WithoutTransaction
  Restore -> psql: sanitized schema.sql
end
Restore -> PostgreSQL: BEGIN; single TRUNCATE without CASCADE; load; sequences
PostgreSQL --> Restore: any error
Restore -> PostgreSQL: ROLLBACK
Restore --> Caller: original error
```

### Clone failure flow

```text
Run -> PhysicalBackup: Mkdir TargetDir
alt exists: reject before pg_basebackup; untouched
else created
  PhysicalBackup -> pg_basebackup: execute
  alt subprocess/post-validation fails
    PhysicalBackup --> Run: wrapped cause + retained path + cleanup duty; no deletion
  else success: return nil; retain completed target
  end
end

Run -> SchemaReplay: CREATE DATABASE; postCreate
postCreate --> SchemaReplay: failure/cancellation
SchemaReplay -> PostgreSQL: DROP DATABASE (independent 10s context)
SchemaReplay --> Run: primary error; cleanup failure only warned
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/clone/strategy_replication.go` | Modify | Reject existing targets; remove cleanup; report retained failures. |
| `internal/clone/strategy_schema_replay.go` | Modify | Bound cleanup independently. |
| `internal/clone/strategy_copy_stream.go` | Modify | Apply schema precedence before listing all. |
| `internal/db/models.go` | Modify | Add optional `data_file`. |
| `internal/dump/{dump,stream,metadata}.go` | Modify | Emit declared paths in all modes. |
| `internal/restore/{metadata,restore,conflict}.go` | Modify | Validate paths, gate replay, scope replace. |
| `internal/{clone,dump,restore}/*_test.go` | Modify | Focused regressions. |
| `internal/restore/restore_integration_test.go` | Modify | FK and rollback proof. |

## Interfaces / Contracts

`data_file = "data/" + hex.EncodeToString([]byte(schema)) + "." + hex.EncodeToString([]byte(table)) + ".ndjson"`. New dumps always set it. Missing field resolves to legacy `<table>.ndjson`.

Before mutation, reject empty, absolute, backslash, non-clean, traversal, duplicate, directory, and escaping-symlink paths. INSERT/COPY share resolution. `WithReplace()+WithoutTransaction()` fails before mutation, including library calls.

Physical backup requires successful `os.Mkdir(TargetDir, 0700)` before launch. Later errors preserve cause via `%w`, name retained target, require operator cleanup, and never recursively delete it.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Physical target rejection, failure retention/error, unchanged success | Reuse `mockCommandRunner`/`t.TempDir()`: no launch for existing target; retain subprocess/post-validation failures; assert path, cleanup duty, `errors.Is`, and successful config/signal files. Delete removal/identity tests. |
| Unit | Hex paths, cross-schema names, fallback, unsafe-path rejection | `t.TempDir()`; validation precedes SQL. |
| Unit | Schema fail-closed, scoped truncate, mode rejection | `go-sqlmock` ordering. |
| Integration | External-FK rejection and load rollback | `DOLLY_TEST_PG_DSN`; short mode skips. |

## Threat Matrix

Process integration changes are present, but matrix-specific VCS/classification boundaries are absent.

| Boundary | Applicability | Reason |
|---|---|---|
| Documentation-like paths | N/A | No executable-file classification changes. |
| Git repository selection | N/A | No Git invocation. |
| Commit state | N/A | No commit automation. |
| Push state | N/A | No push automation. |
| PR commands | N/A | No PR command composition. |

## Compatibility, Risks, Rollout, and Rollback

No migration or flag. CLI and successful backups are unchanged. A failed backup blocks same-path retry until explicit cleanup; its error names the retained path. New readers restore old metadata; old readers need not support new dumps.

**Risk**: partial data consumes disk or looks complete. Error text assigns inspection/cleanup; only success emits completion. No recursive cleanup means no path-deletion TOCTOU.

Forecast remains 350–650 authored lines, below 800; deletion should reduce it. Roll back the release-blocker commit and keep publication blocked. Never restore recursive path cleanup without a proven race-free mechanism; retained targets remain operator-managed. Pair new dumps with their binary.

## Open Questions

None.
