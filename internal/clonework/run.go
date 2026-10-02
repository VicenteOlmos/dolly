// Package clonework adapts TUI clone requests to the controlled clone runner
// while keeping command execution outside the TUI package.
package clonework

import (
	"context"
	"fmt"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/config"
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
	// VerifyWarnings receives schema-replay verify warnings when non-nil.
	VerifyWarnings *[]string
	// VerifyTables receives the compared table count when schema-replay verification runs.
	// Callers that need to detect a skipped check should initialize it to -1.
	VerifyTables *int
}

// Run executes clone through the controlled clone runner using selected schemas.
func Run(ctx context.Context, p Params, onProgress func(clone.ProgressEvent)) error {
	opts, strategy, err := buildCloneOptions(p)
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(config.ResolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Sanitization.Enabled && (strategy == "template" || strategy == "physical-backup") {
		return fmt.Errorf("sanitization cannot rewrite %s clones; use schema-replay or logical-stream, or disable sanitization", strategy)
	}
	opts.VerifyWarnings = p.VerifyWarnings
	opts.VerifyTables = p.VerifyTables
	return runInProcess(ctx, opts, onProgress)
}

// VerifiedTablesLine is the clean schema-replay verification summary.
func VerifiedTablesLine(n int) string {
	return clone.FormatVerifiedTables(n)
}

var cloneRun = clone.Run

var runInProcess = func(ctx context.Context, opts clone.Options, onProgress func(clone.ProgressEvent)) error {
	opts.ProgressEvent = onProgress
	opts.CommandRunner = clone.SilentCommandRunner{Inner: clone.OSCommandRunner{}}
	return cloneRun(ctx, opts)
}
