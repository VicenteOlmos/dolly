package clone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ReplicationStrategy runs pg_basebackup for physical-backup clones.
type ReplicationStrategy struct {
	Runner CommandRunner
}

func (s *ReplicationStrategy) Name() string { return "physical-backup" }

func (s *ReplicationStrategy) Execute(ctx context.Context, opts Options) error {
	if opts.SourceDSN == "" {
		return fmt.Errorf("source DSN is required")
	}
	if opts.TargetDir == "" {
		return fmt.Errorf("target directory is required for physical-backup clone")
	}

	startedAt := time.Now()

	comps, err := DecomposeDSN(opts.SourceDSN)
	if err != nil {
		return fmt.Errorf("parse source DSN: %w", err)
	}

	// ponytail: never write password to postgresql.auto.conf — plaintext is a security risk.
	// Replication connections must authenticate via ~/.pgpass instead.
	pwForEnv := comps.Password
	if comps.Password != "" {
		fmt.Fprintf(os.Stderr, "warning: password found in source DSN for physical-backup clone; it will NOT be written to postgresql.auto.conf. Ensure the replica has a matching entry in ~/.pgpass.\n")
		comps.Password = "" // strip so BuildPrimaryConninfo omits it
	}

	runner := commandRunnerForProgress(s.Runner, opts.ProgressFn)
	args := []string{
		"-h", comps.Host,
		"-p", comps.Port,
		"-U", comps.User,
		"-D", opts.TargetDir,
		"-Fp", "-Xs", "-P", "-v",
	}
	var env map[string]string
	if pwForEnv != "" {
		env = map[string]string{"PGPASSWORD": pwForEnv}
	}

	reportProgressEvent(opts, ProgressEvent{
		Phase:   "running_pg_basebackup",
		Step:    "running pg_basebackup...",
		Current: 1,
		Total:   1,
		Elapsed: time.Since(startedAt),
	})
	if err := runner.RunWithEnv(ctx, env, "pg_basebackup", args...); err != nil {
		_ = os.RemoveAll(opts.TargetDir)
		return fmt.Errorf("pg_basebackup: %w", err)
	}

	conninfo := BuildPrimaryConninfo(comps)
	autoConfPath := filepath.Join(opts.TargetDir, "postgresql.auto.conf")
	autoConf := fmt.Sprintf("primary_conninfo = %s\n", quoteLiteral(conninfo))
	if err := os.WriteFile(autoConfPath, []byte(autoConf), 0o600); err != nil {
		_ = os.RemoveAll(opts.TargetDir)
		return fmt.Errorf("write postgresql.auto.conf: %w", err)
	}

	standbyPath := filepath.Join(opts.TargetDir, "standby.signal")
	if err := os.WriteFile(standbyPath, nil, 0o600); err != nil {
		_ = os.RemoveAll(opts.TargetDir)
		return fmt.Errorf("write standby.signal: %w", err)
	}

	reportProgressEvent(opts, ProgressEvent{
		Phase: "running_pg_basebackup",
		Step: fmt.Sprintf(
			"clone complete: data directory at %s. Start with: pg_ctl -D %s start. "+
				"Verify with: pg_isready -h localhost -p %s. "+
				"Note: pg_basebackup copies the entire cluster (all databases).",
			opts.TargetDir, opts.TargetDir, comps.Port,
		),
		Current: 1,
		Total:   1,
		Elapsed: time.Since(startedAt),
	})
	return nil
}
