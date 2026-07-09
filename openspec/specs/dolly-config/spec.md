# dolly-config Specification

## Purpose

Define how dolly loads, parses, and merges configuration from code defaults and an optional `config.jsonc` file at the project root.

## Requirements

### Requirement: Code defaults always apply

The system SHALL maintain a `DefaultConfig()` function providing values for all 24 configuration knobs. `LoadConfig` MUST apply code defaults on every run. Default `clone.name_template` MUST be `{db}_dolly_{n}`. No file on disk is required for the system to start or operate correctly.

#### Scenario: No config file present

- GIVEN no `config.jsonc` exists at the project root
- WHEN any dolly command runs
- THEN defaults from `DefaultConfig()` apply, including `clone.name_template: {db}_dolly_{n}`
- AND the process does not exit with a config-related error

#### Scenario: Fresh checkout runs without init

- GIVEN a clean checkout with no local config file
- WHEN `dolly dump --dsn DSN --output DIR` or `dolly clone -ff` runs
- THEN the run proceeds using code defaults
- AND no "config not found" or "run config init first" message appears

#### Scenario: TUI default clone name

- GIVEN no override and database `app`
- WHEN clone derives a name
- THEN it follows `{db}_dolly_{n}` semantics

### Requirement: JSONC config loading and merge

When `config.jsonc` is present at the project root, `LoadConfig` MUST parse it — stripping `//` line comments, `/* */` block comments, and trailing commas — then merge its keys over `DefaultConfig()`. Keys absent from the file MUST retain code-default values. `config.jsonc` SHALL be the only supported application config file format; `LoadConfig` MUST return an error if the path extension is `.yaml` or `.yml`.

#### Scenario: Partial override applies

- GIVEN `config.jsonc` sets one knob (e.g. `maxConnections`)
- WHEN `LoadConfig` runs
- THEN that knob uses the file value and all others use code defaults

#### Scenario: JSONC with comments and trailing commas

- GIVEN `config.jsonc` contains `// line comment`, `/* block */`, and trailing commas after values
- WHEN `LoadConfig` parses the file
- THEN no parse error occurs and values decode correctly

#### Scenario: Malformed JSONC

- GIVEN `config.jsonc` exists but contains invalid JSON after comment stripping
- WHEN `LoadConfig` runs
- THEN an error is returned identifying `config.jsonc` as the source

#### Scenario: YAML config path is rejected

- GIVEN a path whose extension is `.yaml` or `.yml` is passed to `LoadConfig`
- WHEN `LoadConfig` runs
- THEN it returns an error mentioning `config.jsonc` and does not invoke any YAML parser

### Requirement: Repo ships config.jsonc

The repository MUST include a `config.jsonc` documenting all config keys including `sanitization.*` and `subset.*`. The `sanitization.enabled` key is no longer inert — when `true` it activates the built-in `dump-row-transform`.

#### Scenario: Fresh clone has config.jsonc

- GIVEN a developer clones the dolly repository
- WHEN they list project root files
- THEN `config.jsonc` is present with all 24 knobs documented

#### Scenario: config.jsonc documents sanitization behavior

- GIVEN `config.jsonc` exists at the project root
- WHEN inspected
- THEN the `sanitization.enabled` comment describes its effect on dump output

### Requirement: config init is optional reset only

The `config init` command MAY regenerate `config.jsonc` from the embedded template. Without `--force`, it MUST refuse to overwrite an existing file (exit 1, explanatory message). With `--force`, it MUST overwrite. Running `config init` MUST NOT be required before any other dolly command.

#### Scenario: Init writes template when no file exists

- GIVEN no `config.jsonc` at the project root
- WHEN `dolly config init` runs
- THEN `config.jsonc` is written with all 24 knobs and the process exits 0

#### Scenario: Init refuses to overwrite without --force

- GIVEN `config.jsonc` already exists
- WHEN `dolly config init` runs without `--force`
- THEN the process exits 1 and stderr explains that `--force` is required to overwrite

#### Scenario: Init overwrites with --force

- GIVEN `config.jsonc` already exists with user edits
- WHEN `dolly config init --force` runs
- THEN `config.jsonc` is replaced with the default template

### Requirement: SaveConfig write path

The system MUST provide `SaveConfig(cfg *Config, path string) error` that writes `cfg` to `path`. For JSONC files the system MUST patch the existing document in place so comments and surrounding formatting are preserved; only changed values are updated. The function MUST return an error without writing a partial file if marshalling or the write fails.

#### Scenario: SaveConfig writes valid JSON

- GIVEN a populated `*Config` and a writable file path
- WHEN `SaveConfig` is called
- THEN the file at `path` contains valid JSON (or valid JSONC)
- AND `LoadConfig` can reload it without error
- AND all 24 knobs match the original `cfg` values

#### Scenario: SaveConfig preserves JSONC comments

- GIVEN an existing `config.jsonc` with documentation comments
- WHEN `SaveConfig` updates one or more knob values
- THEN the comments from the original file remain present
- AND the changed values reflect the saved `*Config`

#### Scenario: SaveConfig returns error on write failure

- GIVEN a path in a non-writable directory
- WHEN `SaveConfig` is called
- THEN an error is returned
- AND no partial file is written

#### Scenario: Round-trip reload stability

- GIVEN `SaveConfig` writes to `path` and `LoadConfig` reads it back
- WHEN the round-trip completes
- THEN the reloaded `*Config` is value-equal to the saved one

### Requirement: TUI section entry config

Config MUST include `tui.section_entry` with values `overview` or `inside`. Default MUST be `inside`. When `overview`, section screens (connection without saved list, dump, clone) open in section list mode. When `inside`, they open drilled into the first section.

#### Scenario: section_entry round-trips

- GIVEN config is saved with `"tui": { "section_entry": "overview" }`
- WHEN config is loaded
- THEN `TUI.SectionEntry` is `overview`

### Requirement: Dotenv local profile mapping

When `.env` contains PostgreSQL settings, the system MUST expose profile `local` for clone resolution without the store.

#### Scenario: Dotenv maps to local

- GIVEN `.env` has a valid PostgreSQL connection
- WHEN clone resolves profiles
- THEN a profile named `local` is available

#### Scenario: Missing dotenv

- GIVEN no `.env` file exists
- WHEN configuration loads
- THEN defaults apply without dotenv error

### Requirement: Secret redaction for output paths

Config/profile rendering MUST redact passwords before prompts, TUI, logs, errors, stdout, or stderr.

#### Scenario: Password redacted

- GIVEN config, `.env`, or a profile contains password `secret`
- WHEN user-visible or log output renders it
- THEN `secret` is not present

#### Scenario: Diagnostics remain

- GIVEN rendered data has host, user, database, and password
- WHEN it is redacted
- THEN host, user, and database remain diagnosable
- AND password is hidden

### Requirement: Local secret files ignored

The repository MUST ignore `.env` and local stores. Examples/tests MUST NOT contain real secrets.

#### Scenario: Local secrets ignored

- GIVEN a developer creates `.env` or `.dolly.connections.yaml`
- WHEN git status is inspected
- THEN those files are ignored by default
