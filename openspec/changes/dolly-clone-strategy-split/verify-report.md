# Verification Report: dolly-clone-strategy-split

status: PASS WITH WARNINGS

executive_summary: Implementation satisfies the change delta: canonical strategy names are wired through `Resolve`, `Name()`, CLI help, TUI help catalog, config comments/templates, docs, and active `dolly-cli` spec. Legacy aliases remain accepted. Runtime execution paths are unchanged aside from canonical naming/error/help surfaces. Residual legacy references in `openspec/specs/dolly-clone-preflight/spec.md` are a warning, not an archive blocker, because the aliases remain valid and preflight receives canonical names after `Resolve`; schedule a follow-up spec cleanup.

artifacts:
- Read proposal, exploration, design, tasks, change spec, and active preflight spec.
- Tasks: all checked (`tasks.md` 1.1–5.5).
- Core evidence: `internal/clone/strategy.go` accepts `logical-stream`/`physical-backup` plus legacy aliases and reports canonical names in errors.
- Name evidence: `CopyStreamStrategy.Name()` returns `logical-stream`; `ReplicationStrategy.Name()` returns `physical-backup`.
- Surface evidence: `cmd/dolly/clone.go`, `cmd/dolly/help.go`, `internal/tui/cli_capabilities.go`, `internal/config/config.jsonc.tmpl`, `config.jsonc`, and `docs/physical-backup.md` use canonical names.
- Spec evidence: `openspec/specs/dolly-cli/spec.md` and archived production-scale delta reflect canonical names.

tests/commands:
- `rtk go test ./internal/clone/...` — exit 0; 167 passed / 1 package.
- `rtk go test ./internal/tui -run Test -count=1` — exit 0; 200 passed / 1 package.
- `rtk go test ./...` — exit 0; 732 passed / 12 packages.
- `rtk go build ./...` — exit 0.
- `rtk go vet ./...` — exit 0.
- `go run ./cmd/dolly clone --help` — exit 0; help lists `template, schema-replay, logical-stream, physical-backup`.
- `go run ./cmd/dolly config show` — exit 0; default strategy remains `schema-replay`.

risks:
- WARNING: `openspec/specs/dolly-clone-preflight/spec.md` still uses `copy-stream` and `production-scale` as strategy labels and includes stale wording around copy-stream preflight scope. Not an archive blocker for this change because aliases are preserved and canonical behavior is specified in `dolly-cli`; it should be cleaned in a follow-up preflight spec amendment.

skill_resolution: Standard SDD verification executor path; no delegation; strict TDD evidence not available/required.
