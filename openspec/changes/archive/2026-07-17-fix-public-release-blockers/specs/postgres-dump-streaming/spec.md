# Delta for PostgreSQL Dump Streaming

## MODIFIED Requirements

### Requirement: Dump Artifact Generation

The system MUST write each table's data to `data/<hex-UTF-8(schema)>.<hex-UTF-8(table)>.ndjson` and MUST declare that relative path as `data_file` in table metadata. Encoding MUST be deterministic and MUST preserve distinct schema/table identities.
(Previously: each table used an unscoped `<table>.ndjson` file.)

#### Scenario: Same table names do not collide
- GIVEN tables with same name in different schemas
- WHEN a dump is written
- THEN each table has distinct encoded `data_file` metadata and data file

#### Scenario: Empty tables are represented
- GIVEN a discovered table has no rows
- WHEN the dump completes
- THEN metadata declares its data file and that file contains no row objects

#### Scenario: Legacy-independent output is deterministic
- GIVEN same schema and table names across repeated dumps
- WHEN artifacts are generated
- THEN their relative `data_file` values are identical
