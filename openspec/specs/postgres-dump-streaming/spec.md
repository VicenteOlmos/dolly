# PostgreSQL Dump Streaming Specification

## Purpose

Define how `internal/dump` exports PostgreSQL public schema metadata and table rows into portable files without loading entire tables into memory.
## Requirements
### Requirement: Dump Artifact Generation

The system MUST dump PostgreSQL public schema data into an output directory containing `metadata.json` and one `.ndjson` file per dumped table.

#### Scenario: Complete dump artifacts are produced

- GIVEN a PostgreSQL database with public tables
- WHEN a dump is requested for an output directory
- THEN the output contains `metadata.json`
- AND each discovered table has a corresponding `.ndjson` data file

#### Scenario: Empty tables are represented

- GIVEN a discovered public table with no rows
- WHEN the dump completes
- THEN the table appears in `metadata.json`
- AND its `.ndjson` file is present and contains no row objects

### Requirement: Metadata Descriptor

The system MUST generate `metadata.json` from `internal/db` schema structures, preserving table, column, row-count, and foreign-key metadata needed by later phases.

#### Scenario: Schema metadata is written

- GIVEN loaded public schema metadata
- WHEN `metadata.json` is generated
- THEN it includes dump generation time and all discovered tables
- AND each table includes columns and foreign keys from the schema metadata

#### Scenario: Metadata order is deterministic

- GIVEN the same schema across repeated dumps
- WHEN metadata is generated
- THEN table, column, and foreign-key metadata order is stable

### Requirement: NDJSON Row Streaming

The system MUST write each table as newline-delimited JSON with bounded memory usage independent of table row count. Row data MAY be transformed before serialization when a `RowTransform` is configured.

#### Scenario: Rows are streamed as JSON objects

- GIVEN a table with rows and columns
- WHEN the table is dumped
- THEN each output line is a valid JSON object
- AND each object is keyed by column name
- AND transformed values obey the same contract

#### Scenario: Large tables do not require table-sized memory

- GIVEN a table with many rows
- WHEN the table is dumped
- THEN rows are processed incrementally
- AND the dump does not retain the full table in memory
- AND the transform does not accumulate state across rows

### Requirement: Optional row-transform hook

The system SHOULD accept an optional `RowTransform` callback applied to each row before JSON serialization. When unset, row data MUST be written without mutation.

#### Scenario: Transform mutates row values

- GIVEN a `RowTransform` that replaces column `email` values
- WHEN `streamTable` processes rows
- THEN the NDJSON output contains the transformed value
- AND the original value is not written

#### Scenario: Disabled transform is passthrough

- GIVEN no `RowTransform` is configured
- WHEN `streamTable` processes rows
- THEN the NDJSON output matches pre-change behavior byte-for-byte

#### Scenario: Transform preserves row count and column set

- GIVEN a `RowTransform` is configured
- WHEN a row passes through the transform
- THEN the NDJSON output has the same number of rows
- AND each row has the same column keys as the original

### Requirement: Built-in column-pattern sanitizer

When `sanitization.enabled` is true in config, the system SHOULD apply a built-in transform that replaces text values in columns matching known sensitive patterns.

#### Scenario: Known pattern columns are replaced

- GIVEN a column named `email`, `password`, `ssn`, `phone`, `credit_card`, `secret`, or `token`
- WHEN the column type is text/varchar
- THEN the NDJSON value is replaced with `[SANITIZED]` (or `redacted@example.com` for email, `000-00-0000` for ssn)
- AND non-text columns with those names are left unchanged

#### Scenario: Unmatched columns are passthrough

- GIVEN a column named `name` or `description` or any non-sensitive name
- WHEN the transform runs
- THEN the value is written unchanged regardless of data type

### Requirement: Dependency-Safe Table Order

The system MUST dump tables in foreign-key dependency order, with referenced tables before dependent tables when the dependency graph permits it.

#### Scenario: Parent tables precede child tables

- GIVEN a child table references a parent table
- WHEN dump order is determined
- THEN the parent table is scheduled before the child table

#### Scenario: Cyclic dependencies remain dumpable

- GIVEN tables with cyclic foreign-key dependencies
- WHEN dump order is determined
- THEN every table is still dumped exactly once
- AND the chosen order is deterministic

### Requirement: Snapshot Consistency

The system MUST use one consistent read snapshot for schema metadata and row data by default, and MAY allow callers to opt out.

#### Scenario: Default dump uses a consistent snapshot

- GIVEN concurrent database writes occur during a dump
- WHEN the default dump completes
- THEN metadata and data reflect one consistent read view

#### Scenario: Snapshot consistency can be disabled

- GIVEN a caller explicitly opts out of snapshot consistency
- WHEN the dump runs
- THEN the system may read tables without a single shared snapshot

### Requirement: Atomic Completion and Error Visibility

The system MUST avoid presenting partial files as completed output and MUST surface dump failures with useful context.

#### Scenario: Successful dump is signaled last

- GIVEN all table files are written successfully
- WHEN the dump completes
- THEN `metadata.json` is written only after data files are complete

#### Scenario: Failed dump reports context

- GIVEN schema loading, row reading, writing, cancellation, or closing fails
- WHEN the failure occurs
- THEN the dump returns an error identifying the failed operation or table
- AND incomplete table output is not promoted as complete

### Requirement: Optional dump progress observer

The dump capability MAY accept an optional progress callback invoked once per table boundary during `Dump`. Each `ProgressEvent` SHALL carry `Phase`, `Table`, `Current`, `Total`, and `Elapsed`; `Current` MUST increase monotonically from 1 through `Total`, and `Total` MUST equal the number of tables scheduled from metadata. When unset, behavior MUST match the default: no observer invocations and unchanged artifacts. The callback MUST NOT alter artifact layout, dependency order, snapshot semantics, atomic completion rules, or default error behavior.

#### Scenario: Default dump without observer

- GIVEN a caller invokes `Dump` without a progress callback
- WHEN the dump completes successfully
- THEN artifacts match existing dump-output requirements
- AND no progress callback is invoked

#### Scenario: Observer receives numeric table progress

- GIVEN a caller configures a progress callback before `Dump`
- WHEN tables are exported in dependency order
- THEN the callback is invoked once per table boundary with table name, elapsed time, current index, and total table count
- AND `Current` values are monotonic and never exceed `Total`

#### Scenario: Observer behavior does not change dump contract

- GIVEN a progress callback is configured
- WHEN dump succeeds or fails for reasons unrelated to the observer
- THEN dump success and failure semantics remain governed by existing dump requirements
- AND observer behavior MUST NOT weaken atomic completion or error visibility rules

### Requirement: Capability Boundary

The system MUST NOT implement CLI behavior, TUI behavior, compression, encryption, remote storage, parallel dumping, or non-PostgreSQL database support as part of this capability.

#### Scenario: Dump remains a package capability

- GIVEN this capability is implemented
- WHEN callers use it
- THEN it exposes package-level dump behavior only
- AND it does not add user-interface or remote-target behavior

### Requirement: Integration Validation of Dump Artifacts

When integration tests are enabled, the system MUST validate `Dump` end-to-end against the shared PostgreSQL fixture.

#### Scenario: Complete artifact set on fixture database

- GIVEN integration tests run with `-tags=integration`, reachable `DOLLY_TEST_PG_DSN`, and fixtures applied
- WHEN `Dump` writes to a temporary output directory with default options
- THEN `metadata.json` exists
- AND `.ndjson` files exist for `departments`, `employees`, and `empty_audit`

#### Scenario: Metadata reflects fixture schema

- GIVEN a successful integration dump
- WHEN `metadata.json` is decoded
- THEN it lists all fixture tables with columns and foreign keys consistent with `LoadPostgresPublicSchema` on the same database
- AND generation timestamp is present

### Requirement: Integration Validation of NDJSON Row Content

When integration tests are enabled, the system MUST validate streamed row JSON against seeded fixture data.

#### Scenario: Seeded rows decode as column-keyed objects

- GIVEN `departments` and `employees` contain seeded rows from fixture bootstrap
- WHEN their `.ndjson` files are read line by line
- THEN each non-empty line is valid JSON object keyed by column name
- AND at least one line per non-empty table contains expected seed values for primary identifiers

#### Scenario: Empty table file has no row objects

- GIVEN `empty_audit` has zero rows
- WHEN the dump completes
- THEN `empty_audit` appears in `metadata.json`
- AND `empty_audit.ndjson` is present with no JSON row objects (empty or whitespace-only per implementation)

### Requirement: Integration Validation of Default Snapshot

When integration tests are enabled, the system MUST run at least one dump scenario using default snapshot consistency against live Postgres.

#### Scenario: Default dump succeeds on fixture data

- GIVEN concurrent writes are not required for the assertion
- WHEN `Dump` runs with default options (snapshot enabled)
- THEN the dump completes without error
- AND metadata and data files are written for all fixture tables

### Requirement: Integration Validation of Dependency Order

When integration tests are enabled, the system MUST assert dump processing order respects the fixture foreign-key graph when observable.

#### Scenario: Parent table precedes child in dump order

- GIVEN `employees` references `departments`
- WHEN integration tests observe table dump order (via metadata table order or documented ordering hook)
- THEN `departments` is ordered before `employees` when the dependency graph permits

### Requirement: Integration Dump Failure Visibility

When integration tests are enabled, the suite MUST include at least one scenario proving dump failure is surfaced against an invalid output or database condition without duplicating all sqlmock cases.

#### Scenario: Dump to invalid path returns error

- GIVEN a valid integration database connection
- WHEN `Dump` targets a non-writable or invalid output path
- THEN it returns a non-nil error identifying the failure context
- AND incomplete output is not treated as success

