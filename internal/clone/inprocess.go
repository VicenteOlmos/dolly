package clone

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// RunInProcess executes clone without spawning pg_dump/psql subprocesses.
// Deprecated: TUI clone should use clonework.Run, which delegates to Run.
// schema-replay and logical-stream map to dump→restore with in-process schema apply.
func RunInProcess(ctx context.Context, opts Options, onProgress func(string)) error {
	if opts.SourceDSN == "" {
		return fmt.Errorf("source DSN is required")
	}
	if opts.CloneName == "" {
		return fmt.Errorf("clone name is required")
	}

	strategy := strings.TrimSpace(opts.Strategy)
	if strategy == "" {
		strategy = "schema-replay"
	}

	switch strategy {
	case "template":
		if onProgress != nil {
			onProgress("strategy: template")
		}
		return (&TemplateStrategy{}).Execute(ctx, opts)
	case "physical-backup", "replication":
		return (&ReplicationStrategy{Runner: opts.CommandRunner}).Execute(ctx, opts)
	case "logical-stream", "schema-replay", "copy-stream", "streaming-copy":
		return inProcessDumpRestore(ctx, opts, onProgress)
	default:
		return fmt.Errorf("unknown clone strategy %q; supported for in-process TUI: template, schema-replay, logical-stream", strategy)
	}
}

func inProcessDumpRestore(ctx context.Context, opts Options, onProgress func(string)) error {
	if onProgress != nil {
		onProgress("strategy: in-process dump-restore")
	}

	targetDSN, err := resolveCloneTargetDSN(opts)
	if err != nil {
		return err
	}

	if !opts.SkipCreate {
		adminDSN, err := RewriteDSN(targetDSN, "postgres")
		if err != nil {
			return fmt.Errorf("build admin DSN: %w", err)
		}
		if onProgress != nil {
			onProgress("creating target database")
		}
		if err := CreateDatabase(ctx, adminDSN, opts.CloneName); err != nil {
			return fmt.Errorf("create target database: %w", err)
		}
	}

	srcDB, err := sqlOpenDB(opts.SourceDSN)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcDB.Close()

	tgtDB, err := sqlOpenDB(targetDSN)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer tgtDB.Close()

	schemas := schemasFromCloneOpts(opts)
	if len(schemas) == 0 {
		return fmt.Errorf("clone requires at least one source schema")
	}
	if onProgress != nil {
		onProgress("schemas: " + strings.Join(schemas, ", "))
		onProgress("applying schema to target")
	}
	if err := applySchemasFunc(ctx, srcDB, tgtDB, schemas); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	dumpDir, err := makeCloneTempDir(opts.DumpDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dumpDir)

	if onProgress != nil {
		onProgress("dumping source data")
	}
	if err := dumpFunc(ctx, srcDB, dumpDir, opts.DumpOpts...); err != nil {
		return fmt.Errorf("dump: %w", err)
	}

	if onProgress != nil {
		onProgress("restoring into target")
	}
	if err := restoreFunc(ctx, tgtDB, dumpDir, opts.RestoreOpts...); err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	if onProgress != nil {
		onProgress("restoring sequences")
	}
	if err := restoreSequencesFunc(ctx, srcDB, tgtDB); err != nil {
		return fmt.Errorf("restore sequences: %w", err)
	}

	return nil
}

var applySchemasFunc = ApplySchemasFromSource

func resolveCloneTargetDSN(opts Options) (string, error) {
	targetDSN := opts.TargetDSN
	var err error
	if targetDSN == "" {
		targetDSN, err = RewriteDSN(opts.SourceDSN, opts.CloneName)
		if err != nil {
			return "", fmt.Errorf("build target DSN: %w", err)
		}
		return targetDSN, nil
	}
	return RewriteDSN(targetDSN, opts.CloneName)
}

// SchemasFromOptions returns schema filters configured on clone options.
func SchemasFromOptions(opts Options) []string {
	return schemasFromCloneOpts(opts)
}

func schemasFromCloneOpts(opts Options) []string {
	if names := dump.InspectSchemas(opts.DumpOpts...); len(names) > 0 {
		return names
	}
	return restore.InspectSchemas(opts.RestoreOpts...)
}

func makeCloneTempDir(base string) (string, error) {
	if base == "" {
		dir, err := os.MkdirTemp("", "dolly-clone-*")
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}
		return dir, nil
	}
	dir, err := os.MkdirTemp(base, "dolly-clone-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	return dir, nil
}
