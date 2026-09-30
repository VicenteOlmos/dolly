# Pullfrog review rubric

The console review prompt is unset. Each auto-review is dispatched with only this task line:

> Review the pull request and provide feedback.

Pullfrog's own system prompt (persona, tools, Review / IncrementalReview playbook) stays outside this repo. This file is the repo rubric, in the same role as CodeRabbit's `reviews.profile: chill` plus `reviews.path_instructions`: a short global bar, then checks that apply only to the paths the diff touches.

Follow it on Review and IncrementalReview. Do not treat it as a request to edit the code unless the task is AddressReviews or Fix.

## Bar

Comment when the change can mis-dump, mis-restore, mis-clone, drop or widen a privilege, leak a credential or other secret, or fail a test that CI actually runs. A missing test does not hide a data-exposure bug. Skip formatting, naming, comment wording, and drive-by refactors.

One inline comment per defect. Keep the review body in the shape the Review and IncrementalReview playbook already requires, including the reviewed-changes preamble and commit SHA. Do not add a nit list to look thorough.

Each inline comment:

- Start with `blocker`, `major`, or `minor`.
- State the broken behavior in one sentence.
- Give one concrete failing case: a catalog shape, a call order, or an input.
- Name the function that has to change and the regression test that should fail first.
- Show only the corrected condition, SQL predicate, or call order. Do not paste a rewrite of the file.

Do not ask for scheduling, MySQL, incremental sync, exclusion constraints, operator classes, or ordered-set aggregates. Do not ask for `--no-tablespaces`, `session_replication_role`, or `--max-in-list-size` in the TUI. Nullable unique indexes stay fail-closed.

## Re-review

On IncrementalReview, comment only on new or still-broken behavior. If an earlier finding is fixed, do not repeat it. Do not push commits on a review run.

## Path checks

Apply a section only when the diff touches that path.

### `internal/clone/**`

Catalog replay order is part of the behavior. `COMMENT ON POLICY` runs after `CREATE POLICY` and `ENABLE ROW LEVEL SECURITY`. Grants run only when privileges are included. A new catalog query needs an empty `ExpectQuery` in the matching sqlmock helper, or every existing test fails. Do not change a mocked column count unless every mock of that query changes with it.

Type `USAGE` covers enums, domains, and standalone composites (`pg_class.relkind = 'c'`). Table row types (`relkind` other than `c`) are not granted. A NULL type ACL is the built-in `PUBLIC USAGE` default, not a revoke. An explicit revoke on the source must be replayed as `REVOKE USAGE ON TYPE ... FROM PUBLIC`, and an effective `PUBLIC` grant must be replayed so a stricter target default does not drop it.

Schema capture stays `--schema-only --no-owner --no-acl` unless privileges are requested.

### `internal/dump/**` and `internal/restore/**`

Partition parents are omitted from row files and listed in `omitted_partition_parents`. Sequences owned by those parents are still restored. An owner that is neither a restored column nor an omitted parent is an error. Standalone sequences with no owner are skipped.

Skip and upsert need a primary key or a persisted unique key. Policy `error` stays a plain insert. Conflict targets must not include `ALWAYS` identity or generated columns.

`go test -tags=integration` compiles `*_test.go` without the integration tag into the same process as the integration tests. `init` in `internal/dump/sqlmock_test.go` and `internal/restore/sqlmock_test.go` sets `db.SkipRelationAnnotations` true. Do not set that flag false in `TestMain`. A test that needs real `relkind`, partition bounds, or generated columns flips it for that test and restores the previous value in `t.Cleanup`.

### `internal/tui/**`

`internal/tui/dump.go` does not import `internal/dump`, `internal/restore`, or `internal/clone`. Only `dump_run.go` imports dump. Clone execution goes through `internal/clonework`. The worker cap goes through `maxDumpWorkers()`.

Saved connection profiles round-trip channel binding. An empty binding means `require`. `sslmode=disable` omits `channel_binding`.

Clone strategy and on-conflict follow the current config until the clone form sets them. Copying `cfg.Clone.Strategy` or `cfg.Clone.RestoreOnConflict` into the draft makes a later Config-screen edit a no-op, because `clonework` prefers the draft value.

`template` and `physical-backup` refuse to start when sanitization is enabled. The dump-screen sanitize toggle does not apply to clone.

### Tests and docs

A behavior change needs a unit test that fails on the old behavior. Integration coverage belongs in a `//go:build integration` test. This environment often has no `DOLLY_TEST_PG_DSN`; do not treat a skipped local integration test as a defect if the test exists and CI's `postgres-integration` job runs it.

`cmd/dolly/readme_test.go` requires the schema-replay fidelity paragraph to keep its English phrases (`schema-replay`, `trigger`, `materialized-view`, `table data`, `sequence`, `not cloned`, `may fire`, `owners`, `ACL`, `cluster-global`) and the Spanish phrases (`schema-replay`, `disparador`, `vistas materializadas`, `datos de tablas`, `secuencia`, `no se clona`, `pueden ejecutarse`, `propietarios`, `ACL`, `ámbito de clúster`). Do not suggest edits that drop them. Put catalog sentences in that fidelity paragraph, TUI sentences in the `dolly tui` intro, and dump or restore sentences in those sections.
