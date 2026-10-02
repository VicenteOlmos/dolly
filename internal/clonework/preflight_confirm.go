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

var preflightForConfirm = clone.Preflight

// PreflightForConfirm runs clone preflight with the same options as Run would use,
// returning non-fatal schema-replay gap warnings without starting the clone.
func PreflightForConfirm(ctx context.Context, p Params) ([]string, error) {
	opts, strategy, err := buildCloneOptions(p)
	if err != nil {
		return nil, err
	}
	strat, err := clone.Resolve(strategy, opts)
	if err != nil {
		return nil, err
	}
	return preflightForConfirm(ctx, opts, strat)
}

func buildCloneOptions(p Params) (clone.Options, string, error) {
	if p.SourceDSN == "" {
		return clone.Options{}, "", fmt.Errorf("source DSN is required")
	}
	if len(p.Schemas) == 0 {
		return clone.Options{}, "", fmt.Errorf("at least one schema is required")
	}

	cfg, err := config.LoadConfig(config.ResolveConfigPath())
	if err != nil {
		return clone.Options{}, "", fmt.Errorf("load config: %w", err)
	}

	sourceDB, err := clone.ParseDBName(p.SourceDSN)
	if err != nil {
		return clone.Options{}, "", fmt.Errorf("parse source database: %w", err)
	}

	cloneName := strings.TrimSpace(p.CloneName)
	if cloneName == "" {
		cloneName = clone.CloneName(sourceDB, cfg.Clone.NameTemplate)
	}
	resolved, err := clone.ResolveTemplateName(cloneName, 1)
	if err == nil {
		cloneName = resolved
	}
	if err := clone.ValidateCloneName(cloneName); err != nil {
		return clone.Options{}, "", fmt.Errorf("validate clone name: %w", err)
	}

	targetURL := strings.TrimSpace(p.TargetDSN)
	if targetURL == "" {
		targetURL = cfg.Clone.TargetURL
	}

	sourceDSN := p.SourceDSN
	sourceDSN, err = cfg.ApplySessionGUCs(sourceDSN, connections.SetDSNParam)
	if err != nil {
		return clone.Options{}, "", fmt.Errorf("configure source connection: %w", err)
	}
	if targetURL != "" {
		targetURL, err = cfg.ApplySessionGUCs(targetURL, connections.SetDSNParam)
		if err != nil {
			return clone.Options{}, "", fmt.Errorf("configure target connection: %w", err)
		}
	}

	permCache, err := clone.NewPermissionCacheConfig(
		cfg.Clone.Preflight.CachePermissions,
		cfg.Clone.Preflight.CachePermissionsPath,
		cfg.Clone.Preflight.CachePermissionsTTL,
	)
	if err != nil {
		return clone.Options{}, "", fmt.Errorf("permission cache config: %w", err)
	}

	maxConns := cfg.DB.MaxOpenConns
	if maxConns <= 0 {
		maxConns = 5
	}

	strategy := strings.TrimSpace(p.Strategy)
	if strategy == "" {
		strategy = cfg.Clone.Strategy
	}
	if strategy == "" {
		strategy = "schema-replay"
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
		return clone.Options{}, "", fmt.Errorf("invalid restore_on_conflict %q: %w", onConflict, err)
	}
	if replace && policy != restore.ConflictError {
		return clone.Options{}, "", errors.New("restore.replace cannot be combined with restore.on_conflict other than error")
	}
	if strategy != "schema-replay" && (replace || policy != restore.ConflictError) {
		return clone.Options{}, "", fmt.Errorf("clone strategy %q does not support replace or on-conflict policies; use schema-replay", strategy)
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
	}
	return opts, strategy, nil
}
