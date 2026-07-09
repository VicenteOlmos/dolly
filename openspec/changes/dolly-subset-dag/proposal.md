# Proposal: dolly-subset-dag

## Intent

Add **partial dumps**: start from seed row predicates, expand along the FK graph to a referentially consistent closure, and export only included tables and rows as the existing NDJSON + `metadata.json` artifacts. Full-schema dump remains the default.

## Scope

### In Scope

- Declarative `RowPredicate` (`eq`, `in`, `is_null`); validated JSON seed file and CLI flags
- Bidirectional FK BFS closure; `SortTables` on the induced subgraph
- Parameterized `WHERE` in streaming; caps (`MaxDepth`, `MaxTables`, `MaxRows`, `MaxInListSize`)
- Subset manifest in metadata; `Dump` with `WithSubset(cfg)` (or dedicated entry point)
- CLI MVP (`--seed-file`, limits); planner + integration tests on fixture DAG

### Out of Scope

- Raw SQL `WHERE` fragments (v1)
- TUI subset UX (phase 2)
- Non-`public` schema; cross-schema FK targets outside loaded schema
- Composite / multi-column PK closure (document limitation; follow-up)
- Restore code changes when closure is FK-consistent

## Capabilities

### New Capabilities

- `postgres-dump-subset`: Seed predicates, FK graph closure, bounded partial table/row export

### Modified Capabilities

- `postgres-dump-streaming`: Coexistence of full vs subset mode; metadata subset manifest fields

## Approach

**Approach A** (exploration recommendation): structured seeds + planner inside `internal/dump`.

1. Validate seeds against introspected schema; compile predicates to fixed templates (`=`, `= ANY()`, `IS NULL`) with `pgx.Identifier` and bound `$N` args only.
2. BFS on bidirectional FK adjacency from seed keys; track visited `(table, pk)`; enforce limits.
3. `SortTables` on included tables; stream each with merged `WHERE`.
4. Write subset manifest (seeds, limits, tables, row counts) into metadata.

CLI and library first; Bubbletea `DumpDraft` extension deferred.

## Phasing

| Phase | Deliverable |
|-------|-------------|
| **1 — CLI / library MVP** | Planner, streamer, seed file, limits, tests, `cmd/dolly dump` flags |
| **2 — TUI** | `DumpDraft` / dump screen fields after UX is specified |

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/dump/dump.go` | Modified | Subset orchestration before stream |
| `internal/dump/stream.go` | Modified | Parameterized `WHERE`; `IN` chunking |
| `internal/dump/order.go` | Modified | Shared graph helpers; sort subgraph |
| `internal/dump/metadata.go` | Modified | Subset manifest section |
| `internal/dump/*_test.go` | Modified | Planner unit + integration tests |
| `cmd/dolly/dump.go` | Modified | `--seed-file`, limit flags |
| `internal/tui/*` | Deferred | Phase 2 |
| `openspec/specs/postgres-dump-subset/` | New | Subset capability spec |
| `openspec/specs/postgres-dump-streaming/` | Modified | Full vs subset + metadata delta |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Composite / multi-column PKs | Med | MVP: single-column PK seeds; document gap |
| Nullable FKs | Med | Skip null references in parent traversal |
| Long RR snapshot on large closure | Med | Document `--no-transaction` tradeoff |
| Incomplete closure vs restore | Med | Fail if referenced parent missing from export |
| Spec drift with full-dump reqs | Low | New `postgres-dump-subset` spec; explicit deltas |

## Rollback Plan

Remove subset types, planner, CLI flags, and metadata manifest fields. Delete `openspec/specs/postgres-dump-subset/` and revert streaming spec deltas. Full `dump.Dump` behavior unchanged; no DB migrations.

## Dependencies

- `postgres-schema-introspection` (FK graph, column validation)
- `postgres-dump-streaming` (NDJSON streaming, ordering, snapshot)

## Success Criteria

- [ ] Seed dump on integration fixture exports expected tables only
- [ ] Limits produce explicit errors; seed file cannot inject SQL
- [ ] Subset dump restores into empty DB via existing restore path
- [ ] CLI `--seed-file` and defaults documented
- [ ] Metadata subset manifest records seeds and caps applied
