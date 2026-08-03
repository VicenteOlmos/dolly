package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/VicenteOlmos/dolly/internal/brand"
)

var errHelp = errors.New("help requested")

func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func printRootUsage() {
	fmt.Fprintln(os.Stderr, brand.Header())
	fmt.Fprintln(os.Stderr, "usage: dolly <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  dump     export PostgreSQL database to NDJSON (see 'dolly dump list')")
	fmt.Fprintln(os.Stderr, "  restore  load dump artifacts into PostgreSQL")
	fmt.Fprintln(os.Stderr, "  tui      interactive terminal cockpit")
	fmt.Fprintln(os.Stderr, "  clone    interactive dump + restore")
	fmt.Fprintln(os.Stderr, "  config   manage config.jsonc (run 'dolly config --help')")
	fmt.Fprintln(os.Stderr, "  update   install the latest stable release")
	fmt.Fprintln(os.Stderr, "  version  print build version")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run 'dolly <command> --help' for command-specific flags.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Saved connections (config.jsonc):")
	fmt.Fprintln(os.Stderr, "  save_connections   enable saved profiles (default false)")
	fmt.Fprintln(os.Stderr, "  connections.scope  project (default) or xdg ($XDG_CONFIG_HOME/dolly/connections.yaml)")
	fmt.Fprintln(os.Stderr, "  connections.path   optional store file override")
	fmt.Fprintln(os.Stderr, "  connections.encrypt  AES-256-GCM store; set DOLLY_CONNECTIONS_KEY (32 bytes, standard base64)")
	fmt.Fprintln(os.Stderr, "  connections.default  saved profile pre-selected in the TUI connect screen")
}

func printDumpUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly dump [flags]")
	fmt.Fprintln(os.Stderr, "       dolly dump list [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --dsn string")
	fmt.Fprintln(os.Stderr, "        PostgreSQL connection string (required unless --connection or env fallback)")
	fmt.Fprintln(os.Stderr, "  --connection string")
	fmt.Fprintln(os.Stderr, "        saved connection profile name (requires save_connections in config.jsonc)")
	fmt.Fprintln(os.Stderr, "  --output string")
	fmt.Fprintln(os.Stderr, "        output directory (required unless config dump.output_dir)")
	fmt.Fprintln(os.Stderr, "  --schemas string")
	fmt.Fprintln(os.Stderr, "        comma-separated source schema names (overrides saved profile and dump.schemas; default public)")
	fmt.Fprintln(os.Stderr, "        Refuses when selected schema scope contains no tables (fail closed; no metadata written)")
	fmt.Fprintln(os.Stderr, "  --no-transaction")
	fmt.Fprintln(os.Stderr, "        skip read-only transaction wrapper (recommended for large subset closures)")
	fmt.Fprintln(os.Stderr, "  --slow-connection")
	fmt.Fprintln(os.Stderr, "        chunk tables by primary key for slow/unstable connections (forces --no-transaction)")
	fmt.Fprintln(os.Stderr, "        Incompatible with --percent and --seed-file (subset dump modes)")
	fmt.Fprintln(os.Stderr, "  --chunk-size int")
	fmt.Fprintln(os.Stderr, "        rows per chunk in slow-connection mode (default: config slow_chunk_size or 1000)")
	fmt.Fprintln(os.Stderr, "  --retry-max int")
	fmt.Fprintln(os.Stderr, "        max query retries per chunk in slow-connection mode (0 = disabled)")
	fmt.Fprintln(os.Stderr, "  --retry-base string")
	fmt.Fprintln(os.Stderr, "        base backoff between slow-connection retries (default: config or 500ms)")
	fmt.Fprintln(os.Stderr, "  --seed-file string")
	fmt.Fprintln(os.Stderr, "        JSON seed file for subset dump (omit for full-schema dump)")
	fmt.Fprintln(os.Stderr, "  --percent int")
	fmt.Fprintln(os.Stderr, "        percent-based subset dump (1-100). Selects recent root rows, then FK closure")
	fmt.Fprintln(os.Stderr, "        adds required related rows (output may exceed the percentage)")
	fmt.Fprintln(os.Stderr, "        Conflicts with --seed-file. Config default: subset.percent")
	fmt.Fprintln(os.Stderr, "  --max-depth int")
	fmt.Fprintln(os.Stderr, "        subset max FK closure depth (default 10)")
	fmt.Fprintln(os.Stderr, "  --max-tables int")
	fmt.Fprintln(os.Stderr, "        subset max tables in closure (default 50)")
	fmt.Fprintln(os.Stderr, "  --max-rows int")
	fmt.Fprintln(os.Stderr, "        subset max rows read during planning (default 100000)")
	fmt.Fprintln(os.Stderr, "  --max-rows-per-table int")
	fmt.Fprintln(os.Stderr, "        subset max rows exported per table (0 = unlimited). Config default: subset.max_rows_per_table")
	fmt.Fprintln(os.Stderr, "  --max-in-list-size int")
	fmt.Fprintln(os.Stderr, "        subset max values per IN/ANY batch (default 500)")
	fmt.Fprintln(os.Stderr, "  --include-table string")
	fmt.Fprintln(os.Stderr, "        exact qualified table to include (repeatable; narrows dump; no globs or CSV)")
	fmt.Fprintln(os.Stderr, "  --exclude-table string")
	fmt.Fprintln(os.Stderr, "        exact qualified table to exclude (repeatable; wins over include; no globs or CSV)")
	fmt.Fprintln(os.Stderr, "  --include-table-file string")
	fmt.Fprintln(os.Stderr, "        newline-delimited include table file (repeatable; # comments and blank lines ignored)")
	fmt.Fprintln(os.Stderr, "  --exclude-table-file string")
	fmt.Fprintln(os.Stderr, "        newline-delimited exclude table file (repeatable; # comments and blank lines ignored)")
	fmt.Fprintln(os.Stderr, "  --chunk-table string")
	fmt.Fprintln(os.Stderr, "        exact qualified table to stream with keyset chunking (repeatable; no globs or CSV)")
	fmt.Fprintln(os.Stderr, "  --chunk-table-file string")
	fmt.Fprintln(os.Stderr, "        newline-delimited chunk table file (repeatable; # comments and blank lines ignored)")
	fmt.Fprintln(os.Stderr, "  --workers int")
	fmt.Fprintln(os.Stderr, "        parallel table dump workers (default: config dump.workers or 1; max 16)")
	fmt.Fprintln(os.Stderr, "        Incompatible with --no-transaction, --slow-connection, chunk/subset modes")
	fmt.Fprintln(os.Stderr, "  --json")
	fmt.Fprintln(os.Stderr, "        emit machine-readable JSON result to stdout (success only; errors still exit non-zero)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Config (config.jsonc):")
	fmt.Fprintln(os.Stderr, "  sanitization.enabled  redact sensitive column values in NDJSON output (default false)")
	fmt.Fprintln(os.Stderr, "  dump.include_tables / dump.exclude_tables  exact qualified table selectors (replaced by CLI flags when set)")
	fmt.Fprintln(os.Stderr, "  dump.schemas  JSON string array of source schema names (default public; CLI --schemas overrides)")
	fmt.Fprintln(os.Stderr, "  dump.include_table_files / dump.exclude_table_files  newline-delimited selector files")
	fmt.Fprintln(os.Stderr, "  dump.chunk_tables / dump.chunk_table_files  keyset-chunked table selectors (replaced by CLI flags when set)")
	fmt.Fprintln(os.Stderr, "  dump.workers  parallel table dump workers (default 1; CLI --workers overrides)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "schema.sql is captured when pg_dump is on PATH and sanitized for cross-version restore compatibility.")
}

func printRestoreUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly restore [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --dsn string")
	fmt.Fprintln(os.Stderr, "        PostgreSQL connection string (required unless --connection)")
	fmt.Fprintln(os.Stderr, "  --connection string")
	fmt.Fprintln(os.Stderr, "        saved connection profile name (requires save_connections in config.jsonc)")
	fmt.Fprintln(os.Stderr, "  --input string")
	fmt.Fprintln(os.Stderr, "        numbered dump directory, e.g. dolly_dump/1 (see .dolly/dump-history.json or TUI dump History)")
	fmt.Fprintln(os.Stderr, "        Refuses dumps with zero tables before schema replay, data load, or sequence work")
	fmt.Fprintln(os.Stderr, "  --on-conflict string")
	fmt.Fprintln(os.Stderr, "        row conflict policy: error, skip, upsert (default \"error\")")
	fmt.Fprintln(os.Stderr, "  --replace")
	fmt.Fprintln(os.Stderr, "        truncate tables before insert (destructive)")
	fmt.Fprintln(os.Stderr, "  --no-transaction")
	fmt.Fprintln(os.Stderr, "        advanced: commit after each table (no global rollback); requires --yes")
	fmt.Fprintln(os.Stderr, "        Use only for trusted clean targets or very large restores; default is atomic")
	fmt.Fprintln(os.Stderr, "  --trust-schema-sql")
	fmt.Fprintln(os.Stderr, "        replay reviewed schema.sql when target tables are missing; requires --no-transaction --yes")
	fmt.Fprintln(os.Stderr, "  --yes")
	fmt.Fprintln(os.Stderr, "        confirm destructive or advanced operations (required with --replace, --no-transaction, or --trust-schema-sql)")
	fmt.Fprintln(os.Stderr, "  --workers int")
	fmt.Fprintln(os.Stderr, "        parallel table restore workers (default: config restore.workers or 1; max 16)")
	fmt.Fprintln(os.Stderr, "        Values above 1 require --no-transaction --yes --ack-partial-state and conflict policy error")
	fmt.Fprintln(os.Stderr, "  --ack-partial-state")
	fmt.Fprintln(os.Stderr, "        acknowledge partial-state risk for parallel restore (required when workers > 1; never stored in config)")
	fmt.Fprintln(os.Stderr, "  --partial-state-file string")
	fmt.Fprintln(os.Stderr, "        partial-state manifest path (default: config restore.partial_state_file or input/.dolly-restore-partial-state.json)")
	fmt.Fprintln(os.Stderr, "  --json")
	fmt.Fprintln(os.Stderr, "        emit machine-readable JSON result to stdout (success only; errors still exit non-zero)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Config (config.jsonc):")
	fmt.Fprintln(os.Stderr, "  restore.workers  parallel table restore workers (default 1; CLI --workers overrides when set)")
	fmt.Fprintln(os.Stderr, "  restore.partial_state_file  partial-state manifest path (CLI --partial-state-file overrides when set)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "schema.sql is never applied unless --trust-schema-sql is set for a reviewed artifact.")
}

func printCloneUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly clone [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --ff")
	fmt.Fprintln(os.Stderr, "        fast-forward: skip prompts and use config defaults")
	fmt.Fprintln(os.Stderr, "  --strategy string")
	fmt.Fprintln(os.Stderr, "        clone strategy: template, schema-replay, logical-stream (large single-DB), physical-backup")
	fmt.Fprintln(os.Stderr, "  --target-dir string")
	fmt.Fprintln(os.Stderr, "        target data directory for physical-backup clone (pg_basebackup -D)")
	fmt.Fprintln(os.Stderr, "  --connection string")
	fmt.Fprintln(os.Stderr, "        saved connection profile as source (requires save_connections; use with -ff)")
	fmt.Fprintln(os.Stderr, "  --schemas string")
	fmt.Fprintln(os.Stderr, "        comma-separated source schema names (overrides clone.schemas config)")
	fmt.Fprintln(os.Stderr, "  --yes")
	fmt.Fprintln(os.Stderr, "        confirm destructive operations (required with -ff when clone.replace=true)")
	fmt.Fprintln(os.Stderr, "  --json")
	fmt.Fprintln(os.Stderr, "        emit machine-readable JSON result to stdout (success only; errors still exit non-zero)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Strategy families:")
	fmt.Fprintln(os.Stderr, "  Logical single-DB: template, schema-replay, logical-stream (recommended for large cross-server clones)")
	fmt.Fprintln(os.Stderr, "  Physical cluster:  physical-backup (pg_basebackup — copies the entire cluster directory)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Reads config.jsonc for env.*, clone.target_url,")
	fmt.Fprintln(os.Stderr, "clone.name_template, clone.schemas, clone.replace, clone.restore_on_conflict,")
	fmt.Fprintln(os.Stderr, "clone.strategy, clone.target_dir, sanitization.enabled, and related keys.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "physical-backup runs pg_basebackup to create a physical replica data directory.")
	fmt.Fprintln(os.Stderr, "Use --target-dir (or clone.target_dir with -ff) for an empty or non-existent path.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Note: sanitization applies to schema-replay dump paths only; logical-stream")
	fmt.Fprintln(os.Stderr, "and template strategies do not redact row data.")
}

func printVersionUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly version [--json]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Print the dolly build version.")
	fmt.Fprintln(os.Stderr, "  --json  emit machine-readable JSON")
}

func printTUIUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly tui")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Starts the interactive terminal cockpit. No flags; configure via config.jsonc.")
	fmt.Fprintln(os.Stderr, "Requires an interactive terminal (stdout must be a TTY).")
}

func mapFlagHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return errHelp
	}
	return err
}
