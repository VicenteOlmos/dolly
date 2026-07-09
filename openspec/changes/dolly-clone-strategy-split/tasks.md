# Tasks: Clone Strategy Naming Split

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 90–150 |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | auto-forecast |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: size-exception
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Core naming + tests + config/docs | PR 1 | Single PR — all changes are tightly coupled naming updates |

## Phase 1: Core Naming

- [x] 1.1 `internal/clone/strategy.go` — Add `"logical-stream"` and `"physical-backup"` to switch before existing aliases; update error message to list canonical names
- [x] 1.2 `internal/clone/strategy_copy_stream.go` — Change `Name()` return to `"logical-stream"`
- [x] 1.3 `internal/clone/strategy_replication.go` — Change `Name()` return to `"physical-backup"`

## Phase 2: Tests

- [x] 2.1 `internal/clone/strategy_test.go` — Update `TestResolve` `wantName` values for canonical names; add explicit alias-resolution cases for all 4 old aliases
- [x] 2.2 `internal/clone/strategy_test.go` — Add test case for unknown strategy error message listing canonical names
- [x] 2.3 Run `go test ./internal/clone/...` and verify all pass

## Phase 3: Config, CLI, TUI

- [x] 3.1 `internal/config/config.go` — Update `DefaultConfig()` strategy comment
- [x] 3.2 `config.jsonc` — Update strategy description comments to list canonical names with aliases
- [x] 3.3 `config.jsonc.tmpl` — Same update as 3.2
- [x] 3.4 `internal/tui/cli_capabilities.go` — Update `--strategy` flag `Description`
- [x] 3.5 `cmd/dolly/clone.go` — Update `--strategy` flag usage text

## Phase 4: Documentation

- [x] 4.1 `docs/replication.md` → `docs/physical-backup.md` — Rename file; update content to reference canonical names; note aliases for backward compat
- [x] 4.2 `openspec/specs/dolly-cli/spec.md` — Update "Supported clone strategies" requirement to list canonical names with alias table
- [x] 4.3 `openspec/changes/dolly-production-scale-clone/specs/dolly-cli/spec.md` — Update delta spec to reflect canonical naming

## Phase 5: Verification

- [x] 5.1 `go test ./...` — All unit tests pass
- [x] 5.2 `go build ./...` — Clean build
- [x] 5.3 `go vet ./...` — No issues
- [x] 5.4 Manual: verify `dolly clone --help` lists canonical names
- [x] 5.5 Manual: verify `dolly config show` strategy default is unchanged (still `schema-replay`)
