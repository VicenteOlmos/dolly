# PostgreSQL Schema Introspection Specification

## Purpose

Define the behavior for loading PostgreSQL `public` schema metadata into `internal/db` models for later dump and transform phases.
## Requirements
### Requirement: Schema Model Metadata

The system MUST represent public schema tables, columns, and foreign keys with stable metadata sufficient for future dump and transform phases.

#### Scenario: Table metadata is represented

- GIVEN a discovered public table
- WHEN schema metadata is returned
- THEN the table includes schema, table name, and a row-count field
- AND the row-count field can represent either a known count or an unavailable count

#### Scenario: Column and foreign-key metadata is represented

- GIVEN a discovered table with columns and foreign keys
- WHEN schema metadata is returned
- THEN each column includes name, type, nullability, primary-key flag, and ordinal position
- AND each foreign key includes constraint name, source table/column, and referenced schema/table/column

### Requirement: Public Schema Loading

The system MUST load all PostgreSQL base tables in the `public` schema from a `*sql.DB` connection.

#### Scenario: Public tables are loaded

- GIVEN a valid PostgreSQL connection with public tables
- WHEN public schema introspection runs
- THEN every public base table is returned with its columns and foreign keys

#### Scenario: Non-public and non-table objects are excluded

- GIVEN schemas or objects outside public base tables
- WHEN public schema introspection runs
- THEN they are not included in the returned schema metadata

### Requirement: Deterministic Metadata Order

The system MUST preserve deterministic ordering where metadata order affects future output.

#### Scenario: Stable table and column order

- GIVEN the same database schema across repeated runs
- WHEN public schema introspection runs
- THEN table order is stable
- AND column order follows each column ordinal position

#### Scenario: Stable foreign-key order

- GIVEN multiple foreign keys for a table
- WHEN metadata is returned for that table
- THEN foreign keys are ordered deterministically by stable constraint/source/reference metadata

### Requirement: Error Visibility

The system MUST surface query, scan, and row-iteration errors with useful operation context.

#### Scenario: Query or scan fails

- GIVEN a database query or row scan fails during introspection
- WHEN the failure occurs
- THEN introspection returns an error instead of partial successful metadata
- AND the error identifies the failed phase or table context

#### Scenario: Row iteration fails after partial reads

- GIVEN row iteration reports an error after `Next` stops
- WHEN introspection completes the loop
- THEN the iteration error is returned to the caller

### Requirement: Capability Boundary

The system MUST NOT implement UI, dump writing, sanitization, clone behavior, or non-PostgreSQL database support as part of this capability.

#### Scenario: Introspection remains bounded

- GIVEN this capability is implemented
- WHEN callers use it
- THEN it only returns PostgreSQL public schema metadata
- AND it does not perform TUI, MySQL, dump, transform, sanitization, or clone operations

### Requirement: Integration Validation of Public Schema Loading

When integration tests are enabled, the system MUST validate `LoadPostgresPublicSchema` against the shared PostgreSQL fixture.

#### Scenario: Fixture tables and columns are loaded

- GIVEN integration tests run with `-tags=integration` and a reachable database with fixtures applied
- WHEN `LoadPostgresPublicSchema` runs
- THEN `departments`, `employees`, and `empty_audit` are returned
- AND each table's columns include name, data type, nullability, primary-key flag, and ordinal position matching the fixture

#### Scenario: Foreign keys match fixture graph

- GIVEN the same integration setup
- WHEN `LoadPostgresPublicSchema` runs
- THEN `employees` includes a foreign key with constraint name, source column, and referenced `departments` column consistent with the fixture DDL
- AND non-public objects are not included

### Requirement: Integration Validation of Deterministic Order

When integration tests are enabled, the system MUST assert stable ordering across repeated loads against the same fixture database.

#### Scenario: Repeated loads produce stable FK order

- GIVEN two consecutive calls to `LoadPostgresPublicSchema` on the same fixture database without schema changes
- WHEN both results are compared
- THEN foreign-key ordering for `employees` is identical across runs
- AND column order follows ordinal position for each table

### Requirement: Integration Row-Count Policy

When integration tests are enabled, assertions on `RowCount` MUST tolerate `pg_stat_user_tables` approximations and MUST NOT require exact row counts unless the test explicitly seeds and analyzes with a documented tolerance.

#### Scenario: Row count is optional or non-exact

- GIVEN the fixture has known seeded rows in `departments` and `employees` and zero rows in `empty_audit`
- WHEN `LoadPostgresPublicSchema` runs in integration tests
- THEN tests MAY assert `RowCount` is nil or non-negative when present
- AND tests MUST NOT fail solely because `RowCount` differs from an exact `COUNT(*)` without an explicit tolerance policy

#### Scenario: Empty table row count does not break load

- GIVEN `empty_audit` has zero rows
- WHEN `LoadPostgresPublicSchema` runs
- THEN `empty_audit` is present in the result
- AND introspection completes without error

### Requirement: Integration Error Path Smoke

When integration tests are enabled, the suite MUST include at least one scenario proving introspection fails visibly against an invalid query context without replacing unit-test error coverage.

#### Scenario: Cancelled or closed connection surfaces error

- GIVEN a valid integration connection that is closed before introspection
- WHEN `LoadPostgresPublicSchema` runs
- THEN it returns a non-nil error
- AND the error is not masked as success

