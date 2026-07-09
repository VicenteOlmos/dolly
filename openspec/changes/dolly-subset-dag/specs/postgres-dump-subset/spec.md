# PostgreSQL Dump Subset Specification

## Purpose

Define how `internal/dump` exports a **referentially consistent subset** of the public schema: seed row predicates, FK-graph closure, bounded table/row inclusion, and the same NDJSON + `metadata.json` artifact shape as full dumps.

## Requirements

### Requirement: RowPredicate Model

The system MUST accept declarative row predicates per table with operators `eq`, `in`, and `is_null` only. Each predicate MUST name an existing column and typed literal value(s) validated against introspected column types.

#### Scenario: Valid eq predicate

- GIVEN schema metadata lists table `employees` with column `id` of type integer
- WHEN a seed specifies `{ "table": "employees", "column": "id", "op": "eq", "value": 42 }`
- THEN validation succeeds
- AND the predicate is eligible for closure planning

#### Scenario: Unknown operator rejected

- GIVEN a seed uses operator `like`
- WHEN seeds are validated
- THEN validation fails with an error naming the unsupported operator
- AND no dump I/O starts

### Requirement: Seed Input Validation

The system MUST load seeds from a validated JSON seed file and/or programmatic configuration. Seeds MUST NOT accept raw SQL `WHERE` fragments in v1.

#### Scenario: Seed file references unknown table

- GIVEN a seed file names table `missing_table`
- WHEN subset configuration is validated against loaded public schema
- THEN validation fails before any table is streamed

#### Scenario: Seed values cannot inject SQL

- GIVEN a seed literal contains characters that would alter SQL structure if concatenated
- WHEN predicates are compiled for streaming
- THEN only bound parameters (`$N`) and identifier-quoted names are used
- AND user literals never appear as executable SQL fragments

### Requirement: FK Closure Planning

The system MUST expand seeds with bidirectional BFS over the public-schema FK adjacency graph until the referential closure is reached or a configured limit stops expansion. The planner MUST track visited primary-key identities per table.

#### Scenario: Child rows pull in parent tables

- GIVEN seed rows on `employees` and `employees.department_id` references `departments`
- WHEN closure planning completes within limits
- THEN `departments` is included
- AND parent rows referenced by included child rows are reachable in the export plan

#### Scenario: Parent seeds pull in dependent rows

- GIVEN seed rows on `departments`
- WHEN closure planning walks outgoing FK references
- THEN dependent tables referencing those parents are included when within limits

#### Scenario: Nullable FK references skipped in traversal

- GIVEN a child row has `NULL` in a referencing FK column
- WHEN the planner traverses from that child
- THEN it does not enqueue a parent lookup for that edge

### Requirement: Closure Limits

The system MUST enforce configurable caps: `MaxDepth`, `MaxTables`, `MaxRows`, and `MaxInListSize`. Exceeding a cap MUST fail the dump with an explicit limit error.

#### Scenario: MaxTables exceeded

- GIVEN `MaxTables` is 2 and closure would include 3 tables
- WHEN planning finishes
- THEN the dump returns an error identifying `MaxTables`
- AND no completed subset artifacts are promoted

#### Scenario: Large IN lists are chunked

- GIVEN closure produces many primary-key values for one table
- WHEN row predicates use `in`
- THEN the streamer MAY split `IN` lists into chunks bounded by `MaxInListSize`
- AND all chunks together MUST export the same logical row set

### Requirement: Included Table Ordering

The system MUST call `SortTables` on the **included** table set only, preserving parent-before-child order on the induced FK subgraph and deterministic order for cycles.

#### Scenario: Subgraph respects parent-before-child

- GIVEN included tables `departments` and `employees` with an FK from `employees` to `departments`
- WHEN export order is computed
- THEN `departments` is scheduled before `employees`

### Requirement: Subset Row Streaming

The system MUST stream only rows matching merged predicates per included table using parameterized `WHERE` clauses compiled from fixed templates (`=`, `= ANY()`, `IS NULL`).

#### Scenario: Only closure rows are written

- GIVEN closure selects a strict subset of rows in `employees`
- WHEN `employees.ndjson` is written
- THEN every line corresponds to a row satisfying the merged predicate
- AND no out-of-closure row appears

#### Scenario: Empty included table still gets a file

- GIVEN an included table has zero matching rows after filtering
- WHEN the subset dump completes
- THEN the table appears in metadata
- AND its `.ndjson` file is present with no row objects

### Requirement: Subset Configuration API

The dump entry point MUST remain full-schema by default. Callers MUST enable subset mode via explicit configuration (for example `WithSubset(cfg)`). Unconfigured dumps MUST behave as today.

#### Scenario: Default dump is full

- GIVEN no subset configuration
- WHEN `Dump` runs
- THEN all discovered public tables are exported
- AND no subset manifest is written

#### Scenario: Subset configuration restricts export

- GIVEN valid subset configuration with seeds
- WHEN `Dump` runs
- THEN only closure-included tables are exported
- AND streaming uses subset predicates

### Requirement: Closure Integrity

The system MUST fail the dump if closure planning requires a referenced parent row whose table is not included in the export plan.

#### Scenario: Missing parent table fails fast

- GIVEN an included child references a parent table excluded by limits
- WHEN the planner detects the inconsistency
- THEN the dump returns an error describing the missing parent context
- AND incomplete output is not promoted as complete

### Requirement: MVP Schema Limitations

The v1 subset capability MUST document and enforce: public schema only; single-column primary keys for seed and traversal keys; composite or multi-column PK closure is out of scope.

#### Scenario: Composite primary key rejected

- GIVEN a table has a multi-column primary key
- WHEN seeds target that table
- THEN validation fails with a clear unsupported-PK error

### Requirement: CLI Subset Flags

The `dolly dump` command MUST accept `--seed-file` and documented limit flags that map to subset configuration. Help text MUST state that omitting `--seed-file` performs a full dump.

#### Scenario: Seed file triggers subset dump

- GIVEN `dolly dump --seed-file seeds.json` with valid seeds and connection
- WHEN the command runs
- THEN dump invokes subset mode with seeds from the file
- AND limit flags apply to the same configuration object

#### Scenario: Missing seed file is full dump

- GIVEN `dolly dump` without `--seed-file`
- WHEN the command runs
- THEN dump runs in full-schema mode
- AND no subset manifest is written

### Requirement: Integration Validation on Fixture DAG

When integration tests are enabled, the system MUST prove subset dumps on the shared fixture export expected tables only, honor limits, and remain restorable via the existing restore path without restore-code changes.

#### Scenario: Seed dump exports expected tables only

- GIVEN integration fixtures with `departments` → `employees` FK graph
- WHEN a subset dump seeds one `employees` row
- THEN output includes `departments` and `employees` (and other closure tables if seeded)
- AND tables outside the closure have no `.ndjson` files

#### Scenario: Subset dump restores on empty database

- GIVEN a successful subset dump from the fixture
- WHEN existing restore runs against an empty database
- THEN restore completes without error
- AND restored rows match the exported closure
