## Exploration: dolly-subset-dag — Partial dumps via seed rows + FK dependency traversal

### Current State

dolly today implements **full public-schema dumps only**. There is no subset, seed, or row-level filter capability.

**Dump pipeline (`internal/dump`)**

- `Dump(ctx, db, outputDir, opts...)` loads all `public` tables via `db.LoadPostgresPublicSchema`, orders them with `SortTables` (Kahn topological sort on FK parent→child edges), writes `metadata.json`, then streams each table with `SELECT <cols> FROM <table>` — **no `WHERE` clause** (`stream.go`).
- Options today: `WithoutTransaction()`, `WithProgress(fn)`.
- Default snapshot: `REPEATABLE READ READ ONLY` transaction wrapping schema + all row reads.
- Artifacts: `metadata.json` + `<table>.ndjson` per table (same shape restore expects).

**Schema / FK graph (`internal/db`)**

- `Table` carries `Columns` (with `PrimaryKey` flag) and `ForeignKeys` (`ConstraintName`, source column, referenced schema/table/column).
- FK metadata comes from `information_schema` with **parameterized** introspection queries (`$1` for table name).
- Integration fixture (`fixtures.sql`) models a **non-trivial DAG**: `departments` ← `employees` ← `project_members` → `projects` (nullable `department_id`), plus `empty_audit` with no FKs. Good graph for subset tests.

**Topological sort (`internal/dump/order.go`)**

- Builds `adj[parent] → []child` and `inDegree` from each table's outbound FKs (same-schema `public` only).
- Handles cycles by appending unvisited tables in stable name order after Kahn exhausts.
- **One-direction only**: orders tables for insert/restore safety (parents before children). Subset traversal needs **both** directions: follow FKs to parents (dependencies) and reverse edges to children (dependents).

**SQL safety patterns already in repo**

- Identifiers: `pgx.Identifier{...}.Sanitize()` in `stream.go` and `restore/conflict.go`.
- Values: `QueryContext(ctx, query, args...)` with `$N` placeholders in introspection and restore inserts.
- **No** user-supplied SQL fragments in dump path today.

**CLI / TUI surfaces**

- CLI: `dolly dump --dsn --output [--no-transaction]` → `dump.Dump` (`cmd/dolly/dump.go`).
- TUI: `DumpDraft{OutputDir, NoTransaction}` → `productionDumpRunner` → `dump.Dump` (`internal/tui/dump_run.go`, `screen.go`).
- Restore is symmetric full-metadata load; subset dumps would still restore if closure is FK-consistent.

**OpenSpec**

- `postgres-dump-streaming` explicitly scopes out UI and requires full-table streaming; subset work will need a **new capability spec** (delta) plus possible metadata extension spec.
- `postgres-schema-introspection` already delivers everything needed to validate filters and build the FK graph.

### Affected Areas

| Path | Why |
|------|-----|
| `internal/dump/dump.go` | Orchestration: must plan subset table set + row predicates before streaming |
| `internal/dump/stream.go` | Add parameterized `WHERE` generation; chunk large `IN` lists |
| `internal/dump/order.go` | Extract shared graph helpers; subset may dump **fewer** tables but still need FK-safe order among included tables |
| `internal/dump/metadata.go` | Record subset manifest (seeds, limits hit, tables included) for reproducibility |
| `internal/db/models.go` | Possibly add helpers (`PrimaryKeyColumns`, `FKGraph`) — optional, keep dump-focused |
| `internal/dump/*_test.go` | Table-driven planner tests; integration tests on fixture DAG |
| `cmd/dolly/dump.go` | Flags or `--plan` file for seeds/filters/limits |
| `internal/tui/dump.go`, `screen.go`, `dump_run.go` | Extend `DumpDraft` when UX is defined (can be phase 2) |
| `openspec/specs/postgres-dump-streaming/spec.md` | Main spec merge after change (or new `postgres-dump-subset` domain) |
| `internal/restore/*` | No code change required for MVP if subset closure maintains referential integrity; document that restore to DB with extra rows may need `--on-conflict` |

### Approaches

| Approach | Description | Pros | Cons | Effort |
|----------|-------------|------|------|--------|
| **A. Structured seed + graph closure in `internal/dump`** | Add `SeedSpec` / `RowFilter` types; planner BFS over FK graph; `streamTable` with bound `WHERE` | Matches existing package boundaries; reuses `SortTables` patterns | `dump` package grows; planner + streamer coupled | **Medium** |
| **B. New `internal/subset` planner + thin `dump` hook** | Planner returns `{table → predicate}`; dump only executes | Clear separation; easier unit test graph logic | Extra package; orchestration split | **Medium–High** |
| **C. User-authored SQL `WHERE` per table** | Pass raw SQL fragment per table | Flexible for power users | **Unsafe** unless heavily restricted; violates injection goals | Low code, **high risk** |
| **D. External tool (pg_dump + manual)** | Defer to Postgres native tools | No dolly work | Breaks NDJSON/metadata story; not product direction | N/A |

### Recommendation

**Approach A (structured seed + closure inside `internal/dump`, with optional extract of `graph.go` later)** for MVP:

1. **Filter model (safe)**: Declarative predicates only — `(table, column, op, values)` where `table`/`column` must exist in loaded schema; `op` ∈ `{eq, in, is_null}` (extend later). Compile to `WHERE col = $1` / `WHERE col = ANY($1)` with **only** `pgx.Identifier` for identifiers. Reject free-text SQL.
2. **Seeds**: Named seed groups: `{table, filter}` e.g. `employees.id IN (1,2)` or `departments.code = 'ENG'`.
3. **Traversal**: From seed tables, BFS on FK graph:
   - **Downstream**: tables with FK → current table; predicate `fk_col IN (...)` from discovered PKs.
   - **Upstream**: referenced parent tables; predicate on referenced PK columns from FK values in child rows.
   - Restrict to tables in `public` schema already loaded.
4. **Order**: `SortTables` on the **induced subgraph** (included tables only).
5. **Limits**: `MaxTables`, `MaxRows`, `MaxDepth`, `MaxInListSize` (chunk queries), `context.Context` cancellation — fail with explicit error when exceeded.
6. **API**: `dump.SubsetDump(ctx, db, outputDir, SubsetConfig{Seeds, Limits, ...}, opts...)` or `Dump` + `WithSubset(cfg)` option to keep one entry point.
7. **CLI first**, TUI second: add `--seed-file` JSON / repeated `--seed-table` flags before Bubbletea UX.

### Answers to the four questions

#### 1. How to express table filters safely?

Use a **schema-validated structured predicate**, not SQL strings:

```go
type RowPredicate struct {
    Table  string
    Column string
    Op     PredicateOp // Eq, In, IsNull
    Values []any       // bound as query args
}
```

At plan time: resolve `Table`/`Column` against `[]db.Table` from introspection; ensure column type is compatible with values; build SQL with sanitized identifiers and `$1..$n` placeholders. Mirror `restore`’s `buildInsert` discipline. Optional JSON/YAML seed file validated before any query runs.

**Do not** accept arbitrary `WHERE` clauses from CLI/TUI in v1.

#### 2. How to traverse the FK graph?

Build two adjacency maps from `[]db.Table`:

- `childToParents[table]` — from `ForeignKeys` (already implied by `SortTables`).
- `parentToChildren[table]` — invert edges where `ReferencedTableName` is in schema.

**Closure algorithm** (per seed batch):

1. Execute seed predicate → set of row keys (PK columns; composite PK supported via tuple encoding).
2. Queue `(table, keys)`; for each hop (respect `MaxDepth`):
   - For each child table referencing this table: `SELECT` child PKs / FK columns `WHERE ref_col IN keys`.
   - For each parent table referenced by rows: collect distinct referenced PK values from child rows.
3. Union tables and keys; stop when no new keys or limits fire.

Dump order: `SortTables(filteredTables)` so parents still precede children among exported tables.

#### 3. How to avoid SQL injection?

| Layer | Rule |
|-------|------|
| Identifiers | Only from introspected names via `pgx.Identifier.Sanitize()` |
| Values | Always `QueryContext(..., args)`; never `fmt.Sprintf` user input into SQL |
| Filter spec | Parse JSON/YAML into typed structs; validate ops and column names against schema |
| Dynamic SQL | Only compile patterns from a fixed template catalog (`=`, `= ANY()`, `IS NULL`) |

Existing `streamTable` is safe because identifiers come from DB metadata; subset work must **not** regress by accepting raw SQL.

#### 4. How to prevent runaway graph expansion?

| Guard | Purpose |
|-------|---------|
| `MaxDepth` | Cap FK hops from seeds |
| `MaxTables` | Cap distinct tables in closure |
| `MaxRows` / `MaxRowsPerTable` | Stop after N rows read per table or globally |
| `MaxInListSize` | Split `IN`/`ANY` into batched queries |
| `Context` cancel | User/TUI cancel |
| Pre-check | If seed predicate is empty or matches whole table (optional `EXPLAIN` / count threshold), warn or reject |
| Metadata | Write `subset_manifest` section documenting seeds, limits, rows exported |

Cyclic FKs (possible in real DBs): table **order** already handled; row closure must track visited `(table, pk)` pairs to avoid infinite BFS.

### Risks

- **Composite primary keys** — closure key representation and `IN` batching are harder; MVP may document single-column PK seeds first.
- **Nullable FKs** — parent optional (`projects.department_id`); traversal must not assume non-null.
- **Multi-column FKs** — `information_schema` returns one row per column; planner must group by `constraint_name`.
- **Cross-schema FKs** — current dump ignores non-`public` referenced tables (`order.go` skips); subset must same-scope or error.
- **Snapshot consistency** — long BFS may hold RR snapshot; large closures increase transaction duration; document `--no-transaction` tradeoff.
- **Restore to full DB** — subset may omit parents if seeds were inconsistent; validate closure includes all referenced parents before write completes.
- **Spec drift** — `postgres-dump-streaming` mandates full public dump; need explicit new requirements for subset mode.

### Ready for Proposal

**Yes.** Next phase (`sdd-propose`) should define MVP scope: seed file format, default limits, CLI flags, metadata extension, and whether TUI is in or out of first slice. Recommend **CLI + library first**, TUI follow-up change.
