package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

type cloneFlags struct {
	FastForward       bool
	Strategy          string
	TargetDir         string
	DumpDir           string
	DumpDirSet        bool
	Connection        string
	Schemas           []string
	Replace           bool
	ReplaceSet        bool
	OnConflict        string
	OnConflictSet     bool
	SkipCreate        bool
	SkipCreateSet     bool
	Yes               bool
	JSON              bool
	IncludePrivileges bool
	NoVerify          bool
	NoVerifySet       bool
	NoAnalyze         bool
	NoAnalyzeSet      bool
}

func cloneFlagSet(flags *cloneFlags, schemasRaw *string) *flag.FlagSet {
	fs := flag.NewFlagSet("clone", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&flags.FastForward, "ff", false, "fast-forward: skip prompts and use config defaults")
	fs.StringVar(&flags.Strategy, "strategy", "", "clone strategy: template, schema-replay, logical-stream (large single-DB), physical-backup")
	fs.StringVar(&flags.TargetDir, "target-dir", "", "target data directory for physical-backup clone (pg_basebackup -D)")
	fs.StringVar(&flags.DumpDir, "dump-dir", "", "intermediate dump directory for schema-replay clone (overrides clone.dump_dir config)")
	fs.StringVar(&flags.Connection, "connection", "", "saved connection profile name (requires save_connections in config.jsonc)")
	fs.BoolVar(&flags.Replace, "replace", false, "truncate target tables before restore (overrides clone.replace config)")
	fs.StringVar(&flags.OnConflict, "on-conflict", "", "restore row conflict policy: error, skip, upsert (overrides clone.restore_on_conflict config)")
	fs.BoolVar(&flags.SkipCreate, "skip-create", false, "skip creating the target database (overrides clone.skip_create config)")
	fs.BoolVar(&flags.Yes, "yes", false, "confirm destructive operations (required with -ff when clone.replace=true)")
	fs.BoolVar(&flags.IncludePrivileges, "with-privileges", false, "schema-replay and logical-stream: keep owners and ACLs (roles must already exist on the target)")
	fs.BoolVar(&flags.NoVerify, "no-verify", false, "skip post-clone row and sequence verification (schema-replay; overrides clone.verify config)")
	fs.BoolVar(&flags.NoAnalyze, "no-analyze", false, "schema-replay: skip post-restore ANALYZE on the target (overrides clone.analyze config)")
	fs.BoolVar(&flags.JSON, "json", false, "emit machine-readable JSON result to stdout (success only; errors still exit non-zero)")
	if schemasRaw != nil {
		fs.StringVar(schemasRaw, "schemas", "", "comma-separated source schema names")
	}
	return fs
}

func parseCloneFlags(args []string) (cloneFlags, error) {
	if wantsHelp(args) {
		printCloneUsage()
		return cloneFlags{}, errHelp
	}

	var flags cloneFlags
	var schemasRaw string
	fs := cloneFlagSet(&flags, &schemasRaw)
	fs.Usage = printCloneUsage

	if err := fs.Parse(args); err != nil {
		return flags, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "replace":
			flags.ReplaceSet = true
		case "on-conflict":
			flags.OnConflictSet = true
		case "skip-create":
			flags.SkipCreateSet = true
		case "dump-dir":
			flags.DumpDirSet = true
		case "no-verify":
			flags.NoVerifySet = true
		case "no-analyze":
			flags.NoAnalyzeSet = true
		}
	})
	flags.Schemas = parseCommaSeparatedSchemas(schemasRaw)

	if flags.OnConflictSet {
		if _, err := restore.ParseConflictPolicy(flags.OnConflict); err != nil {
			return flags, fmt.Errorf("invalid --on-conflict %q: %w", flags.OnConflict, err)
		}
	}

	if flags.Connection != "" && !flags.FastForward {
		return flags, errors.New("--connection requires -ff (non-interactive fast-forward mode)")
	}
	if flags.JSON && !flags.FastForward {
		return flags, errors.New("clone --json requires -ff (non-interactive mode)")
	}

	return flags, nil
}

// Injectable seams for testing runClone without live PostgreSQL.
var (
	cloneLoadConfig    = config.LoadConfig
	clonePromptSource  = config.PromptSource
	cloneIsTerminal    = config.IsStdinTerminal
	cloneRun           = clone.Run
	clonePingContext   = func(db *sql.DB, ctx context.Context) error { return db.PingContext(ctx) }
	cloneLoadDotEnv    = config.LoadDotEnv
	cloneSignalContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}
)

// cloneListSchemaNames lists non-system schemas on the source database.
var cloneListSchemaNames = func(ctx context.Context, dsn string) ([]string, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(5)
	conn.SetConnMaxIdleTime(5 * time.Minute)
	conn.SetConnMaxLifetime(30 * time.Minute)
	if err := clonePingContext(conn, ctx); err != nil {
		return nil, fmt.Errorf("ping source: %w", err)
	}
	return db.ListPostgresSchemaNames(ctx, conn)
}

func init() {
	config.PromptListSchemaNames = func(ctx context.Context, sourceDSN string) ([]string, error) {
		return cloneListSchemaNames(ctx, sourceDSN)
	}
	config.RedactDSNFunc = connections.RedactDSN
}

func resolveCloneSchemas(ctx context.Context, sourceDSN string, flagSchemas []string, cfg *config.Config, fromPrompt []string) ([]string, error) {
	if len(flagSchemas) > 0 {
		return append([]string(nil), flagSchemas...), nil
	}
	if len(fromPrompt) > 0 {
		return append([]string(nil), fromPrompt...), nil
	}
	if cfg != nil && len(cfg.Clone.Schemas) > 0 {
		return append([]string(nil), cfg.Clone.Schemas...), nil
	}
	// Default to public — matches resolveEffectiveDumpSchemas contract.
	return []string{"public"}, nil
}

func logCloneSchemas(schemas []string) {
	if len(schemas) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "info: clone source schemas: %s\n", strings.Join(schemas, ", "))
}

func parseCommaSeparatedSchemas(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runClone(args []string) (err error) {
	flags, ferr := parseCloneFlags(args)

	defer func() {
		if err != nil && flags.JSON {
			emitJSONError(os.Stderr, "clone", err.Error())
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

	if !flags.FastForward && !cloneIsTerminal() {
		return errors.New("not a TTY; use -ff for fast-forward mode")
	}

	cfg, err := cloneLoadConfig(config.ResolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if flags.Connection != "" {
		conn, err := connections.Resolve(cfg, ".", flags.Connection)
		if err != nil {
			return err
		}
		ctx, cancel := cloneSignalContext()
		defer cancel()
		return runCloneWithSource(ctx, flags, cfg, conn)
	}

	envNames := config.EnvVarNames{
		URLVar:      cfg.Env.URLVar,
		HostVar:     cfg.Env.HostVar,
		PortVar:     cfg.Env.PortVar,
		NameVar:     cfg.Env.NameVar,
		UserVar:     cfg.Env.UserVar,
		PasswordVar: cfg.Env.PasswordVar,
	}
	sourceDSN, err := cloneLoadDotEnv(cfg.Env.Path, envNames)
	if flags.FastForward {
		if err != nil {
			return fmt.Errorf("resolve source DSN: %w", err)
		}
	} else if err != nil && !errors.Is(err, config.ErrSourceDSNNotFound) {
		return fmt.Errorf("resolve source DSN: %w", err)
	} else if err != nil {
		sourceDSN = ""
	}

	var cloneName, targetURL, strategy string
	var fromPromptSchemas []string
	if flags.FastForward {
		sourceDB, err := clone.ParseDBName(sourceDSN)
		if err != nil {
			return fmt.Errorf("parse source DB name: %w", err)
		}
		cloneName, err = clone.ResolveTemplateName(clone.CloneName(sourceDB, cfg.Clone.NameTemplate), 1)
		if err != nil {
			return fmt.Errorf("resolve clone name template: %w", err)
		}
		targetURL = cfg.Clone.TargetURL
		strategy = cfg.Clone.Strategy
		if flags.Strategy != "" {
			strategy = flags.Strategy
		}
	} else {
		defaultCloneName := defaultCloneNameForPrompt(sourceDSN, cfg.Clone.NameTemplate)
		defaults := config.PromptDefaults{
			SourceDSN: sourceDSN,
			CloneName: defaultCloneName,
			TargetURL: cfg.Clone.TargetURL,
			Strategy:  cfg.Clone.Strategy,
			Schemas:   append([]string(nil), cfg.Clone.Schemas...),
		}
		savedPicker, err := cloneSavedSourcePicker(cfg)
		if err != nil {
			return err
		}
		result, err := clonePromptSource(os.Stdin, os.Stdout, defaults, savedPicker)
		if err != nil {
			return fmt.Errorf("prompt: %w", err)
		}
		sourceDSN = result.SourceDSN
		cloneName = result.CloneName
		targetURL = result.TargetURL
		fromPromptSchemas = result.SourceSchemas
		strategy = cfg.Clone.Strategy
		if flags.Strategy != "" {
			strategy = flags.Strategy
		}
		if result.Strategy != "" {
			strategy = result.Strategy
		}

		if sourceDSN == "" {
			return errors.New("source DSN is required")
		}
		if cloneName == defaultCloneName {
			sourceDB, err := clone.ParseDBName(sourceDSN)
			if err != nil {
				return fmt.Errorf("parse source DB name: %w", err)
			}
			cloneName, err = clone.ResolveTemplateName(clone.CloneName(sourceDB, cfg.Clone.NameTemplate), 1)
			if err != nil {
				return fmt.Errorf("resolve clone name template: %w", err)
			}
		} else {
			// User-provided clone name from prompt — resolve {n} if present.
			resolved, err := clone.ResolveTemplateName(cloneName, 1)
			if err == nil {
				cloneName = resolved
			}
		}
	}

	ctx, cancel := cloneSignalContext()
	defer cancel()

	if err := clone.ValidateCloneName(cloneName); err != nil {
		return fmt.Errorf("validate clone name: %w", err)
	}

	schemas, err := resolveCloneSchemas(ctx, sourceDSN, flags.Schemas, cfg, fromPromptSchemas)
	if err != nil {
		return fmt.Errorf("resolve schemas: %w", err)
	}
	if !flags.JSON {
		logCloneSchemas(schemas)
	}

	return runCloneExecute(ctx, flags, cfg, sourceDSN, cloneName, targetURL, schemas, strategy)
}

func cloneSavedSourcePicker(cfg *config.Config) (*config.SavedSourcePicker, error) {
	if cfg == nil || !cfg.SaveConnections {
		return nil, nil
	}
	store, err := connections.OpenStore(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("open connection store: %w", err)
	}
	if store == nil {
		return nil, nil
	}
	list, err := store.List()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &config.SavedSourcePicker{
		Pick: func(scanner *bufio.Scanner, w io.Writer) (string, []string, error) {
			conn, err := connections.PickPromptScanner(scanner, w, store)
			if err != nil {
				return "", nil, err
			}
			return conn.DSN(), append([]string(nil), conn.Schemas...), nil
		},
	}, nil
}

func runCloneWithSource(ctx context.Context, flags cloneFlags, cfg *config.Config, conn connections.Connection) error {
	sourceDSN := conn.DSN()
	sourceDB, err := clone.ParseDBName(sourceDSN)
	if err != nil {
		return fmt.Errorf("parse source DB name: %w", err)
	}
	cloneName, err := clone.ResolveTemplateName(clone.CloneName(sourceDB, cfg.Clone.NameTemplate), 1)
	if err != nil {
		return fmt.Errorf("resolve clone name template: %w", err)
	}

	if err := clone.ValidateCloneName(cloneName); err != nil {
		return fmt.Errorf("validate clone name: %w", err)
	}

	targetURL := cfg.Clone.TargetURL
	strategy := cfg.Clone.Strategy
	if flags.Strategy != "" {
		strategy = flags.Strategy
	}

	schemas := append([]string(nil), flags.Schemas...) // CLI --schemas first
	if len(schemas) == 0 {
		schemas = append([]string(nil), conn.Schemas...) // then saved profile
	}
	if len(schemas) == 0 {
		var err error
		schemas, err = resolveCloneSchemas(ctx, sourceDSN, flags.Schemas, cfg, nil)
		if err != nil {
			return fmt.Errorf("resolve schemas: %w", err)
		}
	}
	if !flags.JSON {
		logCloneSchemas(schemas)
	}

	return runCloneExecute(ctx, flags, cfg, sourceDSN, cloneName, targetURL, schemas, strategy)
}

func runCloneExecute(ctx context.Context, flags cloneFlags, cfg *config.Config, sourceDSN, cloneName, targetURL string, schemas []string, strategy string) error {
	var err error
	sourceDSN, err = cfg.ApplySessionGUCs(sourceDSN, appendQueryParam)
	if err != nil {
		return err
	}
	if targetURL != "" {
		targetURL, err = cfg.ApplySessionGUCs(targetURL, appendQueryParam)
		if err != nil {
			return err
		}
	}
	var dumpOpts []dump.Option
	var restoreOpts []restore.Option
	if len(schemas) > 0 {
		dumpOpts = append(dumpOpts, dump.WithSchemas(schemas))
		restoreOpts = append(restoreOpts, restore.WithSchemas(schemas))
	}
	dumpOpts = append(dumpOpts, dump.SanitizationOptions(cfg.Sanitization.Enabled)...)
	replace := cfg.Clone.Replace
	if flags.ReplaceSet {
		replace = flags.Replace
	}
	onConflict := cfg.Clone.RestoreOnConflict
	if onConflict == "" {
		onConflict = "error"
	}
	if flags.OnConflictSet {
		onConflict = flags.OnConflict
	}
	policy, err := restore.ParseConflictPolicy(onConflict)
	if err != nil {
		return fmt.Errorf("invalid restore_on_conflict %q: %w", onConflict, err)
	}
	if replace {
		if !flags.Yes {
			return errors.New("clone with replace truncates the target; pass --yes to confirm")
		}
		fmt.Fprintf(os.Stderr, "info: target database: %s\n", databaseFromDSN(targetURL))
		restoreOpts = append(restoreOpts, restore.WithReplace())
	}
	if policy != restore.ConflictError {
		restoreOpts = append(restoreOpts, restore.WithConflictPolicy(policy))
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

	targetDir := cfg.Clone.TargetDir
	if flags.TargetDir != "" {
		targetDir = flags.TargetDir
	}

	if cfg.Sanitization.Enabled && (strategy == "template" || strategy == "physical-backup") {
		return fmt.Errorf("sanitization cannot rewrite %s clones; use schema-replay or logical-stream, or disable sanitization", strategy)
	}
	if !cfg.Sanitization.Enabled {
		fmt.Fprintf(os.Stderr, "warning: clone will copy unsanitized data (strategy=%s, sanitization=%v)\n", strategy, cfg.Sanitization.Enabled)
	}
	skipCreate := cfg.Clone.SkipCreate
	if flags.SkipCreateSet {
		skipCreate = flags.SkipCreate
	}
	dumpDir := cfg.Clone.DumpDir
	if flags.DumpDirSet {
		if trimmed := strings.TrimSpace(flags.DumpDir); trimmed != "" {
			dumpDir = trimmed
		}
	}
	if skipCreate {
		fmt.Fprintf(os.Stderr, "warning: skip_create may leave partial state on the existing target database if the clone fails\n")
	}

	verify := cfg.Clone.Verify
	if flags.NoVerifySet {
		verify = !flags.NoVerify
	}
	skipAnalyze := !cfg.Clone.Analyze
	if flags.NoAnalyzeSet {
		skipAnalyze = flags.NoAnalyze
	}

	var preflightWarnings []string
	var verifyWarnings []string
	verifyTables := -1
	opts := clone.Options{
		SourceDSN:         sourceDSN,
		CloneName:         cloneName,
		TargetDSN:         targetURL,
		TargetDir:         targetDir,
		SkipCreate:        skipCreate,
		DumpDir:           dumpDir,
		DumpOpts:          dumpOpts,
		RestoreOpts:       restoreOpts,
		Strategy:          strategy,
		PermissionCache:   permCache,
		MaxOpenConns:      maxConns,
		IncludePrivileges: flags.IncludePrivileges,
		SkipAnalyze:       skipAnalyze,
		RowTransform:      dump.InspectRowTransform(dump.SanitizationOptions(cfg.Sanitization.Enabled && strategy == "logical-stream")...),
		PreflightWarnings: &preflightWarnings,
		Verify:            verify,
		VerifyWarnings:    &verifyWarnings,
		VerifyTables:      &verifyTables,
		ProgressEvent: func(ev clone.ProgressEvent) {
			if flags.JSON {
				return
			}
			_ = Render(os.Stderr, ev, isStderrTerminal(os.Stderr.Fd()))
		},
	}

	if err := cloneRun(ctx, opts); err != nil {
		return fmt.Errorf("clone: %w", err)
	}
	for _, w := range preflightWarnings {
		fmt.Fprintln(os.Stderr, w)
	}
	for _, w := range verifyWarnings {
		fmt.Fprintln(os.Stderr, w)
	}
	if verifyTables >= 0 && len(verifyWarnings) == 0 {
		fmt.Fprintln(os.Stderr, clone.FormatVerifiedTables(verifyTables))
	}
	fmt.Fprintln(os.Stderr, "clone complete")

	if flags.JSON {
		sch := schemas
		if sch == nil {
			sch = []string{}
		}
		pw := preflightWarnings
		if pw == nil {
			pw = []string{}
		}
		vw := verifyWarnings
		if vw == nil {
			vw = []string{}
		}
		result := map[string]any{
			"ok":                 true,
			"command":            "clone",
			"source_database":    databaseFromDSN(sourceDSN),
			"clone_name":         cloneName,
			"strategy":           strategy,
			"target_dir":         targetDir,
			"schemas":            sch,
			"preflight_warnings": pw,
			"verify_warnings":    vw,
		}
		if verifyTables >= 0 {
			result["verify_tables"] = verifyTables
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json result: %w", err)
		}
		fmt.Println(string(data))
	}

	return nil
}

func defaultCloneNameForPrompt(sourceDSN, template string) string {
	if sourceDSN == "" {
		return clone.CloneName("db", template)
	}
	sourceDB, err := clone.ParseDBName(sourceDSN)
	if err != nil {
		return clone.CloneName("db", template)
	}
	return clone.CloneName(sourceDB, template)
}
