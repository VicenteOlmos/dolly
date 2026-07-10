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

Integration tests MUST read a canonical DSN from `DOLLY_TEST_PG_DSN`. They MUST skip when the variable is unset and MUST fail when the variable is set but the database is unreachable.

#### Scenario: Unset DSN skips with reason

- GIVEN `DOLLY_TEST_PG_DSN` is unset or empty
- WHEN an integration test starts
- THEN the test calls `t.Skip` with a message naming the required variable
- AND no fatal error is returned to the default test runner

#### Scenario: Unreachable configured database fails with reason

- GIVEN `DOLLY_TEST_PG_DSN` is set to a value that cannot be opened or pinged
- WHEN the shared helper opens the connection
- THEN the test fails with a message indicating the database is unreachable
- AND release/CI jobs fail closed instead of publishing with skipped integration coverage

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

### Requirement: CI and release PostgreSQL 16 client tooling

Continuous integration and release workflows that run `go test -tags=integration` with `DOLLY_TEST_PG_DSN` set MUST install PostgreSQL 16 client tools (including `pg_dump` and `psql`) before the integration test command executes. The installed client major version MUST match the PostgreSQL 16 service used by those jobs so fail-closed client-version checks succeed instead of failing for a missing or mismatched client.

#### Scenario: CI postgres-integration installs client before tests

- GIVEN the CI workflow defines a `postgres-integration` job with `DOLLY_TEST_PG_DSN` set
- WHEN the job runs integration tests
- THEN PostgreSQL 16 client tools are installed after checkout and Go setup
- AND installation completes before `go test -tags=integration` runs

#### Scenario: Release workflow matches CI client install

- GIVEN the release workflow runs integration tests with `DOLLY_TEST_PG_DSN` set
- WHEN the job prepares to run `go test -tags=integration`
- THEN it installs PostgreSQL 16 client tools in the same relative position as CI
- AND release and CI use equivalent client-tool provisioning

#### Scenario: Missing client fails closed when DSN is set

- GIVEN `DOLLY_TEST_PG_DSN` is set in an integration job
- AND PostgreSQL 16 client tools are not available on the runner
- WHEN integration tests that require `pg_dump` major-version match run
- THEN the job fails with an actionable error
- AND the job does not pass with skipped integration coverage
