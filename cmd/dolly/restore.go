package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

var (
	restoreLoadConfig  = config.LoadConfig
	restoreRestore     = restore.Restore
	restorePingContext = func(db *sql.DB, ctx context.Context) error { return db.PingContext(ctx) }
)

type restoreFlags struct {
	DSN                 string
	Connection          string
	Input               string
	OnConflict          string
	Replace             bool
	NoTransaction       bool
	Yes                 bool
	JSON                bool
	TrustSchemaSQL      bool
	Workers             int
	WorkersSet          bool
	AckPartialState     bool
	PartialStateFile    string
	PartialStateFileSet bool
}

func restoreFlagSet(flags *restoreFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&flags.DSN, "dsn", "", "PostgreSQL connection string")
	fs.StringVar(&flags.Connection, "connection", "", "saved connection profile name (requires save_connections in config.jsonc)")
	fs.StringVar(&flags.Input, "input", "", "dump input directory")
	fs.StringVar(&flags.OnConflict, "on-conflict", "error", "row conflict policy: error, skip, upsert")
	fs.BoolVar(&flags.Replace, "replace", false, "truncate tables before insert (destructive)")
	fs.BoolVar(&flags.NoTransaction, "no-transaction", false, "commit after each table")
	fs.BoolVar(&flags.Yes, "yes", false, "confirm destructive or advanced operations (required with --replace, --no-transaction, or --trust-schema-sql)")
	fs.BoolVar(&flags.JSON, "json", false, "emit machine-readable JSON result to stdout (success only; errors still exit non-zero)")
	fs.BoolVar(&flags.TrustSchemaSQL, "trust-schema-sql", false, "replay reviewed schema.sql when target tables are missing (requires --no-transaction --yes)")
	fs.IntVar(&flags.Workers, "workers", 0, "parallel table restore workers (default: config restore.workers or 1; max 16)")
	fs.BoolVar(&flags.AckPartialState, "ack-partial-state", false, "acknowledge partial-state risk for parallel restore (required when workers > 1)")
	fs.StringVar(&flags.PartialStateFile, "partial-state-file", "", "partial-state manifest path (default: config restore.partial_state_file or input/.dolly-restore-partial-state.json)")
	return fs
}

func parseRestoreFlags(args []string) (restoreFlags, error) {
	if wantsHelp(args) {
		printRestoreUsage()
		return restoreFlags{}, errHelp
	}

	var flags restoreFlags
	fs := restoreFlagSet(&flags)
	fs.Usage = printRestoreUsage

	if err := fs.Parse(args); err != nil {
		return flags, mapFlagHelp(err)
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "workers":
			flags.WorkersSet = true
		case "partial-state-file":
			flags.PartialStateFileSet = true
		}
	})
	if err := validateDSNOrConnection(flags.Connection, flags.DSN); err != nil {
		return flags, err
	}
	if flags.Input == "" {
		return flags, errors.New("required flag --input")
	}
	if flags.Replace && flags.OnConflict != "error" {
		return flags, errors.New("--replace cannot be combined with --on-conflict other than error")
	}
	if flags.Replace && !flags.Yes {
		return flags, errors.New("--replace truncates target tables; pass --yes to confirm")
	}
	if flags.Replace && flags.NoTransaction {
		return flags, errors.New("--replace and --no-transaction together can leave tables empty on failure; use --replace alone (atomic truncate+restore in one transaction) or --no-transaction alone (no truncate)")
	}
	if flags.TrustSchemaSQL && !flags.NoTransaction {
		return flags, errors.New("--trust-schema-sql requires --no-transaction because schema replay disables atomic data restore; pass --no-transaction --yes")
	}
	if flags.NoTransaction && !flags.Yes {
		return flags, errors.New("--no-transaction commits per table with no global rollback; pass --yes to confirm (default restore is atomic)")
	}
	return flags, nil
}

func resolveRestoreWorkers(flags restoreFlags, cfg *config.Config) int {
	if flags.WorkersSet {
		return flags.Workers
	}
	workers := cfg.Restore.Workers
	if workers <= 0 {
		workers = 1
	}
	return workers
}

func validateRestoreWorkers(workers int) error {
	if workers < 1 || workers > restore.MaxParallelRestoreWorkers() {
		return fmt.Errorf("--workers must be between 1 and %d, got %d", restore.MaxParallelRestoreWorkers(), workers)
	}
	return nil
}

func resolveRestorePartialStatePath(flags restoreFlags, cfg *config.Config, inputDir string) string {
	if flags.PartialStateFileSet {
		return flags.PartialStateFile
	}
	if cfg.Restore.PartialStateFile != "" {
		return cfg.Restore.PartialStateFile
	}
	return restore.DefaultPartialStatePath(inputDir)
}

func validateParallelRestoreCLI(flags restoreFlags, workers int, policy restore.ConflictPolicy) error {
	if workers <= 1 {
		return nil
	}
	if !flags.NoTransaction {
		return errors.New("parallel restore requires --no-transaction")
	}
	if !flags.Yes {
		return errors.New("parallel restore requires --yes to confirm")
	}
	if !flags.AckPartialState {
		return errors.New("parallel restore requires --ack-partial-state")
	}
	if flags.Replace {
		return errors.New("parallel restore is incompatible with --replace")
	}
	if flags.TrustSchemaSQL {
		return errors.New("parallel restore is incompatible with --trust-schema-sql")
	}
	if policy != restore.ConflictError {
		return fmt.Errorf("parallel restore requires --on-conflict error, got %q", flags.OnConflict)
	}
	return nil
}

func runRestore(args []string) (err error) {
	flags, ferr := parseRestoreFlags(args)

	defer func() {
		if err != nil && flags.JSON {
			emitJSONError(os.Stderr, "restore", err.Error())
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

	policy, err := restore.ParseConflictPolicy(flags.OnConflict)
	if err != nil {
		return err
	}

	cfg, err := restoreLoadConfig(config.ResolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	workers := resolveRestoreWorkers(flags, cfg)
	if err := validateRestoreWorkers(workers); err != nil {
		return err
	}
	if err := validateParallelRestoreCLI(flags, workers, policy); err != nil {
		return err
	}
	partialStatePath := resolveRestorePartialStatePath(flags, cfg, flags.Input)
	if workers > 1 {
		if err := restore.ValidatePartialStatePath(partialStatePath); err != nil {
			return err
		}
	}

	dsn, schemas, err := resolveDataSource(cfg, ".", flags.Connection, flags.DSN)
	if err != nil {
		return err
	}

	if cfg.DB.StatementTimeout != "" && cfg.DB.StatementTimeout != "0" {
		var err error
		dsn, err = appendQueryParam(dsn, "statement_timeout", cfg.DB.StatementTimeout)
		if err != nil {
			return err
		}
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
	if err := restorePingContext(db, ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	if flags.Replace && flags.Yes {
		fmt.Fprintf(os.Stderr, "info: target database: %s\n", databaseFromDSN(dsn))
	}

	var opts []restore.Option
	if flags.Replace {
		opts = append(opts, restore.WithReplace())
	} else {
		opts = append(opts, restore.WithConflictPolicy(policy))
	}
	if flags.NoTransaction {
		opts = append(opts, restore.WithoutTransaction())
	}
	if len(schemas) > 0 {
		opts = append(opts, restore.WithSchemas(schemas))
	}
	opts = append(opts, restore.WithDSN(dsn))
	if workers > 1 || flags.WorkersSet || cfg.Restore.Workers > 1 {
		opts = append(opts, restore.WithWorkers(workers))
	}
	if workers > 1 {
		opts = append(opts, restore.WithPartialStateManifest(partialStatePath))
	}

	if flags.TrustSchemaSQL {
		opts = append(opts, restore.WithTrustedSchemaSQL())
	}

	opts = append(opts, restore.WithProgress(func(ev restore.ProgressEvent) {
		if flags.JSON {
			return
		}
		_ = Render(os.Stderr, ev, isStderrTerminal(os.Stderr.Fd()))
	}))

	if err := restoreRestore(ctx, db, flags.Input, opts...); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	fmt.Fprintln(os.Stderr, "restore complete")

	if flags.JSON {
		meta, err := dump.ReadMetadata(flags.Input)
		if err != nil {
			return fmt.Errorf("read input metadata: %w", err)
		}
		sch := schemas
		if sch == nil {
			sch = []string{}
		}
		result := map[string]any{
			"ok":              true,
			"command":         "restore",
			"input_dir":       flags.Input,
			"target_database": databaseFromDSN(dsn),
			"schemas":         sch,
			"table_count":     len(meta.Tables),
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json result: %w", err)
		}
		fmt.Println(string(data))
	}

	return nil
}
