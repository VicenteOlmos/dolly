# Integration DSN Note: dolly-subset-dag

## Status

Live PostgreSQL integration tests for `dolly-subset-dag` are **intentionally gated by environment** and were skipped during verification because `DOLLY_TEST_PG_DSN` is unset.

## Why this is justified

- The project design decision is: default `go test ./...` must not require PostgreSQL.
- Integration tests live behind `//go:build integration` and skip cleanly with a clear message when `DOLLY_TEST_PG_DSN` is missing.
- All subset-DAG behaviors have passing unit-test coverage (planner, stream, predicate, CLI parse, CLI seam).
- The skip path itself is verified: `env -u DOLLY_TEST_PG_DSN go test -p 1 -tags=integration ./... -count=1` passes.

## How to run live integration tests

```bash
export DOLLY_TEST_PG_DSN="postgres://user:password@localhost:5432/dbname?sslmode=disable"
go test -p 1 -tags=integration ./internal/dump/... -count=1 -v
```

## What live integration tests cover

- `TestIntegrationSubsetDumpEmployeeSeed` — seed one employee row, verify closure includes departments and employees, excludes empty_audit.
- `TestIntegrationSubsetDumpMaxTablesExceeded` — limit error with no promoted metadata.
- `TestIntegrationDumpArtifacts`, `TestIntegrationDumpMetadataAndRows`, etc. — full dump behavior on fixture DAG.

## Artifact

This note was created during post-fix warning cleanup (2026-06-01).
