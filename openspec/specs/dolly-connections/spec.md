# dolly-connections Specification

## Purpose

Persist named PostgreSQL connection profiles for TUI and CLI when `save_connections` is enabled. Supports project-local and XDG-global stores, optional encryption, profile rename, auto-save after TUI connect, schema filters on profiles, and DSN resolution for `clone`, `dump`, and `restore`.

## Requirements

### Requirement: Connection profile record

Each saved profile MUST have a unique `name` and structured fields: `host`, `port`, `database`, `user`, `password`. The record MAY include `schemas` as a string array. When `schemas` is empty or omitted, consumers MUST treat the profile as **public-only** (backward compatible). When `schemas` is non-empty, connect, introspection, and dump MUST be limited to those schema names.

#### Scenario: Round-trip structured fields

- GIVEN a valid profile with all required fields
- WHEN the profile is saved and loaded
- THEN all fields match the original values

#### Scenario: Empty schemas means public only

- GIVEN a profile with empty or omitted `schemas`
- WHEN introspection or dump uses the profile
- THEN only the `public` schema is loaded or dumped

#### Scenario: Non-empty schemas filters work

- GIVEN a profile with `schemas: [app, billing]`
- WHEN introspection or dump uses the profile
- THEN only tables in `app` and `billing` are included

### Requirement: YAML store file

Profiles MUST persist in a YAML file with file mode `0600` and atomic writes (temp file, then rename). Default path for `connections.scope: project` (default) SHALL be `.dolly.connections.yaml` relative to the config working directory. For `connections.scope: xdg`, the path SHALL be `$XDG_CONFIG_HOME/dolly/connections.yaml` (creating parent dirs as needed). `connections.path` MAY override the resolved path for either scope.

#### Scenario: Missing store file

- GIVEN no store file exists
- WHEN `List` runs
- THEN an empty list is returned without error

#### Scenario: Atomic persist

- GIVEN a save operation succeeds
- WHEN the process crashes mid-write
- THEN the previous store contents remain intact or the new file is complete

#### Scenario: XDG scope path

- GIVEN `connections.scope: xdg` and `save_connections: true`
- WHEN the store resolves its file
- THEN the path is under `$XDG_CONFIG_HOME/dolly/connections.yaml`

### Requirement: ConnectionStore API

The package MUST expose `ConnectionStore` with `List`, `Get`, `Save`, `Put`, `Delete`, and `Rename`. `Put` MUST replace an existing profile by name or return `ErrNotFound` when absent. `List` MUST return profiles sorted by `name`. `Get` MUST return an error when the name is unknown. `Delete` MUST remove the named profile or return an error if absent. `Rename(old, new)` MUST move a profile to a new unique name or return an error if `old` is missing or `new` already exists.

#### Scenario: List ordering

- GIVEN multiple saved profiles
- WHEN `List` is called
- THEN results are sorted ascending by `name`

#### Scenario: Get unknown name

- GIVEN no profile named `missing`
- WHEN `Get("missing")` runs
- THEN an actionable not-found error is returned

#### Scenario: Rename success

- GIVEN a profile `staging` exists and `prod` does not
- WHEN `Rename("staging", "prod")` runs
- THEN `Get("prod")` returns the former `staging` data
- AND `Get("staging")` fails

#### Scenario: Rename duplicate target rejected

- GIVEN profiles `a` and `b` exist
- WHEN `Rename("a", "b")` runs
- THEN rename fails with a duplicate-name error
- AND both original entries remain unchanged

### Requirement: Unique name on explicit save

`Save` for a **new** profile name (save-as) MUST reject when another profile already has the same `name`. The system MUST NOT overwrite via save-as and MUST NOT prompt for overwrite confirmation.

#### Scenario: Duplicate save-as name rejected

- GIVEN a profile `staging` already exists
- WHEN `Save` is called with a different profile also named `staging`
- THEN save fails with a duplicate-name error
- AND the existing `staging` entry is unchanged

### Requirement: Auto-save upsert by connection signature

After a successful TUI connect when `save_connections` is true, the system MUST upsert a profile matching the connection signature `(host, port, database, user)` (port normalized to default `5432` when empty). If a profile matches the signature, the system MUST update connection fields in place and MUST preserve `name` and `schemas`. If no profile matches, the system MUST create one with a generated stable name (e.g. `conn-1`) without prompting. Auto-save MUST NOT clear non-empty `schemas` without an explicit operator action.

#### Scenario: Upsert updates existing signature

- GIVEN a saved profile with matching host/port/database/user
- WHEN auto-save runs after successful TUI connect with a new password
- THEN the matching profile password is updated
- AND `name` and `schemas` are unchanged

#### Scenario: Upsert creates new profile

- GIVEN no profile matches the connection signature
- WHEN auto-save runs after successful TUI connect
- THEN a new profile is created with generated name and current fields

### Requirement: List display summary

List UIs MUST show each profile `name` plus a masked summary of host, user, and database. The password MUST NOT appear in any list UI.

#### Scenario: Masked list row

- GIVEN a saved profile with known host, user, and database
- WHEN the profile appears in a list row
- THEN host, user, and database are masked per display rules
- AND password is not shown

### Requirement: DSN round-trip

The store MUST compose DSNs with URL-encoded credentials, default port `5432`, and profile `sslmode` or `prefer`.
(Previously: DSN composition defaulted unspecified `sslmode` to `disable`.)

#### Scenario: Password encoding

- GIVEN a password has URL-special characters
- WHEN a DSN is composed from the profile
- THEN credentials are URL-encoded

#### Scenario: Profile sslmode controls DSN

- GIVEN profile `sslmode: require`
- WHEN a DSN is composed from the profile
- THEN the DSN contains `sslmode=require`

#### Scenario: Missing sslmode

- GIVEN no profile `sslmode`
- WHEN a DSN is composed from the profile
- THEN the DSN contains `sslmode=prefer`

### Requirement: Secret-safe child process environment

Subprocess execution MUST remove ambient PostgreSQL secret variables.

#### Scenario: Ambient secrets stripped

- GIVEN parent env has `PGPASSWORD` or related secrets
- WHEN clone starts `pg_dump`, `psql`, or equivalent tools
- THEN those variables are absent from child env

### Requirement: Store permission enforcement

Connection-store I/O MUST enforce owner-only permissions before using secrets.

#### Scenario: Unsafe mode handled

- GIVEN a store is group/world-readable
- WHEN the store is opened
- THEN it becomes `0600` or the operation fails safely

### Requirement: Optional encryption

When `connections.encrypt` is `true`, passwords (minimum) MUST be stored encrypted (AES-GCM envelope acceptable; whole-file encryption acceptable). The encryption key MUST come from `DOLLY_CONNECTIONS_KEY` (32-byte key, base64-encoded). When encrypt is true and the key is missing or invalid, load and save MUST fail closed with an actionable error. When `connections.encrypt` is `false`, passwords MAY be stored in plaintext; documentation MUST warn operators not to commit the store file and SHOULD recommend gitignore for `*.connections.yaml`.

#### Scenario: Encrypt round-trip

- GIVEN `connections.encrypt: true` and a valid `DOLLY_CONNECTIONS_KEY`
- WHEN a profile is saved and reloaded
- THEN the password matches the original value
- AND the on-disk YAML does not contain the plaintext password

#### Scenario: Missing key when encrypt enabled

- GIVEN `connections.encrypt: true` and `DOLLY_CONNECTIONS_KEY` is unset
- WHEN the store loads or saves
- THEN the operation fails with a clear key-missing error

#### Scenario: Plaintext when encrypt disabled

- GIVEN `connections.encrypt: false`
- WHEN a profile with password is saved
- THEN the password is readable in YAML on disk

### Requirement: Resolve named profile for CLI and TUI

When `save_connections` is true, `Resolve(name)` (or equivalent) MUST load the profile, compose its DSN, and expose its `schemas` filter for downstream commands. When `save_connections` is false, resolution MUST NOT read the store.

#### Scenario: Resolve for dump

- GIVEN `save_connections: true` and profile `staging` exists
- WHEN `dolly dump --connection staging` runs
- THEN the dump uses the profile DSN and schema filter

#### Scenario: Flag false skips store

- GIVEN `save_connections: false`
- WHEN `dolly dump --connection staging` runs
- THEN resolution does not read the store and fails with an actionable error
