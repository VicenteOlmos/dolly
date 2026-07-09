# Exploration: Clone Strategy Naming Split

## Current State

The `internal/clone/strategy.go` `Resolve()` function supports four strategies with inconsistent naming:

| Engine name (code) | User-facing alias(es) | Go type | Behavior |
|---|---|---|---|
| `template` | `template` | `TemplateStrategy` | Same-instance `CREATE DATABASE … TEMPLATE` |
| `schema-replay` | `schema-replay` | `SchemaReplayStrategy` | `pg_dump --schema-only` + dump/restore data |
| `copy-stream` | `copy-stream`, `streaming-copy` | `CopyStreamStrategy` | In-process COPY streaming, no NDJSON intermediate |
| `replication` | `replication`, `production-scale` | `ReplicationStrategy` | `pg_basebackup` physical cluster clone |

Two problems:
1. **No canonical user-visible name** — `copy-stream`/`streaming-copy` and `replication`/`production-scale` are aliased without a primary identity. `Name()` returns the engine name, not the canonical user-facing name.
2. **`production-scale` maps to physical backup** — the user's original `production-scale` intent was logical streaming for huge data without gigabyte dumps. After `dolly-production-scale-clone`, it maps to `pg_basebackup`. This is now correct per user agreement, but must be documented clearly.

## Affected Areas

| Area | Why affected |
|---|---|
| `internal/clone/strategy.go` | `Resolve()` needs canonical names added; error message updated |
| `internal/clone/strategy_copy_stream.go` | `Name()` should return canonical `"logical-stream"` |
| `internal/clone/strategy_replication.go` | `Name()` should return canonical `"physical-backup"` |
| `internal/clone/strategy_test.go` | Tests assert `Name()` values and alias resolution |
| `internal/config/config.go` | Default strategy help comments |
| `config.jsonc` | Strategy descriptions in comments |
| `config.jsonc.tmpl` | Strategy descriptions in template comments |
| `internal/tui/cli_capabilities.go` | `--strategy` flag description lists user-facing names |
| `cmd/dolly/clone.go` | `--strategy` flag help text |
| `docs/replication.md` | Renamed to `docs/physical-backup.md` or updated to reference canonical names |
| `openspec/specs/dolly-cli/spec.md` | "Supported clone strategies" requirement needs canonical names |
| `openspec/changes/dolly-production-scale-clone/specs/dolly-cli/spec.md` | Delta spec may need updating |

## Approaches

1. **Add canonical names + update docs only** — Add `logical-stream` and `physical-backup` as primary names, keep all aliases, update help/docs/config, but don't change `Name()` return values.
   - Pros: Minimal code change; full backward compat
   - Cons: `Name()` still returns engine name; trace output shows old names
   - Effort: Low

2. **Full rename with backward-compat aliases** — Add canonical names, change `Name()` to return canonical names, update all aliases in switch, update all docs/config/help/specs.
   - Pros: Consistent trace output, help, and docs; unambiguous identity
   - Cons: Slightly more code change; tests need updating for `Name()` assertions
   - Effort: Medium

3. **Deprecate old aliases** — Same as (2) but remove old aliases with a deprecation notice.
   - Pros: Cleanest long-term
   - Cons: Breaking change for anyone using old names in config/scripts
   - Effort: Medium

## Recommendation

**Approach 2: Full rename with backward-compat aliases.** Add canonical names (`logical-stream`, `physical-backup`) as the primary identity. Keep all existing aliases working for backward compatibility. Change `Name()` to return canonical names. Update all docs, config help, TUI catalog, and spec references. This is a naming clarification, not a behavior change — no user workflows break, but the system becomes self-documenting.

## Risks

- Low: Old aliases still work, but `Name()` changes may affect log parsing if anyone depends on exact strings. Mitigation: document the change and keep aliases.
- Low: `docs/replication.md` is linked externally? Unlikely for a Go CLI tool. Rename to `docs/physical-backup.md` and leave a symlink or redirect note.
- None: Behavior is unchanged — only names and documentation.

## Ready for Proposal

Yes. The codebase is well-understood. The naming ambiguity is the only concern. Approach 2 is safe, backward-compatible, and clarifies the system.
