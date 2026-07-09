# Delta for dolly-cli

## MODIFIED Requirements

### Requirement: Supported clone strategies

The system SHALL define these user-visible strategies: `template`, `schema-replay`, `logical-stream`, and `physical-backup`. Each strategy MUST have a canonical name returned by `Strategy.Name()`. The system MUST accept these backward-compatible aliases in addition to canonical names:

| Canonical | Aliases |
|-----------|---------|
| `logical-stream` | `copy-stream`, `streaming-copy` |
| `physical-backup` | `replication`, `production-scale` |

Error messages and help text MUST list canonical names as the primary options. Aliases MAY be documented in parentheses or footnotes.
(Previously: listed `streaming-copy` and `production-scale` as primary user-visible names without canonical distinction.)

#### Scenario: Canonical name resolution

- GIVEN the operator passes `--strategy logical-stream`
- WHEN `Resolve` runs
- THEN `CopyStreamStrategy` is returned
- AND `Name()` returns `"logical-stream"`

#### Scenario: Canonical name physical-backup

- GIVEN the operator passes `--strategy physical-backup`
- WHEN `Resolve` runs
- THEN `ReplicationStrategy` is returned
- AND `Name()` returns `"physical-backup"`

#### Scenario: Backward-compatible alias resolution

- GIVEN the operator passes `--strategy streaming-copy` or `--strategy copy-stream`
- WHEN `Resolve` runs
- THEN `CopyStreamStrategy` is returned

- GIVEN the operator passes `--strategy production-scale` or `--strategy replication`
- WHEN `Resolve` runs
- THEN `ReplicationStrategy` is returned

#### Scenario: Error message uses canonical names

- GIVEN the operator passes an unknown strategy name
- WHEN `Resolve` returns an error
- THEN the error message lists `logical-stream` and `physical-backup` (not aliases) as primary supported names

### Requirement: Interactive strategy choice

The clone strategy prompt SHALL list canonical names in help text. The TUI strategy field SHALL accept canonical names and aliases interchangeably.
(Previously: listed `streaming-copy` and `production-scale` without canonical naming.)

#### Scenario: TUI field accepts aliases

- GIVEN the operator enters `production-scale` in the TUI strategy field
- WHEN clone runs
- THEN it resolves to `physical-backup` and executes the ReplicationStrategy

#### Scenario: Help page lists canonical names

- GIVEN the operator views CLI help in the TUI
- WHEN the clone command description renders
- THEN `logical-stream` and `physical-backup` appear as primary strategy options
