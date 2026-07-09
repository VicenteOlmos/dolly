# Design: Clone Strategy Naming Split

## Technical Approach

Pure naming refactor. No behavior changes. The `Resolve()` switch in `strategy.go` gains canonical-name entries before existing alias entries. `Name()` methods return canonical names. All documentation, config comments, CLI help, and TUI catalog update to reflect the new naming convention.

The change is organized in three layers:
1. **Core**: `Resolve()` switch, `Name()` methods, error message
2. **Surface**: Config comments, CLI help, TUI catalog, docs
3. **Specs**: Update spec requirements and scenarios

## Architecture Decisions

| Decision | Choice | Alternatives | Rationale |
|---|---|---|---|
| Canonical for CopyStreamStrategy | `logical-stream` | `streaming-copy`, `logical-copy` | Describes the mechanism (in-process COPY stream) and contrasts with `physical-backup` (filesystem) |
| Canonical for ReplicationStrategy | `physical-backup` | `pg-basebackup`, `replication`, `cluster-clone` | Describes the outcome (filesystem backup) vs mechanism; `pg-basebackup` is too tool-specific |
| Alias ordering in switch | Canonical first, aliases after | Aliases only | Canonical first documents the primary name inline; tests assert canonical `Name()` |
| `Name()` return | Canonical name | Engine name (status quo) | Enables consistent trace output, help references, and UI display |
| `docs/replication.md` | Rename to `docs/physical-backup.md` | Keep and update | File name should match canonical name; git tracks history |

## Data Flow

```
Resolve("logical-stream")  ──→ strategy.go switch ──→ CopyStreamStrategy{Name() → "logical-stream"}
Resolve("copy-stream")     ──→                          ↗ (alias, same strategy)
Resolve("streaming-copy")  ──→                          ↗ (alias, same strategy)

Resolve("physical-backup") ──→ strategy.go switch ──→ ReplicationStrategy{Name() → "physical-backup"}
Resolve("replication")     ──→                          ↗ (alias, same strategy)
Resolve("production-scale")──→                          ↗ (alias, same strategy)
```

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/clone/strategy.go` | Modify | Add `"logical-stream"` and `"physical-backup"` to switch before aliases; update error message |
| `internal/clone/strategy_copy_stream.go` | Modify | `Name()` returns `"logical-stream"` |
| `internal/clone/strategy_replication.go` | Modify | `Name()` returns `"physical-backup"` |
| `internal/clone/strategy_test.go` | Modify | Update `wantName` for canonical; add alias resolution cases |
| `internal/config/config.go` | Modify | Update default strategy comment |
| `config.jsonc` | Modify | Update strategy descriptions in comments |
| `config.jsonc.tmpl` | Modify | Update strategy descriptions in comments |
| `internal/tui/cli_capabilities.go` | Modify | Update `--strategy` flag Description field |
| `cmd/dolly/clone.go` | Modify | Update `--strategy` flag usage text |
| `docs/physical-backup.md` | Create | Renamed from `docs/replication.md` with canonical names |
| `docs/replication.md` | Delete | Replaced by `docs/physical-backup.md` |
| `openspec/specs/dolly-cli/spec.md` | Modify | Update "Supported clone strategies" requirement |

## Interfaces / Contracts

```go
// CopyStreamStrategy.Name() returns "logical-stream"

// ReplicationStrategy.Name() returns "physical-backup"

// Resolve switch additions:
case "logical-stream", "copy-stream", "streaming-copy":
    return &CopyStreamStrategy{...}, nil
case "physical-backup", "replication", "production-scale":
    return &ReplicationStrategy{...}, nil

// Error message updated to:
// "supported: template, schema-replay, logical-stream, physical-backup"
```

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Canonical `Name()` values | Assert `CopyStreamStrategy{}.Name() == "logical-stream"` and `ReplicationStrategy{}.Name() == "physical-backup"` |
| Unit | All alias resolutions | Table-driven: each alias → expected canonical `Name()` |
| Unit | Unknown strategy error message | Assert error lists `logical-stream` and `physical-backup` |
| Unit | Backward compatibility | Verify configs with old names (`"production-scale"`, `"streaming-copy"`) still resolve |

## Migration / Rollout

No migration required. Old aliases continue to work. Users can switch to canonical names at their convenience. The `CHANGELOG` notes the new canonical names and confirms alias support.

## Open Questions

None. The design is straightforward — the exploration confirmed the codebase structure and all affected locations are identified.
