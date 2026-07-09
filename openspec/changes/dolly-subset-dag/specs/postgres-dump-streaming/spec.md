# Delta for postgres-dump-streaming

## ADDED Requirements

### Requirement: Full and subset dump coexistence

When subset configuration is absent, `Dump` MUST satisfy all existing streaming requirements unchanged. When subset configuration is present, `Dump` MUST delegate closure planning and row filtering to the `postgres-dump-subset` capability while preserving NDJSON format, dependency ordering on the included subgraph, snapshot semantics, atomic completion, and error visibility from this capability.

#### Scenario: Full dump unchanged without subset config

- GIVEN a caller invokes `Dump` with default options and no subset configuration
- WHEN the dump completes
- THEN every discovered public table is exported
- AND behavior matches pre-subset `postgres-dump-streaming` requirements

#### Scenario: Subset dump preserves streaming contracts

- GIVEN valid subset configuration
- WHEN `Dump` completes successfully
- THEN included tables use NDJSON row streaming with bounded memory
- AND `metadata.json` is written only after included data files are complete
- AND failures surface with table or operation context

#### Scenario: Subset limits surface as dump errors

- GIVEN subset configuration that exceeds `MaxTables` or related caps
- WHEN planning runs before streaming
- THEN `Dump` returns a non-nil error
- AND incomplete output is not promoted as complete

### Requirement: Subset manifest in metadata

When a subset dump completes, `metadata.json` MUST include a `subset` manifest section recording applied seeds, limit caps, included table names, and per-table exported row counts. Full dumps MUST omit the `subset` section.

#### Scenario: Full dump omits subset manifest

- GIVEN a successful full-schema dump
- WHEN `metadata.json` is decoded
- THEN no `subset` key is present
- AND existing table, column, and foreign-key fields remain populated

#### Scenario: Subset dump records seeds and limits

- GIVEN a successful subset dump with seeds and configured limits
- WHEN `metadata.json` is decoded
- THEN a `subset` object is present
- AND it records seed predicates, applied limit values, included tables, and row counts per included table

#### Scenario: Subset metadata still lists included schema

- GIVEN a subset dump including `employees` and `departments`
- WHEN `metadata.json` is decoded
- THEN metadata entries exist only for included tables
- AND each entry retains columns and foreign keys needed by restore

## MODIFIED Requirements

### Requirement: Dump Artifact Generation

The system MUST dump PostgreSQL public schema data into an output directory containing `metadata.json` and one `.ndjson` file per dumped table. For full dumps, every discovered public table MUST be dumped. For subset dumps, only tables in the referential closure plan MUST receive `.ndjson` files.

(Previously: every discovered public table always received a data file.)

#### Scenario: Complete dump artifacts are produced

- GIVEN a PostgreSQL database with public tables
- WHEN a full dump is requested for an output directory
- THEN the output contains `metadata.json`
- AND each discovered table has a corresponding `.ndjson` data file

#### Scenario: Subset dump artifacts are produced for closure only

- GIVEN a subset dump whose closure includes `departments` and `employees` but not `empty_audit`
- WHEN the dump completes
- THEN the output contains `metadata.json`
- AND `.ndjson` files exist only for included tables
- AND `empty_audit.ndjson` is not present

#### Scenario: Empty tables are represented

- GIVEN a discovered public table with no rows (full dump) or an included table with zero matching rows (subset dump)
- WHEN the dump completes
- THEN the table appears in `metadata.json`
- AND its `.ndjson` file is present and contains no row objects

### Requirement: Metadata Descriptor

The system MUST generate `metadata.json` from `internal/db` schema structures, preserving table, column, row-count, and foreign-key metadata needed by later phases. When subset mode is active, metadata MUST additionally satisfy the subset manifest requirements defined in this change.

(Previously: metadata described all discovered tables with schema fields only.)

#### Scenario: Schema metadata is written

- GIVEN loaded public schema metadata
- WHEN `metadata.json` is generated
- THEN it includes dump generation time and all dumped tables (all discovered tables for full dumps; closure tables for subset dumps)
- AND each table includes columns and foreign keys from the schema metadata

#### Scenario: Metadata order is deterministic

- GIVEN the same schema and dump mode across repeated dumps
- WHEN metadata is generated
- THEN table, column, and foreign-key metadata order is stable

#### Scenario: Subset manifest accompanies subset metadata

- GIVEN a successful subset dump
- WHEN `metadata.json` is generated
- THEN the subset manifest section is present alongside per-table schema metadata for included tables only
