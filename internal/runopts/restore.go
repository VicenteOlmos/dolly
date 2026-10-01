package runopts

import (
	"errors"
	"fmt"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// RestoreOverrides carries CLI restore flags. The TUI history restore path uses
// RestoreHistoryOptions instead.
type RestoreOverrides struct {
	OnConflict          string
	Replace             bool
	NoTransaction       bool
	TrustSchemaSQL      bool
	Workers             int
	WorkersSet          bool
	PartialStateFile    string
	PartialStateFileSet bool
	AckPartialState     bool
	Yes                 bool
	ExcludeTables       []string
}

// ResolveRestoreWorkers returns effective parallel restore worker count.
func ResolveRestoreWorkers(o RestoreOverrides, cfg *config.Config) int {
	if o.WorkersSet {
		return o.Workers
	}
	workers := cfg.Restore.Workers
	if workers <= 0 {
		workers = 1
	}
	return workers
}

// ValidateRestoreWorkers bounds-checks worker count.
func ValidateRestoreWorkers(workers int) error {
	if workers < 1 || workers > restore.MaxParallelRestoreWorkers() {
		return fmt.Errorf("--workers must be between 1 and %d, got %d", restore.MaxParallelRestoreWorkers(), workers)
	}
	return nil
}

// ResolveRestorePartialStatePath picks the partial-state manifest path.
func ResolveRestorePartialStatePath(o RestoreOverrides, cfg *config.Config, inputDir string) string {
	if o.PartialStateFileSet {
		return o.PartialStateFile
	}
	if cfg.Restore.PartialStateFile != "" {
		return cfg.Restore.PartialStateFile
	}
	return restore.DefaultPartialStatePath(inputDir)
}

// ValidateParallelRestoreCLI enforces parallel restore safety flags on the CLI.
func ValidateParallelRestoreCLI(o RestoreOverrides, workers int, policy restore.ConflictPolicy) error {
	if workers <= 1 {
		return nil
	}
	if !o.NoTransaction {
		return errors.New("parallel restore requires --no-transaction")
	}
	if !o.Yes {
		return errors.New("parallel restore requires --yes to confirm")
	}
	if !o.AckPartialState {
		return errors.New("parallel restore requires --ack-partial-state")
	}
	if o.Replace {
		return errors.New("parallel restore is incompatible with --replace")
	}
	if o.TrustSchemaSQL {
		return errors.New("parallel restore is incompatible with --trust-schema-sql")
	}
	if policy != restore.ConflictError {
		return fmt.Errorf("parallel restore requires --on-conflict error, got %q", o.OnConflict)
	}
	return nil
}

func validateParallelRestoreConfig(cfg *config.Config, workers int, trustedSchemaSQL bool) error {
	replace := cfg.Restore.Replace
	onConflict := cfg.Restore.RestoreOnConflict
	if onConflict == "" {
		onConflict = "error"
	}
	return validateParallelRestoreConfigEffective(cfg, workers, trustedSchemaSQL, replace, onConflict)
}

func validateParallelRestoreConfigEffective(cfg *config.Config, workers int, trustedSchemaSQL bool, replace bool, onConflict string) error {
	if workers <= 1 {
		return nil
	}
	if trustedSchemaSQL {
		return errors.New("parallel restore is incompatible with trusted schema.sql")
	}
	if replace {
		return errors.New("parallel restore is incompatible with restore.replace")
	}
	policy, err := restore.ParseConflictPolicy(onConflict)
	if err != nil {
		return err
	}
	if policy != restore.ConflictError {
		return fmt.Errorf("parallel restore requires restore.on_conflict error, got %q", onConflict)
	}
	return nil
}

// ResolveRestoreExcludeTables merges CLI --exclude-table values with restore.exclude_tables.
// When the CLI passes at least one selector, it replaces the config list entirely.
func ResolveRestoreExcludeTables(o RestoreOverrides, cfg *config.Config) []string {
	if len(o.ExcludeTables) > 0 {
		return append([]string(nil), o.ExcludeTables...)
	}
	if len(cfg.Restore.ExcludeTables) > 0 {
		return append([]string(nil), cfg.Restore.ExcludeTables...)
	}
	return nil
}

// RestoreHistoryUserOverrides carries optional per-run overrides for TUI history restore.
// Zero value uses restore.* config for all fields.
type RestoreHistoryUserOverrides struct {
	OnConflict      string
	Replace         bool
	ReplaceSet      bool
	Workers         int
	WorkersSet      bool
	AckPartialState bool
}

// RestoreHistoryOptions builds restore options for TUI history restore from restore.* config keys.
func RestoreHistoryOptions(cfg *config.Config, inputDir string, schemas []string, trustedSchemaSQL bool, dsn string) ([]restore.Option, error) {
	return RestoreHistoryOptionsWithOverrides(cfg, inputDir, schemas, trustedSchemaSQL, dsn, RestoreHistoryUserOverrides{})
}

// RestoreHistoryOptionsWithOverrides merges optional TUI overrides before building restore options.
func RestoreHistoryOptionsWithOverrides(cfg *config.Config, inputDir string, schemas []string, trustedSchemaSQL bool, dsn string, o RestoreHistoryUserOverrides) ([]restore.Option, error) {
	onConflict := cfg.Restore.RestoreOnConflict
	if onConflict == "" {
		onConflict = "error"
	}
	if o.OnConflict != "" {
		onConflict = o.OnConflict
	}
	policy, err := restore.ParseConflictPolicy(onConflict)
	if err != nil {
		return nil, err
	}
	replace := cfg.Restore.Replace
	if o.ReplaceSet {
		replace = o.Replace
	}
	if replace && policy != restore.ConflictError {
		return nil, errors.New("restore.replace cannot be combined with restore.on_conflict other than error")
	}

	workers := ResolveRestoreWorkers(RestoreOverrides{}, cfg)
	if o.WorkersSet {
		workers = o.Workers
	}
	if err := ValidateRestoreWorkers(workers); err != nil {
		return nil, err
	}
	if err := validateParallelRestoreConfigEffective(cfg, workers, trustedSchemaSQL, replace, onConflict); err != nil {
		return nil, err
	}
	if workers > 1 && !o.AckPartialState {
		return nil, errors.New("parallel restore requires --ack-partial-state")
	}
	partialStatePath := ResolveRestorePartialStatePath(RestoreOverrides{}, cfg, inputDir)
	if workers > 1 {
		if err := restore.ValidatePartialStatePath(partialStatePath); err != nil {
			return nil, err
		}
	}

	var opts []restore.Option
	if replace {
		opts = append(opts, restore.WithReplace())
	} else {
		opts = append(opts, restore.WithConflictPolicy(policy))
	}
	if workers > 1 {
		opts = append(opts, restore.WithoutTransaction())
	}
	if len(schemas) > 0 {
		opts = append(opts, restore.WithSchemas(schemas))
	}
	if dsn != "" {
		opts = append(opts, restore.WithDSN(dsn))
	}
	if workers > 1 || cfg.Restore.Workers > 1 {
		opts = append(opts, restore.WithWorkers(workers))
	}
	if workers > 1 {
		opts = append(opts, restore.WithPartialStateManifest(partialStatePath))
	}
	if trustedSchemaSQL {
		opts = append(opts, restore.WithTrustedSchemaSQL())
	}
	if excludes := ResolveRestoreExcludeTables(RestoreOverrides{}, cfg); len(excludes) > 0 {
		opts = append(opts, restore.WithExcludeTables(excludes))
	}
	return opts, nil
}
func AppendRestoreOptionsFromFlags(o RestoreOverrides, cfg *config.Config, inputDir string, policy restore.ConflictPolicy, workers int, schemas []string, dsn string) ([]restore.Option, error) {
	partialStatePath := ResolveRestorePartialStatePath(o, cfg, inputDir)
	if workers > 1 {
		if err := restore.ValidatePartialStatePath(partialStatePath); err != nil {
			return nil, err
		}
	}

	var opts []restore.Option
	if o.Replace {
		opts = append(opts, restore.WithReplace())
	} else {
		opts = append(opts, restore.WithConflictPolicy(policy))
	}
	if o.NoTransaction {
		opts = append(opts, restore.WithoutTransaction())
	}
	if len(schemas) > 0 {
		opts = append(opts, restore.WithSchemas(schemas))
	}
	opts = append(opts, restore.WithDSN(dsn))
	if workers > 1 || o.WorkersSet || cfg.Restore.Workers > 1 {
		opts = append(opts, restore.WithWorkers(workers))
	}
	if workers > 1 {
		opts = append(opts, restore.WithPartialStateManifest(partialStatePath))
	}
	if o.TrustSchemaSQL {
		opts = append(opts, restore.WithTrustedSchemaSQL())
	}
	if excludes := ResolveRestoreExcludeTables(o, cfg); len(excludes) > 0 {
		opts = append(opts, restore.WithExcludeTables(excludes))
	}
	return opts, nil
}
