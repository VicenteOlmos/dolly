# postgres-integration-testing Specification

## Purpose
TBD - created by archiving change dolly-postgres-integration-tests. Update Purpose after archive.
## Requirements
### Requirement: Integration Build Tag Gate

Integration test sources MUST use the `integration` build tag so they are excluded from the default test build.

#### Scenario: Default test run excludes integration files

- GIVEN integration tests live in `*_integration_test.go` with `//go:build integration`
- WHEN `go test ./...` runs without tags
- THEN integration test files are not compiled
- AND no TCP connection to PostgreSQL is attempted

#### Scenario: Integration tests compile only when opted in

- GIVEN the same integration test files
- WHEN `go test -tags=integration` runs on packages that contain them
- THEN integration test files are compiled and eligible to run

### Requirement: DSN Environment Gate

Integration tests MUST read a canonical DSN from `DOLLY_TEST_PG_DSN` and MUST skip (not fail) when the variable is unset or the database is unreachable.

#### Scenario: Unset DSN skips with reason

- GIVEN `DOLLY_TEST_PG_DSN` is unset or empty
- WHEN an integration test starts
- THEN the test calls `t.Skip` with a message naming the required variable
- AND no fatal error is returned to the default test runner

#### Scenario: Unreachable database skips with reason

- GIVEN `DOLLY_TEST_PG_DSN` is set to a value that cannot be opened or pinged
- WHEN the shared helper opens the connection
- THEN the test skips with a message indicating the database is unreachable
- AND other packages' default tests remain unaffected

### Requirement: Shared Integration Helper

The project MUST provide a shared helper (e.g. `internal/testutil/pgintegration`) used only from integration-tagged tests to open a database and apply fixtures.

#### Scenario: Helper opens from DSN

- GIVEN `DOLLY_TEST_PG_DSN` points at a reachable PostgreSQL database
- WHEN a test calls the shared open helper
- THEN it returns a `*sql.DB` connected via the project's PostgreSQL driver
- AND the connection is closed when the test completes

#### Scenario: Helper applies idempotent fixture DDL

- GIVEN a reachable test database
- WHEN the helper bootstraps fixtures
- THEN it creates or reuses `public` tables `departments`, `employees`, and `empty_audit` with `CREATE TABLE IF NOT EXISTS`
- AND seeded data is reset in a way safe for repeated runs (e.g. `TRUNCATE` or equivalent)

### Requirement: Fixture Schema Shape

The integration fixture MUST model a minimal public schema sufficient for introspection and dump scenarios.

#### Scenario: Parent and child tables with foreign key

- GIVEN fixture bootstrap completed
- WHEN the `public` schema is inspected
- THEN `departments` exists with a primary key on `id`
- AND `employees` exists with a foreign key referencing `departments`
- AND at least one nullable column exists on `employees`

#### Scenario: Zero-row table exists

- GIVEN fixture bootstrap completed
- WHEN the `public` schema is inspected
- THEN `empty_audit` exists as a base table with zero rows

### Requirement: Contributor Documentation

The change MUST document how to run integration tests locally without making them the default verify gate.

#### Scenario: Documented local workflow

- GIVEN a contributor wants live-Postgres coverage
- WHEN they follow project documentation
- THEN they can set `DOLLY_TEST_PG_DSN`, optionally start Postgres (e.g. Docker Compose), and run `go test -tags=integration` on `internal/db` and `internal/dump`
- AND default `go test ./...` remains documented as not requiring PostgreSQL

### Requirement: Harness Boundary

The integration harness MUST NOT add production code paths, MUST NOT replace sqlmock unit tests, and MUST NOT require testcontainers in the first delivery slice.

#### Scenario: Production packages unchanged

- GIVEN integration tests are added
- WHEN default unit tests run
- THEN existing sqlmock-based tests in `internal/db` and `internal/dump` behave as before
- AND no new runtime dependency is required for non-integration builds

