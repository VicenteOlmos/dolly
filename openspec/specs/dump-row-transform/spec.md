# dump-row-transform Specification

## Purpose

Define the row-level data transformation hook that can modify individual row values during NDJSON dump streaming, and the built-in column-pattern sanitizer that activates when `sanitization.enabled` is true.

## Requirements

### Requirement: RowTransform hook contract

`RowTransform` SHOULD be a function type `func(schema, table string, columns []db.Column, row map[string]any) (map[string]any, error)`. It MUST receive the original row and return a modified row. A nil return MUST abort the dump with an error.

#### Scenario: Transform receives column metadata

- GIVEN a row for table `users` in schema `public` with columns `[id, email]`
- WHEN `RowTransform` is invoked
- THEN the function receives `"public"`, `"users"`, the column descriptors, and the row map
- AND it returns a modified row map

### Requirement: Built-in sensitive-column patterns

The built-in sanitizer MUST replace values in text/varchar columns whose name matches a known sensitive pattern.

| Pattern | Column names matched | Replacement |
|---------|---------------------|-------------|
| email | `email`, `email_address` | `redacted@example.com` |
| password | `password`, `passwd`, `password_hash` | `[REDACTED]` |
| ssn | `ssn`, `social_security`, `social_security_number` | `000-00-0000` |
| phone | `phone`, `phone_number`, `mobile`, `cellphone` | `+1-555-000-0000` |
| credit_card | `credit_card`, `card_number`, `cc_number` | `xxxx-xxxx-xxxx-0000` |
| secret | `secret`, `token`, `api_key`, `api_secret`, `access_token` | `[REDACTED]` |

#### Scenario: Text match replaces value

- GIVEN a column `email` with data_type `character varying` and value `user@example.com`
- WHEN the built-in sanitizer processes the row
- THEN the value is replaced with `redacted@example.com`

#### Scenario: Non-text column with sensitive name is preserved

- GIVEN a column `token` with data_type `integer` and value `42`
- WHEN the built-in sanitizer processes the row
- THEN the value `42` is written unchanged

#### Scenario: Null values stay null

- GIVEN a column `email` with a nil value
- WHEN the built-in sanitizer processes the row
- THEN the output still contains `null` for that column

### Requirement: Disabled sanitization is passthrough

When `sanitization.enabled` is `false` or unset, no `RowTransform` MUST be applied. Output MUST match pre-change behavior exactly.

#### Scenario: Default passthrough

- GIVEN default config with `sanitization.enabled: false`
- WHEN a dump runs
- THEN NDJSON row data is unchanged from pre-sanitization format
