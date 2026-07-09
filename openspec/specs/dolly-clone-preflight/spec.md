# dolly-clone-preflight Specification

## Purpose

Fail-fast validation of PostgreSQL reachability, role privileges, and version compatibility before `dolly clone` performs dumps, `CREATE DATABASE`, or strategy execution.

## Requirements

### Requirement: Preflight runs before clone side effects

The clone orchestrator MUST invoke shared preflight after `SourceDSN` and `CloneName` are validated and the strategy is resolved, and BEFORE strategy `Execute`, `CREATE DATABASE`, `pg_dump`/`psql`, or creation of any temporary dump directory.

#### Scenario: Preflight precedes temp dump dir

- GIVEN a clone run that would fail permission checks
- WHEN `clone.Run` is invoked with a configured `DumpDir`
- THEN preflight fails
- AND no `dolly-clone-*` child directory exists under `DumpDir`
- AND the process exits with status **1**

#### Scenario: Preflight precedes strategy execute

- GIVEN preflight detects unreachable source
- WHEN `clone.Run` is invoked
- THEN strategy `Execute` MUST NOT be called
- AND stderr reports reachability failure

### Requirement: Database reachability

Preflight MUST verify the source database is reachable via `PingContext`. When the target or admin connection differs from the source, preflight MUST verify that connection is reachable as well.

#### Scenario: Source unreachable

- GIVEN a source DSN that does not connect
- WHEN preflight runs
- THEN it returns an error naming the source
- AND no permission or version checks run afterward

#### Scenario: Distinct target admin unreachable

- GIVEN source is reachable and target/admin DSN is distinct and unreachable
- WHEN preflight runs
- THEN it returns an error naming the target or admin endpoint

### Requirement: Strategy-aware permissions

Preflight MUST evaluate privileges using PostgreSQL catalog queries (e.g. `has_database_privilege`, `pg_roles.rolcreatedb`) against the resolved strategy. At minimum:

| Strategy | Required checks |
|----------|-----------------|
| All | `CONNECT` on source database |
| All when `SkipCreate` is false | Role `CREATEDB` on admin, OR target database already exists and role can connect to it |
| `schema-replay` | `SELECT` on user tables, partitions, views, and materialized views; `SELECT` on FK referenced tables; `USAGE` on sequences; schema `USAGE` on non-system schemas with clone objects; type `USAGE` on composites/domains/enums; function/procedure dump visibility per pg_dump rules; non-`plpgsql` extensions available on target server; when target reachable: extension installed OR `CREATE EXTENSION`, schema `CREATE`/`USAGE`; when `SkipCreate=false`: admin `CREATE EXTENSION` heuristic |
| `logical-stream` | Until implemented: same as **All** (no source read/extension matrix) |
| `template` | Same-instance and `CREATEDB` (or existing target); active-connection guard MAY remain in strategy |

#### Scenario: Missing CREATEDB before create

- GIVEN `SkipCreate` is false and the connecting role lacks `CREATEDB`
- WHEN preflight runs for any strategy that creates a database
- THEN it fails with the role name and missing privilege
- AND stderr suggests granting `CREATEDB` or using `skip_create` with an existing database

#### Scenario: Skip create with existing target

- GIVEN `SkipCreate` is true and the target database exists
- WHEN preflight runs
- THEN it MUST NOT require `CREATEDB`
- AND it MUST verify `CONNECT` (and restore/write prerequisites per strategy) on the target

#### Scenario: Insufficient read on source

- GIVEN `schema-replay` is selected and the role cannot read required source tables
- WHEN preflight runs
- THEN it fails before `pg_dump` or dump engine work
- AND stderr identifies insufficient read privilege

#### Scenario: Insufficient read on view

- GIVEN `schema-replay` is selected and the role lacks `SELECT` on a user view
- WHEN preflight runs
- THEN it fails before `pg_dump`
- AND stderr names the view and indicates missing `SELECT`

#### Scenario: FK referenced table not readable

- GIVEN a foreign key from a user table to another user table
- AND the role can read the referencing table but not the referenced table
- WHEN preflight runs for `schema-replay`
- THEN it fails before `pg_dump`
- AND stderr names the referenced table and the FK relationship

#### Scenario: Sequence USAGE missing

- GIVEN a user-owned sequence used by cloned tables
- AND the role lacks `USAGE` on that sequence
- WHEN preflight runs for `schema-replay`
- THEN it fails before `pg_dump`
- AND stderr names the sequence and indicates missing `USAGE`

#### Scenario: Extension not available on target server

- GIVEN `schema-replay` and the source database uses extension `E` (other than `plpgsql`)
- AND extension `E` is not listed in `pg_available_extensions` on the target server
- WHEN preflight runs for a cross-server clone (or same-server with distinct availability probe)
- THEN it fails before `pg_dump`
- AND stderr names the extension and indicates it must be installed on the target server

#### Scenario: logical-stream defers read matrix

- GIVEN `logical-stream` is selected and expanded checks are not yet implemented
- WHEN preflight runs
- THEN it MUST run reachability and database-level permission checks only
- AND it MUST NOT scan user tables, views, FK, or sequences for read privileges

### Requirement: Source schema and type USAGE for schema-replay

For `schema-replay`, preflight MUST verify `USAGE` on every non-system schema containing user clone objects. It MUST verify `USAGE` on composite types, domains, and enums in those schemas referenced by cloned objects.

#### Scenario: Missing schema USAGE

- GIVEN `schema-replay` and role lacks `USAGE` on user schema `app`
- WHEN preflight runs
- THEN it fails before `pg_dump`
- AND stderr names `app` and missing schema `USAGE`

#### Scenario: Missing type USAGE

- GIVEN role lacks `USAGE` on enum `app.status`
- WHEN preflight runs for `schema-replay`
- THEN it fails before `pg_dump`
- AND stderr names the type and missing `USAGE`

### Requirement: Source function visibility for schema-replay

For `schema-replay`, preflight MUST verify the role can dump every user function/procedure in non-system schemas per pg_dump visibility rules.

#### Scenario: Function not dump-visible

- GIVEN a user function the role cannot dump per pg_dump rules
- WHEN preflight runs for `schema-replay`
- THEN it fails before `pg_dump`
- AND stderr names the function and insufficient dump visibility

### Requirement: Target extension install or creatable

For `schema-replay` when the target DB is reachable (`SkipCreate=true`), preflight MUST verify each required non-`plpgsql` extension is installed in the target DB OR the restore role can `CREATE EXTENSION`.

#### Scenario: Extension not installed and not creatable

- GIVEN extension `E` is on the target server but not in the target DB
- AND the restore role cannot `CREATE EXTENSION`
- WHEN preflight runs with `SkipCreate=true`
- THEN it fails before restore
- AND stderr names `E` and remediation

#### Scenario: Extension creatable on target

- GIVEN extension `E` is not installed but the role may `CREATE EXTENSION`
- WHEN preflight runs with `SkipCreate=true`
- THEN the extension check passes

### Requirement: Target schema DDL for restore

For `schema-replay` when the target is reachable, preflight MUST verify `USAGE` and `CREATE` on each non-system schema where objects will be restored.

#### Scenario: Missing target schema USAGE

- GIVEN the target role lacks `USAGE` on existing schema `app`
- WHEN preflight runs with `SkipCreate=true`
- THEN it fails before restore
- AND stderr names the schema and missing `USAGE`

#### Scenario: Missing target schema CREATE

- GIVEN the target role has `USAGE` but not `CREATE` on schema `app`
- WHEN preflight runs with `SkipCreate=true`
- THEN it fails before restore
- AND stderr names the schema and missing `CREATE`

### Requirement: Admin CREATE EXTENSION when SkipCreate is false

When `SkipCreate=false`, preflight MUST probe whether the admin connection can `CREATE EXTENSION` for required extensions (heuristic — target DB may not exist yet).

#### Scenario: Admin cannot CREATE EXTENSION

- GIVEN `SkipCreate=false`, source requires extension `E`, and admin cannot `CREATE EXTENSION`
- WHEN preflight runs
- THEN it fails with stderr naming `E` and admin remediation

### Requirement: Optional permission result cache

When `clone.preflight.cache_permissions` is enabled, a successful permission phase MAY be stored on disk (default path `.dolly/permissions-cache.yaml`, TTL default 24h). On cache hit, preflight MUST still run reachability and version checks and MUST NOT skip them.

#### Scenario: Cache hit skips permission SQL

- GIVEN a valid non-expired cache entry for the resolved DSNs, strategy, and `skip_create` flag
- WHEN preflight runs
- THEN permission catalog queries are not executed
- AND reachability and version checks still run

### Requirement: Permission cache version invalidation

When the permission matrix changes, `check_version` in the cache key MUST be incremented so stale entries are ignored.

#### Scenario: Cache miss after version bump

- GIVEN a valid cache entry from a prior `check_version`
- WHEN preflight runs after `check_version` is incremented
- THEN permission SQL executes (cache miss)
- AND reachability and version checks still run

### Requirement: Version compatibility

Preflight MUST read `server_version` / `server_version_num` on source and target when both are probed. Default policy: cross-server target major MUST be greater than or equal to source major; mismatch MUST fail. For `schema-replay`, when `pg_dump` is on `PATH`, preflight MUST compare client major to source server major and MUST fail on mismatch (warn-only is not permitted in v1).

#### Scenario: Cross-major downgrade blocked

- GIVEN source major 16 and target major 15
- WHEN preflight runs for a cross-server clone
- THEN it fails with both version numbers on stderr

#### Scenario: pg_dump major mismatch on schema-replay

- GIVEN `schema-replay` and `pg_dump` major differs from source server major
- WHEN preflight runs
- THEN it fails before schema replay
- AND stderr states the client and server majors and remediation (install matching client tools)

#### Scenario: Compatible versions pass

- GIVEN source major 15, target major 15, and matching `pg_dump` major for `schema-replay`
- WHEN preflight runs
- THEN version checks succeed and execution may continue

### Requirement: Actionable preflight errors

Preflight failures MUST be returned to the caller with stderr-safe messages that include: failing check kind (reachability, permission, version), relevant role or database name, and a short remediation hint. Messages MUST NOT require reading source code to act on.

#### Scenario: Permission error is actionable

- GIVEN a failed `CREATEDB` check for role `app_user`
- WHEN the CLI surfaces the error
- THEN stderr contains `app_user`, `CREATEDB`, and guidance to grant privilege or use `skip_create`

### Requirement: Testable preflight without live PostgreSQL

Preflight logic MUST be unit-testable with `go-sqlmock` and table-driven cases covering the permission matrix and version policy. Live-cluster cases MUST be behind the `integration` build tag and `DOLLY_TEST_PG_DSN`, and MUST be skippable in default `go test ./...`.

#### Scenario: Unit tests cover matrix without cluster

- GIVEN sqlmock expectations for privilege and version queries
- WHEN `go test ./internal/clone/...` runs without `DOLLY_TEST_PG_DSN`
- THEN reachability, permission, and version failure paths are asserted without a live server
- AND sqlmock covers schema/type/function source checks and target extension/schema failures

#### Scenario: Live preflight integration is skippable

- GIVEN `DOLLY_TEST_PG_DSN` is unset or `-short` is set
- WHEN `go test -tags=integration ./internal/clone/...` runs
- THEN live preflight tests skip without failing the package
- WHEN `DOLLY_TEST_PG_DSN` is set and points at a reachable PostgreSQL
- THEN `TestPreflightSchemaReplayLive` exercises reachability, permissions, extensions, and version checks against a real server

### Requirement: Deferred preflight scope

v1 MUST document checks deferred beyond expanded `schema-replay` matrix: explicit trigger scan (table-derived coverage), RLS row visibility on target sessions, tablespaces/global roles (v2 warn), `logical-stream` expanded matrix (follow-on), dry-run `pg_dump` primary gate.

#### Scenario: Triggers deferred to table-derived coverage

- GIVEN trigger DDL derives from table dump visibility
- WHEN preflight runs v1 `schema-replay`
- THEN no explicit `pg_trigger` scan runs
- AND deferral is documented in spec
