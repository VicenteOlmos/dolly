package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	DSN             string
	Connection      string
	Output          string
	NoTransaction   bool
	SlowConnection  bool
	ChunkSize       int
	RetryMax        int
	RetryBase       string
	SeedFile        string
	Percent         int
	MaxDepth        int
	MaxTables       int
	MaxRows         int
	MaxRowsPerTable int
	MaxInListSize   int
	JSON            bool
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

	if err := validateDSNOrConnection(flags.Connection, flags.DSN); err != nil {
		return flags, err
	}

	return flags, nil
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
	if effectivePercent == 0 && cfg.Subset.Percent > 0 {
		effectivePercent = cfg.Subset.Percent
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
	if flags.SlowConnection {
		opts = append(opts, dump.WithSlowConnection())

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
	}

	if effectiveSeedFile != "" {
		subCfg, err := dump.ParseSeedFile(effectiveSeedFile)
		if err != nil {
			return nil, err
		}
		subCfg.Limits = dump.ApplySubsetLimitDefaults(subCfg.Limits)
		if flags.MaxDepth > 0 {
			subCfg.Limits.MaxDepth = flags.MaxDepth
		}
		if flags.MaxTables > 0 {
			subCfg.Limits.MaxTables = flags.MaxTables
		}
		if flags.MaxRows > 0 {
			subCfg.Limits.MaxRows = flags.MaxRows
		}
		if flags.MaxRowsPerTable > 0 {
			subCfg.Limits.MaxRowsPerTable = flags.MaxRowsPerTable
		} else if cfg.Subset.MaxRowsPerTable > 0 {
			subCfg.Limits.MaxRowsPerTable = cfg.Subset.MaxRowsPerTable
		}
		if flags.MaxInListSize > 0 {
			subCfg.Limits.MaxInListSize = flags.MaxInListSize
		}
		opts = append(opts, dump.WithSubset(subCfg))
	}

	if effectivePercent > 0 {
		if effectivePercent < 1 || effectivePercent > 100 {
			return nil, fmt.Errorf("--percent must be between 1 and 100, got %d", effectivePercent)
		}
		subCfg := dump.SubsetConfig{
			Percent: effectivePercent,
			Limits:  dump.DefaultSubsetLimits(),
		}
		if flags.MaxDepth > 0 {
			subCfg.Limits.MaxDepth = flags.MaxDepth
		}
		if flags.MaxTables > 0 {
			subCfg.Limits.MaxTables = flags.MaxTables
		}
		if flags.MaxRows > 0 {
			subCfg.Limits.MaxRows = flags.MaxRows
		}
		if flags.MaxRowsPerTable > 0 {
			subCfg.Limits.MaxRowsPerTable = flags.MaxRowsPerTable
		} else if cfg.Subset.MaxRowsPerTable > 0 {
			subCfg.Limits.MaxRowsPerTable = cfg.Subset.MaxRowsPerTable
		}
		if flags.MaxInListSize > 0 {
			subCfg.Limits.MaxInListSize = flags.MaxInListSize
		}
		opts = append(opts, dump.WithSubset(subCfg))
	}

	return opts, nil
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

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	maxConns := cfg.DB.MaxOpenConns
	if maxConns <= 0 {
		maxConns = 5
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
	if flags.SlowConnection {
		if resumable, resumableSeq, ok := findResumableSlowDumpDir(out, databaseFromDSN(dsn), schemas); ok {
			outputDir = resumable
			seq = resumableSeq
		} else {
			outputDir, seq, err = dumphistory.AllocateDir(out, store)
			if err != nil {
				return fmt.Errorf("allocate dump directory: %w", err)
			}
		}
	} else {
		outputDir, seq, err = dumphistory.AllocateDir(out, store)
		if err != nil {
			return fmt.Errorf("allocate dump directory: %w", err)
		}
	}

	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		return err
	}
	if len(schemas) > 0 {
		opts = append(opts, dump.WithSchemas(schemas))
	}
	opts = append(opts, dump.SanitizationOptions(cfg.Sanitization.Enabled)...)
	opts = append(opts, dump.WithProvenance(dump.Provenance{
		Seq:            seq,
		BaseDir:        out,
		SourceDatabase: databaseFromDSN(dsn),
		Schemas:        append([]string(nil), schemas...),
	}))

	opts = append(opts, dump.WithProgress(func(ev dump.ProgressEvent) {
		if flags.JSON {
			return
		}
		_ = Render(os.Stderr, ev, isStderrTerminal(os.Stderr.Fd()))
	}))

	if err := dumpRun(ctx, db, outputDir, opts...); err != nil {
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

func databaseFromDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// findResumableSlowDumpDir returns the latest numbered dump directory under
// baseDir that looks like an interrupted slow-connection dump: it contains
// slow checkpoint/temp artifacts, no final metadata.json, and its
// metadata.json.tmp provenance matches the current source database and schemas.
// Normal dumps are unaffected because this helper is only consulted for
// --slow-connection.
func findResumableSlowDumpDir(baseDir, sourceDB string, schemas []string) (string, int, bool) {
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
		if meta.Provenance.SourceDatabase != sourceDB {
			continue
		}
		if !schemasEqual(meta.Provenance.Schemas, schemas) {
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
