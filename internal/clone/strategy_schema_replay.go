package clone

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/VicenteOlmos/dolly/internal/dump"
)

// lookPath is overridable for testing precondition checks.
var lookPath = exec.LookPath

// SchemaReplayStrategy replays schema via pg_dump --schema-only | psql,
// then dumps and restores data.
type SchemaReplayStrategy struct {
	Runner CommandRunner
}

func (s *SchemaReplayStrategy) Name() string { return "schema-replay" }

func (s *SchemaReplayStrategy) Execute(ctx context.Context, opts Options) error {
	if opts.SourceDSN == "" {
		return fmt.Errorf("source DSN is required")
	}
	if opts.CloneName == "" {
		return fmt.Errorf("clone name is required")
	}

	startedAt := time.Now()
	totalSteps := 5

	targetDSN := opts.TargetDSN
	var err error
	if targetDSN == "" {
		targetDSN, err = RewriteDSN(opts.SourceDSN, opts.CloneName)
		if err != nil {
			return fmt.Errorf("build target DSN: %w", err)
		}
	} else {
		targetDSN, err = RewriteDSN(targetDSN, opts.CloneName)
		if err != nil {
			return fmt.Errorf("build target DSN: %w", err)
		}
	}

	step := 0

	if !opts.SkipCreate {
		adminDSN, err := RewriteDSN(targetDSN, "postgres")
		if err != nil {
			return fmt.Errorf("build admin DSN: %w", err)
		}
		step++
		reportProgressEvent(opts, ProgressEvent{
			Phase:   "creating_target",
			Step:    "creating target database",
			Current: step,
			Total:   totalSteps,
			Elapsed: time.Since(startedAt),
		})
		if err := CreateDatabase(ctx, adminDSN, opts.CloneName); err != nil {
			return fmt.Errorf("create target database: %w", err)
		}
		// Run the remaining steps; if they fail, clean up the database
		// we just created.
		if err := s.postCreate(ctx, opts, targetDSN, startedAt, totalSteps, step); err != nil {
			if dropErr := dropDatabaseFunc(ctx, adminDSN, opts.CloneName); dropErr != nil {
				fmt.Fprintf(os.Stderr, "warning: cleanup drop database %q failed: %v (original error: %v)\n", opts.CloneName, dropErr, err)
			}
			return err
		}
		return nil
	}
	return s.postCreate(ctx, opts, targetDSN, startedAt, totalSteps, step)
}

// postCreate handles all steps after database creation (or when SkipCreate is set).
func (s *SchemaReplayStrategy) postCreate(ctx context.Context, opts Options, targetDSN string, startedAt time.Time, totalSteps, currentStep int) error {
	step := currentStep

	// Replay schema: pg_dump --schema-only --no-owner --no-acl | psql target
	runner := commandRunnerForProgress(s.Runner, opts.ProgressFn)

	srcCleanDSN, srcPw := StripPassword(opts.SourceDSN)
	tgtCleanDSN, tgtPw := StripPassword(targetDSN)
	if srcPw != "" && tgtPw != "" && srcPw != tgtPw {
		return fmt.Errorf("source and target DSNs have different passwords: schema-replay pipe shares a single PGPASSWORD environment; use matching credentials or connect via ~/.pgpass")
	}
	srcArgs := []string{"--schema-only", "--no-owner", "--no-acl", srcCleanDSN}
	tgtArgs := []string{"-v", "ON_ERROR_STOP=1", tgtCleanDSN}
	env := map[string]string{}
	if srcPw != "" {
		env["PGPASSWORD"] = srcPw
	} else if tgtPw != "" {
		env["PGPASSWORD"] = tgtPw
	}
	step++
	reportProgressEvent(opts, ProgressEvent{
		Phase:   "replaying_schema",
		Step:    "replaying schema",
		Current: step,
		Total:   totalSteps,
		Elapsed: time.Since(startedAt),
	})
	if err := runner.PipeWithEnv(ctx, env, "pg_dump", srcArgs, "psql", tgtArgs); err != nil {
		return fmt.Errorf("replay schema: %w", err)
	}

	// Dump and restore data using existing pipeline.
	var dumpDir string
	var err error
	if opts.DumpDir == "" {
		dumpDir, err = os.MkdirTemp("", "dolly-clone-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
	} else {
		dumpDir, err = os.MkdirTemp(opts.DumpDir, "dolly-clone-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
	}
	defer os.RemoveAll(dumpDir)

	srcDB, err := sqlOpenDB(opts.SourceDSN)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcDB.Close()

	step++
	reportProgressEvent(opts, ProgressEvent{
		Phase:   "dumping",
		Step:    "dumping source data",
		Current: step,
		Total:   totalSteps,
		Elapsed: time.Since(startedAt),
	})
	dumpOpts := opts.DumpOpts
	if dump.InspectSchemas(dumpOpts...) == nil {
		schemaNames, err := listSchemaNamesFunc(ctx, srcDB)
		if err != nil {
			return fmt.Errorf("list schemas: %w", err)
		}
		dumpOpts = append(append([]dump.Option(nil), dumpOpts...), dump.WithSchemas(schemaNames))
	}
	if err := dumpFunc(ctx, srcDB, dumpDir, dumpOpts...); err != nil {
		return fmt.Errorf("dump: %w", err)
	}

	tgtDB, err := sqlOpenDB(targetDSN)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer tgtDB.Close()

	step++
	reportProgressEvent(opts, ProgressEvent{
		Phase:   "restoring",
		Step:    "restoring into target",
		Current: step,
		Total:   totalSteps,
		Elapsed: time.Since(startedAt),
	})
	if err := restoreFunc(ctx, tgtDB, dumpDir, opts.RestoreOpts...); err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	// Restore sequence values so serial/identity-backed tables won't duplicate IDs.
	step++
	reportProgressEvent(opts, ProgressEvent{
		Phase:   "restoring_sequences",
		Step:    "restoring sequences",
		Current: step,
		Total:   totalSteps,
		Elapsed: time.Since(startedAt),
	})
	if err := restoreSequencesFunc(ctx, srcDB, tgtDB); err != nil {
		return fmt.Errorf("restore sequences: %w", err)
	}

	return nil
}

// Ensure SchemaReplayStrategy implements Strategy.
var _ Strategy = (*SchemaReplayStrategy)(nil)
