// Package clonework adapts TUI clone requests to the controlled clone runner
// while keeping command execution outside the TUI package.
package clonework

import (
	"context"
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// ProgressEvent mirrors clone.ProgressEvent for use by consumers that cannot
// import the clone package directly (e.g., the TUI isolation boundary).
type ProgressEvent = clone.ProgressEvent

// Params configures a controlled clone run from the TUI.
type Params struct {
	SourceDSN string
	CloneName string
	TargetDSN string
	Strategy  string
	Schemas   []string
}

// Run executes clone through the controlled clone runner using selected schemas.
func Run(ctx context.Context, p Params, onProgress func(clone.ProgressEvent)) error {
	if p.SourceDSN == "" {
		return fmt.Errorf("source DSN is required")
	}
	if len(p.Schemas) == 0 {
		return fmt.Errorf("at least one schema is required")
	}

	cfg, err := config.LoadConfig(config.ResolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sourceDB, err := clone.ParseDBName(p.SourceDSN)
	if err != nil {
		return fmt.Errorf("parse source database: %w", err)
	}

	cloneName := strings.TrimSpace(p.CloneName)
	if cloneName == "" {
		cloneName = clone.CloneName(sourceDB, cfg.Clone.NameTemplate)
	}

	// Resolve template {n} placeholder and validate before any side effects.
	resolved, err := clone.ResolveTemplateName(cloneName, 1)
	if err == nil {
		cloneName = resolved
	}
	if err := clone.ValidateCloneName(cloneName); err != nil {
		return fmt.Errorf("validate clone name: %w", err)
	}

	targetURL := strings.TrimSpace(p.TargetDSN)
	if targetURL == "" {
		targetURL = cfg.Clone.TargetURL
	}

	strategy := strings.TrimSpace(p.Strategy)
	if strategy == "" {
		strategy = cfg.Clone.Strategy
	}

	var dumpOpts []dump.Option
	var restoreOpts []restore.Option
	dumpOpts = append(dumpOpts, dump.WithSchemas(p.Schemas))
	dumpOpts = append(dumpOpts, dump.SanitizationOptions(cfg.Sanitization.Enabled)...)
	restoreOpts = append(restoreOpts, restore.WithSchemas(p.Schemas))
	if cfg.Clone.Replace {
		restoreOpts = append(restoreOpts, restore.WithReplace())
	}
	if cfg.Clone.RestoreOnConflict != "" && cfg.Clone.RestoreOnConflict != "error" {
		policy, err := restore.ParseConflictPolicy(cfg.Clone.RestoreOnConflict)
		if err != nil {
			return fmt.Errorf("invalid restore_on_conflict %q: %w", cfg.Clone.RestoreOnConflict, err)
		}
		restoreOpts = append(restoreOpts, restore.WithConflictPolicy(policy))
	}

	opts := clone.Options{
		SourceDSN:   p.SourceDSN,
		CloneName:   cloneName,
		TargetDSN:   targetURL,
		SkipCreate:  cfg.Clone.SkipCreate,
		DumpDir:     cfg.Clone.DumpDir,
		TargetDir:   cfg.Clone.TargetDir,
		DumpOpts:    dumpOpts,
		RestoreOpts: restoreOpts,
		Strategy:    strategy,
	}

	return runInProcess(ctx, opts, onProgress)
}

var cloneRun = clone.Run

var runInProcess = func(ctx context.Context, opts clone.Options, onProgress func(clone.ProgressEvent)) error {
	opts.ProgressEvent = onProgress
	opts.CommandRunner = clone.SilentCommandRunner{Inner: clone.OSCommandRunner{}}
	return cloneRun(ctx, opts)
}
