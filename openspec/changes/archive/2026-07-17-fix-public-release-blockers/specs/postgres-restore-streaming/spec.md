# Delta for PostgreSQL Restore Streaming

## MODIFIED Requirements

### Requirement: Restore Artifact Input

The system MUST use metadata `data_file` when present, MUST resolve it only within the dump root, and MUST reject missing or unsafe artifacts before any mutation. If `data_file` is absent, it MUST fall back to legacy `<table>.ndjson`.
(Previously: restore derived an unscoped data filename from table name.)

#### Scenario: Safe metadata path restores
- GIVEN metadata declares an existing relative `data_file` contained by dump root
- WHEN restore starts
- THEN that file is read

#### Scenario: Unsafe artifact references fail before mutation
- GIVEN metadata declares an empty, absolute, backslash-containing, non-clean, `.`, `..`, traversal, duplicate, directory, or symlink-escaping `data_file`
- WHEN restore starts
- THEN restore fails before truncate or insert
- AND no schema/data mutation is attempted

#### Scenario: Legacy metadata remains readable
- GIVEN metadata omits `data_file` and legacy `<table>.ndjson` exists
- WHEN restore starts
- THEN legacy file is read

#### Scenario: Missing artifact fails early
- GIVEN metadata resolves to a missing data file
- WHEN restore starts
- THEN restore returns an artifact error before truncate or inserts

#### Scenario: Validation precedes all data mutation
- GIVEN any declared or legacy artifact is missing or unsafe
- WHEN restore starts
- THEN validation fails before truncate and before any insert

### Requirement: Conflict Policies

The system MUST implement `replace` as one transaction-bound metadata-table-only truncate without `CASCADE`; it MUST fail closed when external foreign-key dependencies make that operation unsafe.
(Previously: replace used truncate semantics that could use `CASCADE`.)

#### Scenario: Replace scopes mutation
- GIVEN replace targets selected metadata tables
- WHEN restore begins
- THEN only those tables are truncated within restore transaction
- AND dependent external tables are not cascaded

#### Scenario: External dependency rejects replace
- GIVEN an external foreign key would prevent safe metadata-only truncation
- WHEN replace begins
- THEN restore fails before mutation

#### Scenario: Replace loads selected rows
- GIVEN replace can safely truncate selected metadata tables
- WHEN restore processes them
- THEN their rows are loaded after truncation

### Requirement: Transactional Atomicity

Default restore MUST reject automatic schema application when schema is missing because external schema execution cannot join its transaction. Explicit non-transactional mode MAY apply schema and MAY retain partial effects; default transactional failure MUST leave schema and data unchanged.
(Previously: restore behavior did not state this transaction boundary.)

#### Scenario: Default missing schema fails closed
- GIVEN target schema is missing and default transactional mode is enabled
- WHEN restore starts
- THEN it rejects automatic schema application
- AND schema and data remain unchanged

#### Scenario: Explicit non-transactional schema application
- GIVEN target schema is missing and explicit non-transactional mode is enabled
- WHEN restore starts
- THEN schema application MAY occur
- AND later failure MAY leave partial effects

#### Scenario: Successful transactional restore commits once
- GIVEN default transactional mode and valid existing schema
- WHEN all tables load successfully
- THEN all changes commit together

### Requirement: Replace Transaction Boundary

The system MUST reject `WithReplace()+WithoutTransaction()` before any mutation, including when invoked through library APIs.

#### Scenario: Replace without transaction fails closed
- GIVEN replace and explicit non-transactional mode are both enabled
- WHEN restore is invoked
- THEN it returns an option error before truncate, insert, or other mutation
