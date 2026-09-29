package runopts

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

// DumpOverrides carries runtime dump settings. CLI flags map here; the TUI supplies
// NoTransaction from DumpDraft and leaves other fields at zero values so config applies.
type DumpOverrides struct {
	NoTransaction     bool
	SlowConnection    bool
	ChunkSize         int
	RetryMax          int
	RetryBase         string
	SeedFile          string
	Percent           int
	MaxDepth          int
	MaxTables         int
	MaxRows           int
	MaxRowsPerTable   int
	MaxInListSize     int
	IncludeTables     []string
	ExcludeTables     []string
	IncludeTableFiles []string
	ExcludeTableFiles []string
	ChunkTables       []string
	ChunkTableFiles   []string
	Workers           int
	WorkersSet        bool
	RequireSafeKey    bool
}

func (o DumpOverrides) HasChunkSelectors() bool {
	return len(o.ChunkTables) > 0 || len(o.ChunkTableFiles) > 0
}

func ChunkPolicyConfigured(cfg *config.Config) bool {
	return len(cfg.Dump.ChunkTables) > 0 || len(cfg.Dump.ChunkTableFiles) > 0
}

func EffectiveResilientDumpMode(o DumpOverrides, cfg *config.Config) bool {
	return o.SlowConnection || o.HasChunkSelectors() || ChunkPolicyConfigured(cfg)
}

func applySubsetLimits(limits dump.SubsetLimits, o DumpOverrides, cfg *config.Config) dump.SubsetLimits {
	if o.MaxDepth > 0 {
		limits.MaxDepth = o.MaxDepth
	}
	if o.MaxTables > 0 {
		limits.MaxTables = o.MaxTables
	}
	if o.MaxRows > 0 {
		limits.MaxRows = o.MaxRows
	}
	if o.MaxRowsPerTable > 0 {
		limits.MaxRowsPerTable = o.MaxRowsPerTable
	} else if cfg.Subset.MaxRowsPerTable > 0 {
		limits.MaxRowsPerTable = cfg.Subset.MaxRowsPerTable
	}
	if o.MaxInListSize > 0 {
		limits.MaxInListSize = o.MaxInListSize
	}
	return limits
}

func resolveDumpWorkers(o DumpOverrides, cfg *config.Config) int {
	if o.WorkersSet {
		return o.Workers
	}
	workers := cfg.Dump.Workers
	if workers <= 0 {
		workers = 1
	}
	return workers
}

func validateDumpWorkers(o DumpOverrides, cfg *config.Config, workers int) error {
	if workers < 1 || workers > dump.MaxParallelWorkers() {
		return fmt.Errorf("--workers must be between 1 and %d, got %d", dump.MaxParallelWorkers(), workers)
	}
	if workers <= 1 {
		return nil
	}
	if o.NoTransaction {
		return errors.New("--workers > 1 requires a read-only transaction snapshot; remove --no-transaction")
	}
	if o.SlowConnection {
		return errors.New("--workers and --slow-connection are incompatible")
	}
	if o.HasChunkSelectors() || ChunkPolicyConfigured(cfg) {
		return errors.New("--workers and chunk-table selectors are incompatible")
	}
	effectiveSeedFile := o.SeedFile
	if effectiveSeedFile == "" && cfg.Subset.SeedFile != "" {
		effectiveSeedFile = cfg.Subset.SeedFile
	}
	effectivePercent := o.Percent
	if effectivePercent == 0 {
		effectivePercent = cfg.Subset.Percent
	}
	if effectiveSeedFile != "" {
		return errors.New("--workers and --seed-file (subset dump) are incompatible")
	}
	if effectivePercent > 0 {
		return errors.New("--workers and --percent (subset dump) are incompatible")
	}
	return nil
}

// BuildDumpOptions resolves config.jsonc dump/subset keys and optional CLI-style overrides.
func BuildDumpOptions(o DumpOverrides, cfg *config.Config) ([]dump.Option, error) {
	var opts []dump.Option
	if o.NoTransaction {
		opts = append(opts, dump.WithoutTransaction())
	}

	effectiveSeedFile := o.SeedFile
	if effectiveSeedFile == "" && cfg.Subset.SeedFile != "" {
		effectiveSeedFile = cfg.Subset.SeedFile
	}
	effectivePercent := o.Percent
	if effectivePercent == 0 {
		effectivePercent = cfg.Subset.Percent
	}

	if effectivePercent < 0 || effectivePercent > 100 {
		return nil, fmt.Errorf("--percent must be between 1 and 100, got %d", effectivePercent)
	}
	if effectiveSeedFile != "" && effectivePercent > 0 {
		return nil, errors.New("--percent and --seed-file are mutually exclusive")
	}
	if o.SlowConnection && effectiveSeedFile != "" {
		return nil, errors.New("--slow-connection and --seed-file (subset dump) are incompatible")
	}
	if o.SlowConnection && effectivePercent > 0 {
		return nil, errors.New("--slow-connection and --percent (subset dump) are incompatible")
	}
	if (o.HasChunkSelectors() || ChunkPolicyConfigured(cfg)) && effectiveSeedFile != "" {
		return nil, errors.New("--chunk-table and --seed-file (subset dump) are incompatible")
	}
	if (o.HasChunkSelectors() || ChunkPolicyConfigured(cfg)) && effectivePercent > 0 {
		return nil, errors.New("--chunk-table and --percent (subset dump) are incompatible")
	}
	workers := resolveDumpWorkers(o, cfg)
	if err := validateDumpWorkers(o, cfg, workers); err != nil {
		return nil, err
	}
	if o.RequireSafeKey {
		opts = append(opts, dump.WithRequireSafeKey())
	}
	if o.SlowConnection || o.HasChunkSelectors() {
		if o.SlowConnection {
			opts = append(opts, dump.WithSlowConnection())
		}

		chunkSize := o.ChunkSize
		if chunkSize <= 0 && cfg.Dump.SlowChunkSize > 0 {
			chunkSize = cfg.Dump.SlowChunkSize
		}
		if chunkSize <= 0 {
			chunkSize = dump.DefaultSlowChunkSize
		}
		const maxChunkSize = 1000000
		if chunkSize > maxChunkSize {
			fmt.Fprintf(os.Stderr, "chunk-size %d exceeds max %d, capping to %d\n", chunkSize, maxChunkSize, maxChunkSize)
			chunkSize = maxChunkSize
		}
		opts = append(opts, dump.WithSlowChunkSize(chunkSize))

		retryMax := o.RetryMax
		if retryMax <= 0 && cfg.Dump.SlowRetryMax > 0 {
			retryMax = cfg.Dump.SlowRetryMax
		}
		if retryMax > 0 {
			retryBaseStr := o.RetryBase
			if retryBaseStr == "" {
				retryBaseStr = cfg.Dump.SlowRetryBase
			}
			if retryBaseStr == "" {
				retryBaseStr = "500ms"
			}
			retryBase, err := time.ParseDuration(retryBaseStr)
			if err != nil {
				return nil, fmt.Errorf("parse retry base duration %q: %w", retryBaseStr, err)
			}
			if retryBase <= 0 {
				return nil, errors.New("slow-connection retry base must be positive when retry-max > 0")
			}
			opts = append(opts, dump.WithSlowRetry(retryMax, retryBase))
		}
	} else if ChunkPolicyConfigured(cfg) {
		chunkSize := cfg.Dump.SlowChunkSize
		if chunkSize <= 0 {
			chunkSize = dump.DefaultSlowChunkSize
		}
		opts = append(opts, dump.WithSlowChunkSize(chunkSize))
		if cfg.Dump.SlowRetryMax > 0 {
			retryBaseStr := cfg.Dump.SlowRetryBase
			if retryBaseStr == "" {
				retryBaseStr = "500ms"
			}
			retryBase, err := time.ParseDuration(retryBaseStr)
			if err != nil {
				return nil, fmt.Errorf("parse retry base duration %q: %w", retryBaseStr, err)
			}
			if retryBase <= 0 {
				return nil, errors.New("chunk retry base must be positive when slow_retry_max > 0")
			}
			opts = append(opts, dump.WithSlowRetry(cfg.Dump.SlowRetryMax, retryBase))
		}
	}

	if effectiveSeedFile != "" {
		subCfg, err := dump.ParseSeedFile(effectiveSeedFile)
		if err != nil {
			return nil, err
		}
		subCfg.Limits = dump.ApplySubsetLimitDefaults(subCfg.Limits)
		subCfg.Limits = applySubsetLimits(subCfg.Limits, o, cfg)
		opts = append(opts, dump.WithSubset(subCfg))
	}

	if effectivePercent > 0 {
		subCfg := dump.SubsetConfig{
			Percent: effectivePercent,
			Limits:  dump.DefaultSubsetLimits(),
		}
		subCfg.Limits = applySubsetLimits(subCfg.Limits, o, cfg)
		opts = append(opts, dump.WithSubset(subCfg))
	}

	if policy, ignored, err := ResolveTableSelection(o, cfg); err != nil {
		return nil, err
	} else if policy != nil {
		opts = append(opts, dump.WithTableSelection(*policy, ignored))
	}

	if chunkPolicy, chunkIgnored, err := ResolveChunkPolicy(o, cfg); err != nil {
		return nil, err
	} else if chunkPolicy != nil {
		opts = append(opts, dump.WithChunkTablePolicy(*chunkPolicy, chunkIgnored))
	}

	opts = append(opts, dump.WithWorkers(workers))

	return opts, nil
}

// ResolveChunkPolicy merges CLI overrides with config dump chunk selectors.
func ResolveChunkPolicy(o DumpOverrides, cfg *config.Config) (*dump.ChunkPolicy, []dump.IgnoredFileLine, error) {
	direct := cfg.Dump.ChunkTables
	files := cfg.Dump.ChunkTableFiles
	sourceKind, sourceName := "config", "dump.chunk_tables"

	if o.HasChunkSelectors() {
		direct = o.ChunkTables
		files = o.ChunkTableFiles
		sourceKind, sourceName = "flag", "--chunk-table"
	}

	return dump.BuildChunkPolicyWithSources(direct, files, sourceKind, sourceName)
}

// ResolveTableSelection merges CLI overrides with config include/exclude lists.
func ResolveTableSelection(o DumpOverrides, cfg *config.Config) (*dump.SelectionPolicy, []dump.IgnoredFileLine, error) {
	includeDirect := cfg.Dump.IncludeTables
	includeFiles := cfg.Dump.IncludeTableFiles
	excludeDirect := cfg.Dump.ExcludeTables
	excludeFiles := cfg.Dump.ExcludeTableFiles
	includeKind, includeName := "config", "dump.include_tables"
	excludeKind, excludeName := "config", "dump.exclude_tables"

	if len(o.IncludeTables) > 0 || len(o.IncludeTableFiles) > 0 {
		includeDirect = o.IncludeTables
		includeFiles = o.IncludeTableFiles
		includeKind, includeName = "flag", "--include-table"
	}
	if len(o.ExcludeTables) > 0 || len(o.ExcludeTableFiles) > 0 {
		excludeDirect = o.ExcludeTables
		excludeFiles = o.ExcludeTableFiles
		excludeKind, excludeName = "flag", "--exclude-table"
	}

	return dump.BuildSelectionPolicyWithSources(
		includeDirect, includeFiles, excludeDirect, excludeFiles,
		includeKind, includeName, excludeKind, excludeName,
	)
}
