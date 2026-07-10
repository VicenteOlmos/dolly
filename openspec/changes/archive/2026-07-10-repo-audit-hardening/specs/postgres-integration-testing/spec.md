# Delta for postgres-integration-testing

## ADDED Requirements

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
