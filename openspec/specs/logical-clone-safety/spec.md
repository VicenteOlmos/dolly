# Logical Clone Safety Specification

## Requirements

### Requirement: Filtered Logical-Stream Enumeration

The logical clone MUST apply `SchemasFromOptions` to logical-stream enumeration. Dump-first precedence MUST be preserved, restore fallback MUST apply when dump is unavailable, and all schemas MAY be enumerated only when no filter is supplied.

#### Scenario: Schema filter limits stream
- GIVEN a schema filter selects subset of schemas
- WHEN logical clone enumerates streams
- THEN only selected schemas are enumerated

#### Scenario: Precedence remains stable
- GIVEN dump and restore options both provide schema sources
- WHEN clone chooses source
- THEN dump source wins; restore is fallback only when dump is unavailable

#### Scenario: No filter enumerates all
- GIVEN no schema filter is supplied
- WHEN logical clone enumerates streams
- THEN all schemas are enumerated

### Requirement: Cancellation-Safe Schema-Replay Cleanup

After cancellation, schema-replay database cleanup MUST run with an independent bounded context and MUST NOT be prevented by the cancelled operation's context. The primary operation error MUST remain authoritative; cleanup failure MUST be surfaced only as warning or secondary evidence.

#### Scenario: Cleanup runs after cancellation
- GIVEN schema replay is cancelled after partial work
- WHEN cleanup begins
- THEN cleanup executes using independent bounded lifetime

#### Scenario: Cleanup remains bounded
- GIVEN cleanup cannot complete before its bound
- WHEN the bound expires
- THEN cleanup stops and returns cancellation/timeout context without unbounded waiting

#### Scenario: Primary error remains authoritative
- GIVEN schema replay fails or is cancelled and cleanup also fails
- WHEN the operation returns
- THEN the replay or cancellation error remains the primary returned error
- AND cleanup failure is surfaced as warning or secondary evidence
