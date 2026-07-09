# PostgreSQL Restore Streaming Specification

## Purpose

Define how `internal/restore` loads PostgreSQL `public` schema row data from dump artifacts (`metadata.json` and per-table `.ndjson` files) into an existing database without loading entire tables into memory or performing DDL.

## Requirements

### Requirement: Restore Artifact Input

The system MUST accept a dump output directory containing `metadata.json` and one `.ndjson` file per table listed in metadata.

#### Scenario: Complete artifact set is accepted

- GIVEN a directory produced by `postgres-dump-streaming` with `metadata.json` and per-table `.ndjson` files
- WHEN restore is requested for that input directory
- THEN `metadata.json` is read and decoded
- AND each table in metadata has a corresponding `.ndjson` file present

#### Scenario: Missing metadata or data file fails early

- GIVEN an input directory without `metadata.json` or missing a `.ndjson` for a table listed in metadata
- WHEN restore starts
- THEN restore returns an error identifying the missing artifact
- AND no row inserts are attempted

#### Scenario: Empty table file is valid

- GIVEN a table with zero rows in metadata and an empty or whitespace-only `.ndjson` file
- WHEN restore processes that table
- THEN no rows are inserted for that table
- AND restore continues with remaining tables

### Requirement: Metadata Contract

The system MUST treat `metadata.json` as the authoritative contract between dump and restore, using the same table, column, and foreign-key structures defined by `postgres-dump-streaming`.

#### Scenario: Metadata drives table schedule

- GIVEN decoded metadata with a `tables` array in dump order
- WHEN restore loads rows
- THEN tables are processed in `metadata.Tables` order
- AND each table's column list from metadata defines the insert column set

#### Scenario: Schema field scopes restore target

- GIVEN metadata declares `schema` (today `public`)
- WHEN restore builds identifiers and validates the target
- THEN only that schema's tables are targeted for insert and validation

#### Scenario: Provenance fields are non-blocking

- GIVEN `generated_at` or approximate `row_count` in metadata
- WHEN restore runs
- THEN those fields MAY be recorded for logging
- AND restore MUST NOT fail solely because counts differ from live table statistics

### Requirement: Foreign-Key-Safe Insert Order

The system MUST insert rows in an order that respects foreign-key dependencies when metadata reflects dump dependency order (referenced tables before dependents).

#### Scenario: Parent rows precede child rows

- GIVEN metadata lists a parent table before a child that references it
- WHEN restore inserts rows
- THEN all parent-table rows for a dependency edge are inserted before child-table rows that reference them

#### Scenario: Cyclic graphs use deterministic metadata order

- GIVEN tables with cyclic foreign-key dependencies still present in metadata order
- WHEN restore runs with default conflict policy into a database that enforces FKs at insert time
- THEN restore attempts inserts in metadata order
- AND failure due to unresolved cycles MUST surface as an insert or constraint error with table context

### Requirement: Target Schema Validation

The system MUST validate the target database against metadata using live public-schema introspection before the first insert.

#### Scenario: Compatible schema passes validation

- GIVEN a target database whose public tables match metadata table names, column names, column count, `data_type`, `is_nullable`, and `primary_key` flags
- WHEN restore validates the target
- THEN validation succeeds
- AND row loading proceeds

#### Scenario: Missing table fails before insert

- GIVEN metadata lists a table absent from the target public schema
- WHEN validation runs
- THEN restore returns an error naming the missing table
- AND no rows are inserted

#### Scenario: Column mismatch fails with context

- GIVEN a table exists but a column differs in name, type, nullability, or primary-key flag from metadata
- WHEN validation runs
- THEN restore returns an error identifying the table and column
- AND no rows are inserted for that restore run

#### Scenario: Extra target-only columns are allowed

- GIVEN the target has columns not present in metadata for a table
- WHEN validation runs with default rules
- THEN validation succeeds
- AND restore inserts only columns declared in metadata

#### Scenario: NDJSON keys must match metadata columns

- GIVEN a non-empty NDJSON line for a table
- WHEN the line is loaded
- THEN object keys MUST match metadata column names for that table
- AND unknown keys or missing required non-nullable columns without defaults MUST fail the restore with row/table context

### Requirement: Row Streaming and Bounded Memory

The system MUST read each `.ndjson` file incrementally and MUST NOT require loading an entire table into memory.

#### Scenario: Rows are loaded line by line

- GIVEN a table with many NDJSON lines
- WHEN that table is restored
- THEN each line is parsed as one JSON object keyed by column name
- AND memory use does not grow with total row count of the table

#### Scenario: Primary key values are preserved

- GIVEN dumped rows include explicit primary-key column values
- WHEN restore inserts with default options
- THEN inserted rows use the values from the artifact
- AND foreign-key references among restored tables remain consistent

### Requirement: Conflict Policies

The system MUST support configurable row-level conflict handling: `error` (default), `skip`, `upsert`, and table-level `replace` (truncate then insert).

#### Scenario: Default error on duplicate key

- GIVEN conflict policy `error` and a target row with the same primary key as a dumped row
- WHEN restore inserts that row
- THEN restore fails with constraint or conflict context
- AND the active transaction is rolled back when transactional mode is enabled

#### Scenario: Skip ignores conflicting rows

- GIVEN conflict policy `skip`
- WHEN a row conflicts on primary key or unique constraint
- THEN that row is not inserted
- AND restore continues with remaining rows

#### Scenario: Upsert updates non-key columns

- GIVEN conflict policy `upsert` and a composite or single-column primary key from metadata
- WHEN a row conflicts on that key
- THEN the existing row is updated for non-primary-key columns present in the insert
- AND non-conflicting rows are inserted normally

#### Scenario: Replace truncates then loads

- GIVEN conflict policy `replace`
- WHEN restore begins loading a table's rows
- THEN target table data is removed using truncate semantics that respect foreign-key direction (children before parents, or equivalent CASCADE)
- AND subsequent inserts load all rows from the artifact for that table

#### Scenario: Replace is destructive and opt-in

- GIVEN conflict policy `replace`
- WHEN restore runs
- THEN pre-existing rows in affected target tables are removed before insert
- AND operators MUST select this policy explicitly (not the default)

### Requirement: Transactional Atomicity

The system MUST wrap restore in a single read/write transaction by default and MAY allow per-table commits when opted out.

#### Scenario: Default single-transaction restore

- GIVEN default options and a failure mid-restore
- WHEN any table load or validation fails
- THEN no partial commits from that restore run remain visible
- AND the database state is unchanged from before the restore began

#### Scenario: Successful transactional restore commits once

- GIVEN default transactional mode and all tables load successfully
- WHEN restore completes
- THEN all inserted rows are committed together

#### Scenario: Per-table commits when opted out

- GIVEN the caller opts out of a single wrapping transaction
- WHEN restore processes multiple tables
- THEN each table's loads MAY commit independently
- AND a later failure leaves earlier tables' inserts visible

#### Scenario: Replace and truncate share transactional boundary

- GIVEN conflict policy `replace` and default transactional mode
- WHEN restore runs
- THEN truncate and insert for affected tables occur within the same transaction as other tables unless per-table mode is enabled

### Requirement: No DDL

The system MUST NOT create, alter, or drop database objects as part of restore.

#### Scenario: Target schema must pre-exist

- GIVEN dump artifacts and an empty database without matching tables
- WHEN restore runs
- THEN validation fails for missing tables
- AND restore does not create tables, indexes, or constraints

#### Scenario: Restore is a library capability without DDL

- GIVEN this capability is implemented
- WHEN callers use `internal/restore`
- THEN restore is exposed as a Go package API
- AND restore does not perform DDL, compression, encryption, remote storage, parallel restore, or non-PostgreSQL support
- AND the `dolly restore` CLI subcommand (see `dolly-cli`) delegates to this package without changing artifact formats

### Requirement: Dump Round-Trip Fidelity

When integration tests are enabled, the system MUST validate restore against artifacts produced from the shared PostgreSQL fixture via dump.

#### Scenario: Dump then restore into empty fixture-shaped database

- GIVEN integration tests with `-tags=integration`, reachable `DOLLY_TEST_PG_DSN`, fixtures applied, and a successful dump to a temporary directory
- WHEN restore runs into a second empty database with matching schema and default `error` policy
- THEN restore completes without error
- AND row counts and representative primary-key values match the source fixture for dumped tables

#### Scenario: Schema mismatch fails integration round-trip pre-insert

- GIVEN dump artifacts and a target database with an altered column type
- WHEN restore runs with default validation
- THEN restore fails before inserting rows
- AND the error identifies the incompatible table or column

### Requirement: Restore Failure Visibility

The system MUST surface restore failures with enough context to diagnose the failing phase, table, or row.

#### Scenario: Validation failure is reported before inserts

- GIVEN incompatible target schema
- WHEN restore runs
- THEN the returned error describes validation failure
- AND no rows from that run are committed in default transactional mode

#### Scenario: Insert failure identifies table

- GIVEN a reachable database and valid metadata
- WHEN a row insert violates constraints under policy `error`
- THEN restore returns an error identifying the table and failure class
- AND transactional restore rolls back the whole run

#### Scenario: Cancellation aborts restore

- GIVEN a cancelled context during NDJSON streaming
- WHEN restore observes cancellation
- THEN restore returns a cancellation error
- AND default transactional mode leaves the database unchanged

### Requirement: Optional restore progress observer

The restore capability MAY accept `WithProgress(fn)` to observe table-level restore progress. Each `ProgressEvent` SHALL carry `Phase`, `Table`, `Current`, `Total`, and `Elapsed`; one event MUST be emitted per table boundary in metadata order. When unset, restore behavior, artifacts, validation, transaction, and error semantics MUST remain unchanged.

#### Scenario: Default restore remains silent

- GIVEN restore is invoked without `WithProgress`
- WHEN restore completes successfully
- THEN row loading and transaction behavior match existing restore requirements
- AND no progress callback is invoked

#### Scenario: Observer receives table progress

- GIVEN restore is invoked with `WithProgress`
- WHEN metadata lists multiple tables
- THEN the callback receives one event per processed table in metadata order
- AND `Current` increases from 1 through `Total` with elapsed time populated

#### Scenario: Failure preserves restore semantics

- GIVEN restore fails during validation, insert, or cancellation
- WHEN progress was configured
- THEN the returned error and transactional outcome follow existing restore requirements
- AND progress events do not imply successful commit
