package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/dumphistory"
	"github.com/VicenteOlmos/dolly/internal/schemacapture"
)

// Injectable seams for testing runDump without live PostgreSQL or dump I/O.
var (
	dumpPingContext   = func(db *sql.DB, ctx context.Context) error { return db.PingContext(ctx) }
	dumpRun           = dump.Dump
	dumpLoadConfig    = config.LoadConfig
	dumpCaptureSchema = captureSchema // test seam: replace in tests
)

type dumpFlags struct {
	DSN               string
	Connection        string
	Output            string
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
	JSON              bool
}

func dumpFlagSet(flags *dumpFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&flags.DSN, "dsn", "", "PostgreSQL connection string")
	fs.StringVar(&flags.Connection, "connection", "", "saved connection profile name (requires save_connections in config.jsonc)")
	fs.StringVar(&flags.Output, "output", "", "output directory")
	fs.BoolVar(&flags.NoTransaction, "no-transaction", false, "skip read-only transaction wrapper (recommended for large subset closures)")
	fs.BoolVar(&flags.SlowConnection, "slow-connection", false, "chunk tables by primary key for slow/unstable connections (forces --no-transaction)")
	fs.IntVar(&flags.ChunkSize, "chunk-size", 0, "rows per chunk in slow-connection mode (default: config slow_chunk_size or 1000)")
	fs.IntVar(&flags.RetryMax, "retry-max", 0, "max query retries per chunk in slow-connection mode (0 = disabled)")
	fs.StringVar(&flags.RetryBase, "retry-base", "", "base backoff between slow-connection retries (default: config or 500ms)")
	fs.StringVar(&flags.SeedFile, "seed-file", "", "JSON seed file for subset dump (omit for full-schema dump)")
	fs.IntVar(&flags.Percent, "percent", 0, "percent-based subset dump (1-100). Selects recent root rows, then FK closure. Conflicts with --seed-file")
	fs.IntVar(&flags.MaxDepth, "max-depth", 0, "subset max FK closure depth (default 10)")
	fs.IntVar(&flags.MaxTables, "max-tables", 0, "subset max tables in closure (default 50)")
	fs.IntVar(&flags.MaxRows, "max-rows", 0, "subset max rows read during planning (default 100000)")
	fs.IntVar(&flags.MaxRowsPerTable, "max-rows-per-table", 0, "subset max rows exported per table (0 = unlimited)")
	fs.IntVar(&flags.MaxInListSize, "max-in-list-size", 0, "subset max values per IN/ANY batch (default 500)")
	fs.Func("include-table", "exact qualified table to include (repeatable; narrows dump; no globs or CSV)", func(s string) error {
		flags.IncludeTables = append(flags.IncludeTables, s)
		return nil
	})
	fs.Func("exclude-table", "exact qualified table to exclude (repeatable; wins over include; no globs or CSV)", func(s string) error {
		flags.ExcludeTables = append(flags.ExcludeTables, s)
		return nil
	})
	fs.Func("include-table-file", "newline-delimited include table file (repeatable; # comments and blank lines ignored)", func(s string) error {
		flags.IncludeTableFiles = append(flags.IncludeTableFiles, s)
		return nil
	})
	fs.Func("exclude-table-file", "newline-delimited exclude table file (repeatable; # comments and blank lines ignored)", func(s string) error {
		flags.ExcludeTableFiles = append(flags.ExcludeTableFiles, s)
		return nil
	})
	fs.Func("chunk-table", "exact qualified table to stream with keyset chunking (repeatable; no globs or CSV)", func(s string) error {
		flags.ChunkTables = append(flags.ChunkTables, s)
		return nil
	})
	fs.Func("chunk-table-file", "newline-delimited chunk table file (repeatable; # comments and blank lines ignored)", func(s string) error {
		flags.ChunkTableFiles = append(flags.ChunkTableFiles, s)
		return nil
	})
	fs.IntVar(&flags.Workers, "workers", 0, "parallel table dump workers (default: config dump.workers or 1; max 16)")
	fs.BoolVar(&flags.JSON, "json", false, "emit machine-readable JSON result to stdout (success only; errors still exit non-zero)")
	return fs
}

func parseDumpFlags(args []string) (dumpFlags, error) {
	if wantsHelp(args) {
		printDumpUsage()
		return dumpFlags{}, errHelp
	}

	var flags dumpFlags
	fs := dumpFlagSet(&flags)
	fs.Usage = printDumpUsage

	if err := fs.Parse(args); err != nil {
		return flags, mapFlagHelp(err)
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "workers" {
			flags.WorkersSet = true
		}
	})

	if err := validateDSNOrConnection(flags.Connection, flags.DSN); err != nil {
		return flags, err
	}

	return flags, nil
}

func applySubsetLimits(limits dump.SubsetLimits, flags dumpFlags, cfg *config.Config) dump.SubsetLimits {
	if flags.MaxDepth > 0 {
		limits.MaxDepth = flags.MaxDepth
	}
	if flags.MaxTables > 0 {
		limits.MaxTables = flags.MaxTables
	}
	if flags.MaxRows > 0 {
		limits.MaxRows = flags.MaxRows
	}
	if flags.MaxRowsPerTable > 0 {
		limits.MaxRowsPerTable = flags.MaxRowsPerTable
	} else if cfg.Subset.MaxRowsPerTable > 0 {
		limits.MaxRowsPerTable = cfg.Subset.MaxRowsPerTable
	}
	if flags.MaxInListSize > 0 {
		limits.MaxInListSize = flags.MaxInListSize
	}
	return limits
}

func resolveDumpWorkers(flags dumpFlags, cfg *config.Config) int {
	if flags.WorkersSet {
		return flags.Workers
	}
	workers := cfg.Dump.Workers
	if workers <= 0 {
		workers = 1
	}
	return workers
}

func validateDumpWorkers(flags dumpFlags, cfg *config.Config, workers int) error {
	if workers < 1 || workers > dump.MaxParallelWorkers() {
		return fmt.Errorf("--workers must be between 1 and %d, got %d", dump.MaxParallelWorkers(), workers)
	}
	if workers <= 1 {
		return nil
	}
	if flags.NoTransaction {
		return errors.New("--workers > 1 requires a read-only transaction snapshot; remove --no-transaction")
	}
	if flags.SlowConnection {
		return errors.New("--workers and --slow-connection are incompatible")
	}
	if hasChunkFlags(flags) || chunkPolicyConfigured(cfg) {
		return errors.New("--workers and chunk-table selectors are incompatible")
	}
	effectiveSeedFile := flags.SeedFile
	if effectiveSeedFile == "" && cfg.Subset.SeedFile != "" {
		effectiveSeedFile = cfg.Subset.SeedFile
	}
	effectivePercent := flags.Percent
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

func buildDumpOptions(flags dumpFlags, cfg *config.Config) ([]dump.Option, error) {
	var opts []dump.Option
	if flags.NoTransaction {
		opts = append(opts, dump.WithoutTransaction())
	}

	// Resolve effective subset settings: CLI overrides config.
	effectiveSeedFile := flags.SeedFile
	if effectiveSeedFile == "" && cfg.Subset.SeedFile != "" {
		effectiveSeedFile = cfg.Subset.SeedFile
	}
	effectivePercent := flags.Percent
	if effectivePercent == 0 {
		effectivePercent = cfg.Subset.Percent
	}

	if effectivePercent < 0 || effectivePercent > 100 {
		return nil, fmt.Errorf("--percent must be between 1 and 100, got %d", effectivePercent)
	}
	if effectiveSeedFile != "" && effectivePercent > 0 {
		return nil, errors.New("--percent and --seed-file are mutually exclusive")
	}
	if flags.SlowConnection && effectiveSeedFile != "" {
		return nil, errors.New("--slow-connection and --seed-file (subset dump) are incompatible")
	}
	if flags.SlowConnection && effectivePercent > 0 {
		return nil, errors.New("--slow-connection and --percent (subset dump) are incompatible")
	}
	if (hasChunkFlags(flags) || chunkPolicyConfigured(cfg)) && effectiveSeedFile != "" {
		return nil, errors.New("--chunk-table and --seed-file (subset dump) are incompatible")
	}
	if (hasChunkFlags(flags) || chunkPolicyConfigured(cfg)) && effectivePercent > 0 {
		return nil, errors.New("--chunk-table and --percent (subset dump) are incompatible")
	}
	workers := resolveDumpWorkers(flags, cfg)
	if err := validateDumpWorkers(flags, cfg, workers); err != nil {
		return nil, err
	}
	if flags.SlowConnection || hasChunkFlags(flags) {
		if flags.SlowConnection {
			opts = append(opts, dump.WithSlowConnection())
		}

		chunkSize := flags.ChunkSize
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

		retryMax := flags.RetryMax
		if retryMax <= 0 && cfg.Dump.SlowRetryMax > 0 {
			retryMax = cfg.Dump.SlowRetryMax
		}
		if retryMax > 0 {
			retryBaseStr := flags.RetryBase
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
	} else if chunkPolicyConfigured(cfg) {
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
		subCfg.Limits = applySubsetLimits(subCfg.Limits, flags, cfg)
		opts = append(opts, dump.WithSubset(subCfg))
	}

	if effectivePercent > 0 {
		subCfg := dump.SubsetConfig{
			Percent: effectivePercent,
			Limits:  dump.DefaultSubsetLimits(),
		}
		subCfg.Limits = applySubsetLimits(subCfg.Limits, flags, cfg)
		opts = append(opts, dump.WithSubset(subCfg))
	}

	if policy, ignored, err := resolveTableSelection(flags, cfg); err != nil {
		return nil, err
	} else if policy != nil {
		opts = append(opts, dump.WithTableSelection(*policy, ignored))
	}

	if chunkPolicy, chunkIgnored, err := resolveChunkPolicy(flags, cfg); err != nil {
		return nil, err
	} else if chunkPolicy != nil {
		opts = append(opts, dump.WithChunkTablePolicy(*chunkPolicy, chunkIgnored))
	}

	opts = append(opts, dump.WithWorkers(workers))

	return opts, nil
}

func hasChunkFlags(flags dumpFlags) bool {
	return len(flags.ChunkTables) > 0 || len(flags.ChunkTableFiles) > 0
}

func chunkPolicyConfigured(cfg *config.Config) bool {
	return len(cfg.Dump.ChunkTables) > 0 || len(cfg.Dump.ChunkTableFiles) > 0
}

func effectiveResilientDumpMode(flags dumpFlags, cfg *config.Config) bool {
	return flags.SlowConnection || hasChunkFlags(flags) || chunkPolicyConfigured(cfg)
}

type resumableDumpExpectation struct {
	sourceSignature      string
	schemas              []string
	sanitizationEnabled  bool
	chunkFingerprint     *dump.ChunkTableProvenance
	selectionFingerprint *dump.TableSelectionProvenance
}

func buildResumableDumpExpectation(flags dumpFlags, cfg *config.Config, dsn string, schemas []string, sanitizationEnabled bool) (resumableDumpExpectation, error) {
	exp := resumableDumpExpectation{
		sourceSignature:     sourceSignatureFromDSN(dsn),
		schemas:             append([]string(nil), schemas...),
		sanitizationEnabled: sanitizationEnabled,
	}
	chunkPolicy, _, err := resolveChunkPolicy(flags, cfg)
	if err != nil {
		return resumableDumpExpectation{}, err
	}
	exp.chunkFingerprint = dump.ChunkPolicyResumeFingerprint(chunkPolicy)

	selPolicy, _, err := resolveTableSelection(flags, cfg)
	if err != nil {
		return resumableDumpExpectation{}, err
	}
	exp.selectionFingerprint = dump.SelectionPolicyResumeFingerprint(selPolicy)
	return exp, nil
}

func provenanceMatchesResumable(meta *dump.Provenance, exp resumableDumpExpectation) bool {
	if meta == nil {
		return false
	}
	if !dump.ChunkResumeProvenanceMatches(exp.chunkFingerprint, meta.ChunkTables) {
		return false
	}
	return dump.SelectionResumeProvenanceMatches(exp.selectionFingerprint, meta.TableSelection)
}

func resolveChunkPolicy(flags dumpFlags, cfg *config.Config) (*dump.ChunkPolicy, []dump.IgnoredFileLine, error) {
	direct := cfg.Dump.ChunkTables
	files := cfg.Dump.ChunkTableFiles
	sourceKind, sourceName := "config", "dump.chunk_tables"

	if hasChunkFlags(flags) {
		direct = flags.ChunkTables
		files = flags.ChunkTableFiles
		sourceKind, sourceName = "flag", "--chunk-table"
	}

	return dump.BuildChunkPolicyWithSources(direct, files, sourceKind, sourceName)
}

func resolveTableSelection(flags dumpFlags, cfg *config.Config) (*dump.SelectionPolicy, []dump.IgnoredFileLine, error) {
	includeDirect := cfg.Dump.IncludeTables
	includeFiles := cfg.Dump.IncludeTableFiles
	excludeDirect := cfg.Dump.ExcludeTables
	excludeFiles := cfg.Dump.ExcludeTableFiles
	includeKind, includeName := "config", "dump.include_tables"
	excludeKind, excludeName := "config", "dump.exclude_tables"

	if len(flags.IncludeTables) > 0 || len(flags.IncludeTableFiles) > 0 {
		includeDirect = flags.IncludeTables
		includeFiles = flags.IncludeTableFiles
		includeKind, includeName = "flag", "--include-table"
	}
	if len(flags.ExcludeTables) > 0 || len(flags.ExcludeTableFiles) > 0 {
		excludeDirect = flags.ExcludeTables
		excludeFiles = flags.ExcludeTableFiles
		excludeKind, excludeName = "flag", "--exclude-table"
	}

	return dump.BuildSelectionPolicyWithSources(
		includeDirect, includeFiles, excludeDirect, excludeFiles,
		includeKind, includeName, excludeKind, excludeName,
	)
}

func runDump(args []string) (err error) {
	flags, ferr := parseDumpFlags(args)

	defer func() {
		if err != nil && flags.JSON {
			emitJSONError(os.Stderr, "dump", err.Error())
			err = errJSONHandled
		}
	}()

	if ferr != nil {
		if errors.Is(ferr, errHelp) {
			return nil
		}
		err = ferr
		return
	}

	cfg, err := dumpLoadConfig(config.ResolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	out := flags.Output
	if out == "" {
		out = cfg.Dump.OutputDir
	}
	if out == "" {
		return errors.New("required flag --output or config dump.output_dir")
	}

	dsn, schemas, err := resolveDataSource(cfg, ".", flags.Connection, flags.DSN)
	if err != nil {
		return err
	}

	if cfg.DB.StatementTimeout != "" && cfg.DB.StatementTimeout != "0" {
		dsn = appendQueryParam(dsn, "statement_timeout", cfg.DB.StatementTimeout)
	}
	// Validate all dump options before opening the database or allocating output.
	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		return err
	}

	maxConns := cfg.DB.MaxOpenConns
	if maxConns <= 0 {
		maxConns = 5
	}
	if err := dump.ValidateWorkerPoolHeadroom(dump.InspectWorkers(opts...), maxConns); err != nil {
		return err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(maxConns)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := dumpPingContext(db, ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	store, err := dumphistory.OpenStore(cfg, ".")
	if err != nil {
		return fmt.Errorf("open dump history: %w", err)
	}

	var outputDir string
	var seq int
	freshAllocated := false
	resumeExpect, err := buildResumableDumpExpectation(flags, cfg, dsn, schemas, cfg.Sanitization.Enabled)
	if err != nil {
		return err
	}
	if effectiveResilientDumpMode(flags, cfg) {
		if resumable, resumableSeq, ok := findResumableDumpDir(out, resumeExpect); ok {
			outputDir = resumable
			seq = resumableSeq
		} else {
			outputDir, seq, err = dumphistory.AllocateDir(out, store)
			if err != nil {
				return fmt.Errorf("allocate dump directory: %w", err)
			}
			freshAllocated = true
		}
	} else {
		outputDir, seq, err = dumphistory.AllocateDir(out, store)
		if err != nil {
			return fmt.Errorf("allocate dump directory: %w", err)
		}
		freshAllocated = true
	}

	if len(schemas) > 0 {
		opts = append(opts, dump.WithSchemas(schemas))
	}
	opts = append(opts, dump.SanitizationOptions(cfg.Sanitization.Enabled)...)
	sanitizationEnabled := cfg.Sanitization.Enabled
	opts = append(opts, dump.WithProvenance(dump.Provenance{
		Seq:             seq,
		BaseDir:         out,
		SourceDatabase:  databaseFromDSN(dsn),
		SourceSignature: sourceSignatureFromDSN(dsn),
		Schemas:         append([]string(nil), schemas...),
		Sanitized:       &sanitizationEnabled,
	}))

	opts = append(opts, dump.WithProgress(func(ev dump.ProgressEvent) {
		if flags.JSON {
			return
		}
		_ = Render(os.Stderr, ev, isStderrTerminal(os.Stderr.Fd()))
	}))

	if err := dumpRun(ctx, db, outputDir, opts...); err != nil {
		if freshAllocated && (dump.IsTableSelectionError(err) || dump.IsChunkPolicyError(err)) {
			_ = removeFreshEmptyDumpDir(outputDir)
		}
		return fmt.Errorf("dump: %w", err)
	}
	fmt.Fprintln(os.Stderr, "dump complete")

	if err := dumpCaptureSchema(ctx, dsn, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: schema capture: %v\n", err)
	}

	sourceDB := databaseFromDSN(dsn)

	if store != nil || flags.JSON {
		meta, err := dump.ReadMetadata(outputDir)
		if err != nil {
			return fmt.Errorf("read dump metadata: %w", err)
		}
		if flags.JSON {
			sch := schemas
			if sch == nil {
				sch = []string{}
			}
			result := map[string]any{
				"ok":              true,
				"command":         "dump",
				"output_dir":      outputDir,
				"seq":             seq,
				"source_database": sourceDB,
				"schemas":         sch,
				"table_count":     len(meta.Tables),
			}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal json result: %w", err)
			}
			fmt.Println(string(data))
		}
		if store != nil {
			rec := dumphistory.RecordFromMetadata(out, seq, outputDir, sourceDB, schemas, meta)
			if err := store.Register(rec); err != nil {
				fmt.Fprintf(os.Stderr, "warning: register dump history: %v\n", err)
			}
		}
	}

	return nil
}

// removeFreshEmptyDumpDir deletes dir only when it contains no entries.
// Uses os.Remove, never RemoveAll. Skips non-empty dirs (partial/resumable dumps).
func removeFreshEmptyDumpDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	return os.Remove(dir)
}

func databaseFromDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// sourceSignatureFromDSN returns a stable resume identity without credentials.
func sourceSignatureFromDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	user := ""
	if u.User != nil {
		user = u.User.Username()
	}
	return fmt.Sprintf("postgres://%s@%s/%s", user, net.JoinHostPort(strings.ToLower(u.Hostname()), port), databaseFromDSN(dsn))
}

// findResumableDumpDir returns the latest numbered dump directory under baseDir
// that looks like an interrupted resilient dump: it contains checkpoint/temp
// artifacts, no final metadata.json, and metadata.json.tmp provenance matches
// the current source, schemas, sanitization, chunk, and selection policy.
func findResumableDumpDir(baseDir string, exp resumableDumpExpectation) (string, int, bool) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", 0, false
	}
	var seqs []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n, err := strconv.Atoi(e.Name())
		if err != nil || n <= 0 {
			continue
		}
		seqs = append(seqs, n)
	}
	if len(seqs) == 0 {
		return "", 0, false
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] > seqs[j] })
	for _, n := range seqs {
		dir := filepath.Join(baseDir, strconv.Itoa(n))
		if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err == nil {
			continue
		}
		if !hasSlowArtifacts(dir) {
			continue
		}
		meta, err := readMetadataTmp(dir)
		if err != nil {
			continue
		}
		if meta.Provenance == nil {
			continue
		}
		if meta.Provenance.SourceSignature == "" || meta.Provenance.SourceSignature != exp.sourceSignature {
			continue
		}
		if !schemasEqual(meta.Provenance.Schemas, exp.schemas) {
			continue
		}
		if meta.Provenance.Sanitized == nil || *meta.Provenance.Sanitized != exp.sanitizationEnabled {
			continue
		}
		if !provenanceMatchesResumable(meta.Provenance, exp) {
			continue
		}
		return dir, n, true
	}
	return "", 0, false
}

func hasSlowArtifacts(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".ckpt.json") || strings.HasSuffix(name, ".ckpt.json.tmp") || strings.HasSuffix(name, ".ndjson.tmp") {
			return true
		}
	}
	return false
}

func readMetadataTmp(dir string) (dump.Metadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json.tmp"))
	if err != nil {
		return dump.Metadata{}, err
	}
	var m dump.Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return dump.Metadata{}, err
	}
	return m, nil
}

func schemasEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aCopy := append([]string(nil), a...)
	bCopy := append([]string(nil), b...)
	sort.Strings(aCopy)
	sort.Strings(bCopy)
	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
}

// captureSchema runs pg_dump --schema-only on dsn and writes the output
// to outDir/schema.sql. It strips the password and any pg_dump-rejected
// URI params before calling pg_dump.
func captureSchema(ctx context.Context, dsn, outDir string) error {
	return schemacapture.Capture(ctx, dsn, outDir)
}
