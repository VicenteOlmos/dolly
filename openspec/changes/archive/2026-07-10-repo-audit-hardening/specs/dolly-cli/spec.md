# Delta for dolly-cli

## ADDED Requirements

### Requirement: Destructive-operation stderr guardrails

Before clone or restore begins side effects, the CLI MUST emit at most one stderr line per guardrail concern using established `warning:` and `info:` prefixes. Guardrail output MUST NOT change exit codes, MUST NOT bypass existing confirmation gates, and MUST NOT require a live database to be asserted in unit tests.

When `dolly clone` runs with sanitization disabled in config OR a strategy that never sanitizes (`template`, `logical-stream`, `physical-backup`), the CLI MUST emit a `warning:` that the clone is unsanitized.

When `dolly clone` runs with skip-create enabled, the CLI MUST emit a `warning:` that failure may leave partial state on an existing target database.

When `dolly restore --replace` or `dolly clone` with replace enabled proceeds after the operator has supplied required confirmation (`--yes`), the CLI MUST emit `info: target database:` naming the target database before truncate or replace side effects begin.

#### Scenario: Unsanitized clone warns on stderr

- GIVEN sanitization is disabled or the selected clone strategy never sanitizes
- WHEN `dolly clone` begins execution after preflight passes
- THEN stderr contains a `warning:` about unsanitized data
- AND at most one unsanitized warning line is emitted

#### Scenario: Skip-create warns on partial-state risk

- GIVEN skip-create is enabled for a clone run
- WHEN `dolly clone` begins execution after preflight passes
- THEN stderr contains a `warning:` about partial-state risk on an existing target
- AND at most one skip-create warning line is emitted

#### Scenario: Confirmed replace restore names target database

- GIVEN `dolly restore` is invoked with `--replace` and `--yes`
- WHEN restore is about to perform truncate or replace side effects
- THEN stderr contains `info: target database:` with the target database name
- AND the line appears before those side effects begin

#### Scenario: Confirmed replace clone names target database

- GIVEN `dolly clone` runs with replace enabled and required confirmation supplied
- WHEN clone is about to perform truncate or replace side effects
- THEN stderr contains `info: target database:` with the target database name
- AND the line appears before those side effects begin

### Requirement: Unchanged destructive confirmation gates

Adding stderr guardrails MUST NOT weaken existing destructive-operation gates. `dolly restore --replace` MUST continue to require explicit `--yes` before proceeding. `dolly clone` with replace enabled MUST continue to require explicit confirmation equivalent to the existing replace gate before proceeding.

#### Scenario: Replace restore without yes still rejected

- GIVEN `dolly restore` is invoked with `--replace` and without `--yes`
- WHEN flag validation completes
- THEN the process exits with status **1**
- AND stderr explains that `--yes` is required
- AND no restore side effects begin

#### Scenario: Replace clone without yes still rejected

- GIVEN `dolly clone` is invoked with replace enabled and without required confirmation
- WHEN validation completes before destructive work
- THEN the process exits with status **1**
- AND stderr explains the missing confirmation
- AND no clone side effects begin

### Requirement: Table-driven guardrail CLI tests

Clone and restore guardrail stderr behavior MUST be verifiable through table-driven unit tests that exercise flag combinations in isolation without a live PostgreSQL instance.

#### Scenario: Table tests cover guardrail substrings

- GIVEN table-driven tests for clone and restore guardrail cases
- WHEN tests run without `DOLLY_TEST_PG_DSN` or database connectivity
- THEN stderr output is asserted for unsanitized warning, skip-create warning, and target-database info substrings per scenario
- AND existing `--yes` gate assertions remain covered

#### Scenario: Guardrail tests do not require PostgreSQL

- GIVEN only default unit test build (no `integration` tag)
- WHEN guardrail table tests run
- THEN no TCP connection to PostgreSQL is attempted
