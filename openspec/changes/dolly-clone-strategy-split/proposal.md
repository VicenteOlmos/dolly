# Proposal: Clone Strategy Naming Split

## Intent

Clone strategies have inconsistent naming: `copy-stream`/`streaming-copy` (same strategy) and `replication`/`production-scale` (same strategy). Users cannot tell which name is canonical, and `production-scale` historically implied logical streaming but now maps to physical backup. This change clarifies identity: `logical-stream` for db-to-db COPY streaming, `physical-backup` for pg_basebackup — while keeping all existing aliases working.

## Scope

### In Scope
- Add canonical name `logical-stream` for `CopyStreamStrategy` (aliases: `copy-stream`, `streaming-copy`)
- Add canonical name `physical-backup` for `ReplicationStrategy` (aliases: `replication`, `production-scale`)
- Update `Strategy.Name()` to return canonical names
- Update `Resolve()` error message to list canonical names as primary
- Update `config.jsonc`, `config.jsonc.tmpl`, `internal/config/config.go` help comments
- Update `internal/tui/cli_capabilities.go` flag description
- Update `cmd/dolly/clone.go` `--strategy` flag help text
- Update strategy tests to assert canonical names and all alias paths
- Rename `docs/replication.md` → `docs/physical-backup.md` with updated content
- Update `openspec/specs/dolly-cli/spec.md` requirement for supported clone strategies
- Update `openspec/changes/dolly-production-scale-clone/specs/dolly-cli/spec.md` delta spec

### Out of Scope
- Removing old aliases (kept for backward compatibility)
- Changing any strategy behavior or execution logic
- Adding new clone strategies
- Changing template or schema-replay naming

## Capabilities

### New Capabilities
- `clone-naming-standard`: Canonical naming convention for clone strategies with documented alias resolution.

### Modified Capabilities
- `dolly-cli`: Strategy flag help and supported-strategy requirement updated to canonical names.
- `dolly-tui`: Strategy field/cli-catalog descriptions updated.

## Approach

1. Add `"logical-stream"` and `"physical-backup"` as the first match in their respective switch cases in `Resolve()`.
2. Change `CopyStreamStrategy.Name()` to return `"logical-stream"`.
3. Change `ReplicationStrategy.Name()` to return `"physical-backup"`.
4. Update all help text, config comments, and docs to list canonical names with aliases noted.
5. Rename `docs/replication.md` to `docs/physical-backup.md` and update content.
6. Update spec references across `dolly-cli` specs.
7. Update tests: verify canonical `Name()`, verify all aliases still resolve.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/clone/strategy.go` | Modified | Add canonical names to switch; update error message |
| `internal/clone/strategy_copy_stream.go` | Modified | `Name()` returns `"logical-stream"` |
| `internal/clone/strategy_replication.go` | Modified | `Name()` returns `"physical-backup"` |
| `internal/clone/strategy_test.go` | Modified | Update `Name()` assertions; add alias tests |
| `internal/config/config.go` | Modified | Update DefaultConfig strategy default comment |
| `config.jsonc` | Modified | Update strategy descriptions |
| `config.jsonc.tmpl` | Modified | Update strategy descriptions |
| `internal/tui/cli_capabilities.go` | Modified | Update `--strategy` flag description |
| `cmd/dolly/clone.go` | Modified | Update `--strategy` flag help text |
| `docs/replication.md` | Renamed | → `docs/physical-backup.md` |
| `openspec/specs/dolly-cli/spec.md` | Modified | Update supported strategies requirement |
| `openspec/changes/dolly-production-scale-clone/specs/dolly-cli/spec.md` | Modified | Update delta spec |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Old alias usage in scripts/configs | Low | Keep all aliases working; no breaking change |
| `Name()` change breaks log parsing | Low | Document in CHANGELOG; both names appear in help |
| `docs/replication.md` external links | Very Low | Leave a forwarding note in old location or git tracks rename |

## Rollback Plan

Revert `Name()` to original values, remove canonical name entries from switch, revert docs/config. All aliases remain functional throughout. No data or behavior impact.

## Dependencies

None. This is a pure naming/documentation change with no behavior modifications.

## Success Criteria

- [ ] `Resolve("logical-stream")` returns `CopyStreamStrategy` and `Name()` returns `"logical-stream"`
- [ ] `Resolve("physical-backup")` returns `ReplicationStrategy` and `Name()` returns `"physical-backup"`
- [ ] All old aliases (`copy-stream`, `streaming-copy`, `replication`, `production-scale`) continue to resolve correctly
- [ ] Config, CLI help, TUI catalog, and docs list canonical names as primary
- [ ] All existing tests pass without modification (except `Name()` assertions)
