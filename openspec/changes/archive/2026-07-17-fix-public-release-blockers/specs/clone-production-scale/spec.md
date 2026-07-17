# Delta for Production-Scale Clone

## MODIFIED Requirements

### Requirement: pg_basebackup execution

The strategy MUST run `pg_basebackup` as a subprocess. An existing caller-owned target MUST be rejected before `pg_basebackup` starts and MUST remain untouched. If Dolly creates a partial target, the strategy MUST retain it after `pg_basebackup` or post-backup validation failure, return an error identifying the retained target for explicit cleanup, and MUST NOT invoke recursive target deletion on any failure path.
(Previously: failed execution SHOULD clean up partially written target directories without distinguishing ownership.)

#### Scenario: Existing target is rejected before backup
- GIVEN target already exists and is caller-owned
- WHEN the strategy is executed
- THEN it returns an error before `pg_basebackup` starts
- AND target contents remain unchanged

#### Scenario: Dolly-created target is retained after backup failure
- GIVEN target did not exist before execution
- WHEN `pg_basebackup` fails after creating it
- THEN the strategy returns an error naming the retained target
- AND the target remains available for explicit cleanup

#### Scenario: Dolly-created target is retained after validation failure
- GIVEN `pg_basebackup` succeeds and Dolly-created target fails post-backup validation
- WHEN execution returns the validation error
- THEN the error identifies the retained target for explicit cleanup
- AND no recursive target deletion is invoked

#### Scenario: Successful base backup
- GIVEN preflight checks pass
- WHEN `ReplicationStrategy.Execute` completes successfully
- THEN it returns nil and retains the target
