// Package clonework adapts TUI clone requests to the controlled clone runner
// while keeping command execution outside the TUI package.
package clonework

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// ProgressEvent mirrors clone.ProgressEvent for use by consumers that cannot
// import the clone package directly (e.g., the TUI isolation boundary).
type ProgressEvent = clone.ProgressEvent

// Params configures a controlled clone run from the TUI.
type Params struct {
	SourceDSN         string
	CloneName         string
	TargetDSN         string
	Strategy          string
	Schemas           []string
	IncludePrivileges bool
	Replace           bool
	ReplaceSet        bool
	OnConflict        string
	TargetDir         string
	DumpDir           string
	SkipCreate        bool
	SkipCreateSet     bool
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

	sourceDSN := p.SourceDSN
	sourceDSN, err = cfg.ApplySessionGUCs(sourceDSN, connections.SetDSNParam)
	if err != nil {
		return fmt.Errorf("configure source connection: %w", err)
	}
	if targetURL != "" {
		targetURL, err = cfg.ApplySessionGUCs(targetURL, connections.SetDSNParam)
		if err != nil {
			return fmt.Errorf("configure target connection: %w", err)
		}
	}

	permCache, err := clone.NewPermissionCacheConfig(
		cfg.Clone.Preflight.CachePermissions,
		cfg.Clone.Preflight.CachePermissionsPath,
		cfg.Clone.Preflight.CachePermissionsTTL,
	)
	if err != nil {
		return fmt.Errorf("permission cache config: %w", err)
	}

	maxConns := cfg.DB.MaxOpenConns
	if maxConns <= 0 {
		maxConns = 5
	}

	strategy := strings.TrimSpace(p.Strategy)
	if strategy == "" {
		strategy = cfg.Clone.Strategy
	}
	if cfg.Sanitization.Enabled && (strategy == "template" || strategy == "physical-backup") {
		return fmt.Errorf("sanitization cannot rewrite %s clones; use schema-replay or logical-stream, or disable sanitization", strategy)
	}

	var dumpOpts []dump.Option
	var restoreOpts []restore.Option
	dumpOpts = append(dumpOpts, dump.WithSchemas(p.Schemas))
	dumpOpts = append(dumpOpts, dump.SanitizationOptions(cfg.Sanitization.Enabled)...)
	restoreOpts = append(restoreOpts, restore.WithSchemas(p.Schemas))

	replace := cfg.Clone.Replace
	if p.ReplaceSet {
		replace = p.Replace
	}
	onConflict := cfg.Clone.RestoreOnConflict
	if onConflict == "" {
		onConflict = "error"
	}
	if p.OnConflict != "" {
		onConflict = p.OnConflict
	}
	targetDir := cfg.Clone.TargetDir
	if trimmed := strings.TrimSpace(p.TargetDir); trimmed != "" {
		targetDir = trimmed
	}
	dumpDir := cfg.Clone.DumpDir
	if trimmed := strings.TrimSpace(p.DumpDir); trimmed != "" {
		dumpDir = trimmed
	}
	skipCreate := cfg.Clone.SkipCreate
	if p.SkipCreateSet {
		skipCreate = p.SkipCreate
	}
	policy, err := restore.ParseConflictPolicy(onConflict)
	if err != nil {
		return fmt.Errorf("invalid restore_on_conflict %q: %w", onConflict, err)
	}
	if replace && policy != restore.ConflictError {
		return errors.New("restore.replace cannot be combined with restore.on_conflict other than error")
	}
	if strategy != "schema-replay" && (replace || policy != restore.ConflictError) {
		return fmt.Errorf("clone strategy %q does not support replace or on-conflict policies; use schema-replay", strategy)
	}
	if replace {
		restoreOpts = append(restoreOpts, restore.WithReplace())
	} else if policy != restore.ConflictError {
		restoreOpts = append(restoreOpts, restore.WithConflictPolicy(policy))
	}

	opts := clone.Options{
		SourceDSN:         sourceDSN,
		CloneName:         cloneName,
		TargetDSN:         targetURL,
		SkipCreate:        skipCreate,
		DumpDir:           dumpDir,
		TargetDir:         targetDir,
		DumpOpts:          dumpOpts,
		RestoreOpts:       restoreOpts,
		Strategy:          strategy,
		PermissionCache:   permCache,
		MaxOpenConns:      maxConns,
		IncludePrivileges: p.IncludePrivileges,
		SkipAnalyze:       !cfg.Clone.Analyze,
	}

	return runInProcess(ctx, opts, onProgress)
}

var cloneRun = clone.Run

var runInProcess = func(ctx context.Context, opts clone.Options, onProgress func(clone.ProgressEvent)) error {
	opts.ProgressEvent = onProgress
	opts.CommandRunner = clone.SilentCommandRunner{Inner: clone.OSCommandRunner{}}
	return cloneRun(ctx, opts)
}
