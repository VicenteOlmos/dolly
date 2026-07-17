# Production-Scale Clone Specification

## Purpose

Define the behavior of the `physical-backup`/`replication` clone strategy, which wraps `pg_basebackup` to create a physical cluster-level PostgreSQL replica from a source instance.

## Requirements

### Requirement: Preflight validates replication prerequisites

The strategy MUST validate replication-specific prerequisites before execution and MUST NOT connect to the target database (there is none — target is a data directory).

#### Scenario: wal_level too low

- GIVEN source `wal_level` is less than `replica`
- WHEN the replication preflight runs
- THEN it MUST fail with `PreflightVersion` or `PreflightPermission` error
- AND stderr suggests setting `wal_level = replica` and restarting PostgreSQL

#### Scenario: max_wal_senders too low

- GIVEN `max_wal_senders < 2`
- WHEN the replication preflight runs
- THEN it MUST fail
- AND stderr names the current and minimum values

#### Scenario: Missing REPLICATION privilege

- GIVEN the connecting role lacks `REPLICATION` and is not superuser
- WHEN the replication preflight runs
- THEN it MUST fail
- AND stderr names the role and suggests `ALTER ROLE ... WITH REPLICATION`

#### Scenario: pg_basebackup not on PATH

- GIVEN `pg_basebackup` is not found via `exec.LookPath`
- WHEN the replication preflight runs
- THEN it MUST fail
- AND stderr explains that PostgreSQL client tools are required

#### Scenario: Target directory exists and is non-empty

- GIVEN `TargetDir` points to an existing non-empty directory
- WHEN the replication preflight runs
- THEN it MUST fail
- AND stderr instructs the user to provide an empty or non-existent path

#### Scenario: All prerequisites pass

- GIVEN `wal_level = replica`, `max_wal_senders >= 2`, REPLICATION privilege, `pg_basebackup` on PATH, and empty or non-existent target directory
- WHEN the replication preflight runs
- THEN it MUST pass
- AND strategy execution proceeds

### Requirement: pg_basebackup execution

The strategy MUST run `pg_basebackup` as a subprocess, decomposing the `SourceDSN` into host, port, user, and password flags. An existing caller-owned target MUST be rejected before `pg_basebackup` starts and MUST remain untouched. If Dolly creates a partial target, the strategy MUST retain it after `pg_basebackup` or post-backup validation failure, return an error identifying the retained target for explicit cleanup, and MUST NOT invoke recursive target deletion on any failure path.

#### Scenario: Successful base backup

- GIVEN all preflight checks pass
- WHEN `ReplicationStrategy.Execute` is called
- THEN `pg_basebackup` is spawned with `-h <host> -p <port> -U <user> -D <TargetDir> -Fp -Xs -P -v`
- AND progress is reported via `ProgressFn`
- AND the function returns nil on clean exit

#### Scenario: pg_basebackup failure

- GIVEN preflight checks pass
- AND `pg_basebackup` exits non-zero (e.g., disk full, connection lost)
- WHEN `ReplicationStrategy.Execute` is called
   - THEN the error wraps the `pg_basebackup` exit reason
   - AND the error names the retained target for explicit cleanup
   - AND the target directory remains available if partially written

#### Scenario: Existing target is rejected before backup

- GIVEN target already exists and is caller-owned
- WHEN the strategy is executed
- THEN it returns an error before `pg_basebackup` starts
- AND target contents remain unchanged

#### Scenario: Dolly-created target is retained after validation failure

- GIVEN `pg_basebackup` succeeds and Dolly-created target fails post-backup validation
- WHEN execution returns the validation error
- THEN the error identifies the retained target for explicit cleanup
- AND no recursive target deletion is invoked

### Requirement: Recovery configuration written

After a successful `pg_basebackup`, the strategy MUST write `postgresql.auto.conf` with `primary_conninfo` derived from the source DSN, enabling the clone to start as a streaming replica.

#### Scenario: Recovery config created

- GIVEN `pg_basebackup` succeeded
- WHEN execution completes
- THEN `TargetDir/postgresql.auto.conf` exists
- AND contains `primary_conninfo` with host, port, user, password, and `application_name=dolly_clone`
- AND `TargetDir/standby.signal` exists

### Requirement: Next-steps reporting

The `physical-backup` strategy MUST report actionable next-steps via the error message or a final progress call instead of returning quietly. The target is a new PostgreSQL cluster replica, not a reachable single target database.

#### Scenario: Completion message

- GIVEN strategy execution succeeded
- WHEN the function returns
- THEN the final progress message or return value includes the target directory path, the command to start the new cluster replica, and a note that `pg_basebackup` creates a full cluster copy (all databases)
