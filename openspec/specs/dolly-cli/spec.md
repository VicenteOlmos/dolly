# dolly CLI Specification

## Purpose

Give operators a `dolly` executable to export and restore PostgreSQL data without custom integration code. The CLI delegates to `postgres-dump-streaming` and `postgres-restore-streaming`; library behavior remains defined by those specs.

## Requirements

### Requirement: Executable and subcommands

The system SHALL accept `dump`, `restore`, `tui`, `clone`, and `config`. `clone` MUST run preflight after resolving inputs and strategy, then provision or use the target database and complete the selected clone strategy. Existing commands MUST remain unchanged except for help behavior defined in help requirements below. Invoking `dolly` with **no arguments** MUST print the full command catalog on stderr and exit with status **1**.

#### Scenario: Dump still works

- GIVEN reachable PostgreSQL and writable output
- WHEN `dolly dump --dsn DSN --output DIR` runs
- THEN it exits 0 with valid artifacts

#### Scenario: Bare invocation lists commands

- GIVEN the operator runs `dolly` with no arguments and without `-h` or `--help`
- WHEN the process starts
- THEN stderr lists all available subcommands (`dump`, `restore`, `clone`, `tui`, `config`)
- AND the process exits with status **1**

#### Scenario: Unknown subcommand

- GIVEN `dolly` is invoked with an unrecognized subcommand name (including `help`)
- WHEN the process starts
- THEN it exits with status **1**
- AND stderr explains the error

#### Scenario: Clone succeeds

- GIVEN valid clone inputs, reachable source, passing preflight, and a supported strategy
- WHEN `dolly clone` runs
- THEN it exits 0 and target schema and data match the source per the selected strategy

### Requirement: Clone preflight before destructive work

`dolly clone` MUST run shared clone preflight (per `dolly-clone-preflight`) after clone inputs and strategy are resolved and before database creation, external tools, dump/restore, or temporary artifact directories are created. Preflight failures MUST exit with status **1** and MUST NOT start clone side effects.

#### Scenario: Preflight blocks clone early

- GIVEN insufficient privileges for the selected strategy
- WHEN `dolly clone` runs with otherwise valid flags
- THEN the process exits with status **1**
- AND stderr contains an actionable preflight message
- AND no temporary dump directory is created

#### Scenario: Preflight passes then clone proceeds

- GIVEN reachable databases, sufficient privileges, and compatible versions
- WHEN `dolly clone` runs
- THEN preflight completes successfully
- AND clone continues with existing strategy and creation behavior

#### Scenario: Help skips preflight

- GIVEN the operator runs `dolly clone --help` or `dolly clone -h`
- WHEN flag parsing handles help
- THEN no preflight or database work occurs
- AND the process exits with status **0**

### Requirement: Clone preflight error surfacing

The CLI MUST propagate preflight errors to stderr without wrapping away role names, version numbers, or remediation hints defined by `dolly-clone-preflight`.

#### Scenario: Version mismatch surfaced

- GIVEN preflight fails on cross-major version policy
- WHEN `dolly clone` exits
- THEN stderr includes source and target version information sufficient to diagnose the mismatch

### Requirement: Root help flags

The system MUST recognize `-h` and `--help` on the root invocation (`dolly` with no subcommand). Root help MUST print usage on stderr, list available subcommands (`dump`, `restore`, `clone`, `tui`, `config`), and include a hint that per-command help is available via `dolly <cmd> -h` or `dolly <cmd> --help`. Root help MUST exit with status **0** and MUST NOT connect to a database.

#### Scenario: Root long help

- GIVEN the operator invokes `dolly --help`
- WHEN the process handles the request
- THEN stderr contains root usage, the command list (`dump`, `restore`, `clone`, `tui`, `config`), and a subcommand-help hint
- AND the process exits with status **0**
- AND no database connection is attempted

#### Scenario: Root short help

- GIVEN the operator invokes `dolly -h`
- WHEN the process handles the request
- THEN behavior matches root long help and the process exits with status **0**

### Requirement: config subcommand help flags

The `config` subcommand MUST accept `-h` and `--help`. Config help MUST print usage on stderr, exit with status **0**, and MUST NOT write or modify any config file.

#### Scenario: config help

- GIVEN the operator runs `dolly config --help` or `dolly config -h`
- WHEN flag parsing handles the help request
- THEN stderr contains config usage including the `init` sub-action with `--force` flag and the `show` sub-action
- AND the process exits with status **0**
- AND no file is written

### Requirement: config show sub-action

The `config show` sub-action MUST print the fully resolved configuration (code defaults merged with any file overrides) to stdout as formatted JSON and exit **0**. It MUST NOT modify any config file. When `LoadConfig` returns an error, `config show` MUST print the error on stderr and exit **1**.

#### Scenario: Show prints resolved config

- GIVEN a valid or absent `config.jsonc` (defaults apply when absent)
- WHEN `dolly config show` runs
- THEN the resolved config is printed to stdout as formatted JSON
- AND the process exits with status **0**

#### Scenario: Show reflects file overrides

- GIVEN `config.jsonc` sets one or more knobs
- WHEN `dolly config show` runs
- THEN output reflects file-overridden values merged with code defaults

#### Scenario: Show on config load error

- GIVEN `config.jsonc` contains invalid JSON after comment stripping
- WHEN `dolly config show` runs
- THEN stderr contains the load error
- AND the process exits with status **1**

### Requirement: Reject help subcommand

The system MUST NOT treat `help` as a subcommand. Invocations of the form `dolly help` or `dolly help <anything>` MUST be rejected before any help text for another command is printed.

#### Scenario: Bare help subcommand

- GIVEN the operator runs `dolly help`
- WHEN the process starts
- THEN it exits with status **1**
- AND stderr explains that `help` is not a valid subcommand (or treats it as an unknown command)

#### Scenario: Help with target command

- GIVEN the operator runs `dolly help dump` (or any `dolly help <cmd>`)
- WHEN the process starts
- THEN it exits with status **1**
- AND stderr does not print dump-specific usage
- AND the operator is directed to use `dolly dump --help` instead

### Requirement: Subcommand help flags

Each subcommand `dump`, `restore`, `clone`, and `tui` MUST accept `-h` and `--help`. Subcommand help MUST print that command’s usage on stderr, exit with status **0**, and MUST NOT connect to a database or start dump/restore/clone/TUI work.

#### Scenario: Dump help

- GIVEN the operator runs `dolly dump --help` or `dolly dump -h`
- WHEN flag parsing handles the help request
- THEN stderr contains dump usage including registered flags (e.g. `--dsn`, `--output`)
- AND the process exits with status **0**
- AND no dump run or DB reachability check occurs

#### Scenario: Restore help

- GIVEN the operator runs `dolly restore --help` or `dolly restore -h`
- WHEN flag parsing handles the help request
- THEN stderr contains restore usage including registered flags (e.g. `--dsn`, `--input`, `--on-conflict`)
- AND the process exits with status **0**
- AND no restore run or DB reachability check occurs

#### Scenario: Clone help

- GIVEN the operator runs `dolly clone --help` or `dolly clone -h`
- WHEN flag parsing handles the help request
- THEN stderr contains clone usage including registered flags
- AND the process exits with status **0**
- AND no clone run, prompts, or DB work occurs

#### Scenario: TUI help without terminal

- GIVEN stdout is **not** a terminal (piped or redirected)
- WHEN the operator runs `dolly tui --help` or `dolly tui -h`
- THEN stderr contains tui usage
- AND the process exits with status **0**
- AND the interactive TUI does not start
- AND the non-TTY error for bare `dolly tui` does not apply

### Requirement: TUI subcommand dispatch

The system SHALL accept `tui` as a valid subcommand alongside `dump`. Invoking `dolly tui` MUST start the interactive terminal application defined by `dolly-tui`. **Before** checking whether stdout is a terminal, the system MUST handle `-h` and `--help` for `tui` per subcommand help requirements.

#### Scenario: Successful TUI invocation

- GIVEN stdout is a terminal
- WHEN the operator runs `dolly tui` without help flags
- THEN an interactive TUI session starts
- AND the process exits 0 only after the operator quits the session normally

#### Scenario: Non-TTY TUI invocation rejected

- GIVEN stdout is not a terminal
- AND the operator does not pass `-h` or `--help`
- WHEN the operator runs `dolly tui`
- THEN the process exits with status **1** before starting the interactive program
- AND stderr explains that a terminal is required

### Requirement: Restore subcommand

The system SHALL accept `restore` as a subcommand that loads dump artifacts into PostgreSQL using `postgres-restore-streaming`.

#### Scenario: Successful restore invocation

- GIVEN a reachable PostgreSQL database with a schema matching dump metadata, a valid dump directory, and default conflict policy
- WHEN the operator runs `dolly restore` with valid `--dsn` and `--input`
- THEN the process exits with status 0
- AND row data from the artifacts is present in the target database

#### Scenario: Restore with skip policy

- GIVEN a target database that already contains rows conflicting on primary keys
- WHEN the operator runs `dolly restore` with `--on-conflict skip`
- THEN the process exits with status 0
- AND conflicting rows are left unchanged while non-conflicting rows are inserted

#### Scenario: Restore with upsert policy

- GIVEN a target database with existing rows sharing primary keys with the dump
- WHEN the operator runs `dolly restore` with `--on-conflict upsert`
- THEN the process exits with status 0
- AND existing rows are updated for non-key columns from the artifact

#### Scenario: Restore with replace policy

- GIVEN a target database with existing data in tables listed in the dump
- WHEN the operator runs `dolly restore` with `--replace`
- THEN affected tables are truncated per restore engine rules before insert
- AND the process exits with status 0 when load completes

### Requirement: Restore required flags

The `restore` subcommand MUST accept `--dsn` (PostgreSQL connection string) and `--input` (dump directory). Both MUST be provided and non-empty before a restore run proceeds.

#### Scenario: Missing or empty required restore flag

- GIVEN `--dsn` or `--input` is omitted or empty after parsing
- WHEN `dolly restore` is invoked
- THEN the process exits with status 1
- AND stderr identifies which required flag is missing or invalid

#### Scenario: Valid required restore flags

- GIVEN non-empty `--dsn` and `--input` values
- WHEN flag parsing completes successfully
- THEN the restore run uses those values for connection and artifact location

### Requirement: Restore conflict and transaction flags

The `restore` subcommand MUST expose conflict policy and transactional mode consistent with `postgres-restore-streaming`.

#### Scenario: Default on-conflict is error

- GIVEN `--on-conflict` is not set
- WHEN `dolly restore` encounters a primary-key conflict
- THEN the process exits with status 1
- AND stderr reports the restore failure

#### Scenario: Non-transactional restore

- GIVEN `--no-transaction` is set on `dolly restore`
- AND the database is reachable
- WHEN restore completes or fails mid-run
- THEN commits follow per-table semantics defined by the restore engine
- AND partial progress MAY remain visible after a mid-run failure

### Requirement: Restore engine delegation

The CLI MUST invoke the restore engine for load. It MUST NOT change `internal/db` behavioral contracts or introduce artifact format fields beyond what `postgres-dump-streaming` defines.

#### Scenario: Library contracts drive artifacts

- GIVEN a successful `dolly restore` run
- WHEN behavior is inspected
- THEN loading follows `postgres-restore-streaming` requirements
- AND the CLI does not alter `metadata.json` or `.ndjson` layout

### Requirement: Restore reachability and errors

Before starting restore, the system MUST verify database reachability. Operational failures MUST be reported on stderr with exit code 1.

#### Scenario: Unreachable database on restore

- GIVEN a DSN that does not connect to a live PostgreSQL instance
- WHEN `dolly restore` is invoked with otherwise valid flags
- THEN the process exits with status 1
- AND stderr reports a connection or reachability failure

#### Scenario: Restore engine failure

- GIVEN a reachable database and invalid or incompatible dump input
- WHEN the restore engine returns an error
- THEN the process exits with status 1
- AND stderr includes error detail sufficient to diagnose validation, insert, or policy failure

### Requirement: Testable restore flag validation

Restore flag parsing and required-flag validation MUST be verifiable without a live database.

#### Scenario: Unit tests cover restore parse edge cases

- GIVEN automated tests that exercise `restore` flag combinations in isolation
- WHEN tests run without PostgreSQL
- THEN required flags, `--on-conflict` values, `--replace`, and `--no-transaction` are asserted without connecting to a database

### Requirement: Dump subcommand unchanged

Adding `tui` or `restore` MUST NOT alter `dump` subcommand behavior, flags, exit codes, or delegation to the dump engine.

#### Scenario: Dump behavior preserved

- GIVEN valid or invalid `dolly dump` invocations
- WHEN compared before and after the TUI foundation change
- THEN flag parsing, reachability checks, dump execution, and error reporting behave identically

### Requirement: Required connection flags

The `dump` subcommand MUST accept `--dsn` (PostgreSQL connection string) and `--output` (destination directory). Both MUST be provided and non-empty before a dump run proceeds.

#### Scenario: Missing or empty required flag

- GIVEN `--dsn` or `--output` is omitted or empty after parsing
- WHEN `dolly dump` is invoked
- THEN the process exits with status 1
- AND stderr identifies which required flag is missing or invalid

#### Scenario: Valid required flags

- GIVEN non-empty `--dsn` and `--output` values
- WHEN flag parsing completes successfully
- THEN the dump run uses those values for connection and output location

### Requirement: Optional non-transactional mode

The `dump` subcommand MAY accept `--no-transaction`. When absent, dumps MUST use the dump engine's default transactional consistency. When present, dumps MUST run without requiring a single wrapping transaction.

#### Scenario: Default transactional dump

- GIVEN `--no-transaction` is not set
- AND the database is reachable
- WHEN `dolly dump` completes successfully
- THEN snapshot consistency follows `postgres-dump-streaming` transactional rules

#### Scenario: Non-transactional dump

- GIVEN `--no-transaction` is set
- AND the database is reachable
- WHEN `dolly dump` completes successfully
- THEN the dump runs without a single wrapping transaction while still producing valid artifacts

### Requirement: Database reachability

Before starting a dump, the system MUST verify that the database identified by `--dsn` is reachable.

#### Scenario: Unreachable database

- GIVEN a DSN that does not connect to a live PostgreSQL instance
- WHEN `dolly dump` is invoked with otherwise valid flags
- THEN the process exits with status 1
- AND stderr reports a connection or reachability failure

### Requirement: Dump engine delegation

The CLI MUST invoke the existing dump engine for export. It MUST NOT change `internal/db` or `internal/dump` behavioral contracts.

#### Scenario: Library contracts unchanged

- GIVEN a successful `dolly dump` run
- WHEN artifacts are inspected
- THEN `metadata.json` and per-table `.ndjson` files meet `postgres-dump-streaming` requirements
- AND no new dump-format fields or files are introduced by the CLI alone

### Requirement: Process exit and error reporting

Operational failures MUST be reported on stderr. The process MUST exit **0** on success and **1** on any failure (parse, connection, or engine), **except** that any recognized `-h` or `--help` request at root or on a subcommand MUST exit **0** regardless of whether the invocation would otherwise fail (e.g. bare `dolly` without help still exits **1**).

#### Scenario: Dump engine failure

- GIVEN a reachable database
- WHEN the dump engine returns an error during export
- THEN the process exits with status **1**
- AND stderr includes error detail sufficient to diagnose the failure

#### Scenario: Help always succeeds

- GIVEN any valid root or subcommand help invocation per help requirements
- WHEN the process handles `-h` or `--help`
- THEN the process exits with status **0**

### Requirement: Testable flag validation

Flag parsing and required-flag validation MUST be verifiable without a live database or network access.

#### Scenario: Unit tests cover parse edge cases

- GIVEN automated tests that exercise flag combinations in isolation
- WHEN tests run in environments without PostgreSQL
- THEN required-flag rules, optional `--no-transaction`, and invalid-input cases are asserted without connecting to a database

### Requirement: Clone input validation

`dolly clone` MUST reject unsafe clone names and table artifact names before DB work, subprocesses, or file I/O. Clone names MUST be alphanumeric/underscore; table artifact names MUST NOT contain separators/traversal.

#### Scenario: Unsafe clone name

- GIVEN clone input name `prod;drop` or `prod-copy`
- WHEN clone validates inputs
- THEN it exits 1 with an actionable validation error
- AND no side effects begin

#### Scenario: Unsafe table artifact

- GIVEN a table artifact name contains `../` or separator
- WHEN dump/clone prepares table file output
- THEN the operation fails before opening that path

### Requirement: Interactive clone flow

`dolly clone` MUST prompt for source mode, clone name, and target URL mode. Defaults SHALL be `.env`, `{source}_kloned_1`, and same host. Prompts MUST NOT display passwords.
(Previously: defaults referenced `.env` directly, without named-profile mapping or redacted prompts.)

#### Scenario: Defaults

- GIVEN `.env` has source DSN and stdin is a TTY
- WHEN all prompts are accepted
- THEN clone uses `.env`, `{source}_kloned_1`, and source host

#### Scenario: Manual URL

- GIVEN stdin is a TTY
- WHEN a valid source URL is entered
- THEN clone uses that URL as the source DSN

#### Scenario: Prompt redacts secrets

- GIVEN a default source DSN contains a password
- WHEN clone prompts are rendered
- THEN the password is not displayed in terminal output

### Requirement: Fast-forward and TTY rules

`dolly clone -ff` MUST skip prompts and use config defaults. Non-TTY without `-ff` MUST fail.

#### Scenario: Fast-forward

- GIVEN config can resolve source, target name, and target URL defaults
- WHEN `dolly clone -ff` runs
- THEN no prompt is displayed and clone uses those defaults

#### Scenario: Non-TTY

- GIVEN stdin is not a TTY
- WHEN `dolly clone` runs without `-ff`
- THEN it exits 1 with a hint to use `-ff`

### Requirement: Target database creation

Clone MUST create the target database before restore unless skip-create is set. Creation failures MUST exit 1 with actionable stderr. Database creation MUST occur only after successful preflight when preflight is required for the run.

#### Scenario: Create target

- GIVEN source credentials can create databases and preflight passes
- WHEN clone targets a new database name
- THEN the target database is created before restore

#### Scenario: Skip creation

- GIVEN config sets skip creation and preflight passes for an existing target
- WHEN clone runs
- THEN clone does not create the database and restore may proceed

### Requirement: Clone placeholders

Sanitization MUST be functional when enabled in config. When `sanitization.enabled` is `true`, data SHALL be transformed per `dump-row-transform` before writing. When `false` (default), data MUST NOT be mutated.

#### Scenario: Sanitization enabled mutates data

- GIVEN `sanitization.enabled: true` in config
- WHEN clone runs with schema-replay strategy
- THEN NDJSON output contains sanitized values per `dump-row-transform`

#### Scenario: Sanitization disabled is full copy

- GIVEN `sanitization.enabled: false` (default)
- WHEN clone runs
- THEN data is full and unsanitized

#### Scenario: Clone help mentions sanitization

- GIVEN the operator runs `dolly clone --help`
- THEN stderr includes the `sanitization.enabled` config option reference

### Requirement: Full clone from empty target

When no percentage or subset is selected, `clone` MUST default to a full database clone from scratch. The target MUST NOT require pre-existing user tables, indexes, constraints, sequences, or other source-owned schema objects.

#### Scenario: Empty target is built
- GIVEN a reachable source and a newly created empty target
- WHEN full clone runs
- THEN target structure is created before row loading
- AND target data matches the source after completion

#### Scenario: Subset remains separate
- GIVEN percentage or subset behavior is unsupported or not selected
- WHEN clone runs
- THEN the run is full and unsanitized
- AND unsupported subset settings MUST NOT silently change full-clone semantics

### Requirement: Clone strategy selection

`clone` MUST expose strategy selection through prompt, config, or flag. Fast-forward and non-TTY runs MUST require a resolvable default or explicit strategy. Invalid or unavailable strategies MUST fail with actionable guidance before destructive work.

#### Scenario: Interactive choice
- GIVEN stdin is a TTY
- WHEN clone asks for strategy
- THEN the operator can choose template, schema-replay, logical-stream, or physical-backup

#### Scenario: Unavailable strategy
- GIVEN the selected strategy preconditions are not met
- WHEN clone validates the run
- THEN it exits 1 with the failed precondition and a safe fallback suggestion

### Requirement: Supported clone strategies

The system SHALL define these user-visible strategies: `template`, `schema-replay`, `logical-stream`, and `physical-backup`. Each selected strategy MUST validate preconditions and execute its clone operation. The `physical-backup` strategy (alias: `replication`) MUST execute a physical cluster-level replica via `pg_basebackup`. The `production-scale` alias MUST NOT resolve to any strategy. Unknown strategy errors MUST list canonical names only.

#### Scenario: Same-instance template
- GIVEN source and target are on the same PostgreSQL instance and template cloning is allowed
- WHEN `template` is selected
- THEN target is created with schema and data matching source

#### Scenario: Schema replay
- GIVEN PostgreSQL schema tooling is available and the target is empty
- WHEN `schema-replay` is selected
- THEN source structure is applied before current source data is restored

#### Scenario: Logical stream
- GIVEN source and target are reachable and direct table streaming is selected
- WHEN `logical-stream` runs for a large cross-server clone
- THEN rows are transferred without creating a temporary full data dump artifact

#### Scenario: Backward-compatible logical alias resolution
- GIVEN the operator passes `--strategy streaming-copy` or `--strategy copy-stream`
- WHEN `Resolve` runs
- THEN `CopyStreamStrategy` is returned

#### Scenario: Backward-compatible physical alias resolution
- GIVEN the operator passes `--strategy replication`
- WHEN `Resolve` runs
- THEN `ReplicationStrategy` is returned

#### Scenario: Production-scale alias rejected
- GIVEN the operator passes `--strategy production-scale`
- WHEN `Resolve` runs
- THEN it returns an unknown strategy error
- AND the error lists only `template`, `schema-replay`, `logical-stream`, and `physical-backup` as supported names

#### Scenario: Physical-backup clone
- GIVEN source has `wal_level >= replica`, `max_wal_senders >= 2`, and the role has `REPLICATION` privilege
- AND `pg_basebackup` is on PATH and `--target-dir` is empty or absent
- WHEN `physical-backup` is selected
- THEN `pg_basebackup` creates a physical replica and reports start-up next steps

#### Scenario: Physical-backup preflight blocks execution
- GIVEN any replication prerequisite is unmet (`wal_level`, `max_wal_senders`, privilege, tools, target dir)
- WHEN `physical-backup` is selected
- THEN the system exits with status 1 before any filesystem or database modifications
- AND stderr identifies the failing check with remediation hint

#### Scenario: Error message uses canonical names
- GIVEN the operator passes an unknown strategy name
- WHEN `Resolve` returns an error
- THEN the error message lists the four canonical strategy names and no aliases

### Requirement: Clone schema selection

`dolly clone` MUST resolve source schemas before preflight and clone execution. Explicit `--schemas` values MUST win over `clone.schemas` config defaults. When neither is set, fast-forward clone MUST default to all non-system schemas from the source database. Interactive clone MUST prompt for schema names after source DSN resolution and MUST validate entered names against source database schemas before continuing.

#### Scenario: Explicit schemas flag

- GIVEN reachable source and target databases
- WHEN `dolly clone --schemas app,billing` runs
- THEN clone uses only `app` and `billing` as source schemas
- AND those schemas are propagated to dump/restore execution

#### Scenario: Config default schemas

- GIVEN `config.jsonc` defines `clone.schemas: [app, billing]`
- WHEN `dolly clone -ff` runs without `--schemas`
- THEN clone uses the configured schemas without prompting

#### Scenario: Fast-forward discovers all source schemas

- GIVEN source contains non-system schemas `app` and `billing`
- WHEN `dolly clone -ff` runs without explicit or config schemas
- THEN clone connects to source and selects all non-system schemas
- AND no interactive schema prompt is shown

#### Scenario: Interactive names are validated

- GIVEN stdin is a TTY and source schemas are `app` and `billing`
- WHEN `dolly clone` prompts for schemas after resolving source DSN
- THEN entered schemas not present in source are rejected before preflight
- AND valid entered schemas continue to preflight and clone execution

### Requirement: Dump history list subcommand

The CLI MUST accept `dolly dump list` as a read-only subcommand that prints completed dumps for a base output directory. The base directory MUST default to `cfg.Dump.OutputDir` when `--output` is omitted. Listing MUST use the same merged store+disk history as the TUI (`ListBaseMerged`). The command MUST NOT connect to PostgreSQL or mutate database state. It MUST exit 0 when listing succeeds (including empty list).

#### Scenario: List dumps for config base dir

- GIVEN config `dump.output_dir` is `dolly_dump` and history records exist
- WHEN `dolly dump list` runs
- THEN stdout lists each dump with seq, path, schema label, and table count
- AND exit status is 0

#### Scenario: List with explicit base

- GIVEN `--output /tmp/dumps` is passed
- WHEN `dolly dump list --output /tmp/dumps` runs
- THEN listing is scoped to that base directory

#### Scenario: JSON output

- GIVEN `--json` is passed
- WHEN `dolly dump list --json` runs
- THEN stdout is a JSON array of dump history records
- AND no human table formatting is required

#### Scenario: Help

- GIVEN `dolly dump list --help` or `-h`
- WHEN invoked
- THEN usage for list flags is printed on stderr
- AND exit status is 0
- AND no DB connection occurs

### Requirement: Stderr progress output

The CLI SHALL write operation progress to stderr for `dump`, `restore`, and `clone` while work is running. When stderr is a TTY, progress SHOULD redraw inline as a bar with percentage and ETA. When stderr is not a TTY, progress MUST degrade to plain one-line events. On successful completion, the in-flight bar SHALL be replaced or followed by a final summary line. Progress output MUST NOT write to stdout or change exit codes.

#### Scenario: TTY progress bar

- GIVEN stderr is a TTY and a multi-table dump, restore, or clone runs
- WHEN progress events are emitted
- THEN stderr displays an inline progress bar with percentage and ETA when computable
- AND stdout remains reserved for command data output

#### Scenario: Redirected stderr uses line events

- GIVEN stderr is redirected or not a TTY
- WHEN progress events are emitted
- THEN each event is written as a plain line without carriage-return redraw control

#### Scenario: Completion and failures preserve CLI contract

- GIVEN an operation completes or fails
- WHEN the CLI exits
- THEN success prints a final summary line and exits 0
- AND failures keep existing stderr error reporting and exit status 1
