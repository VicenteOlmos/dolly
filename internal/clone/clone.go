package clone

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/VicenteOlmos/dolly/internal/db" // registers pgx driver
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// ProgressEvent reports step-granularity progress during a clone operation.
type ProgressEvent struct {
	Phase   string        // e.g. "creating_target", "replaying_schema", "dumping", "restoring", "copying_table", "restoring_sequences", "running_pg_basebackup", "creating_from_template"
	Step    string        // human-readable label for the current step
	Table   string        // table name for table-grained phases
	Current int           // 1-based step index
	Total   int           // total number of steps
	Elapsed time.Duration // since Run() entered
}

// Options configures a clone run.
type Options struct {
	SourceDSN     string
	CloneName     string
	TargetDSN     string
	TargetDir     string // filesystem path for physical-backup pg_basebackup
	SkipCreate    bool
	DumpDir       string
	RestoreOpts   []restore.Option
	DumpOpts      []dump.Option
	Strategy      string
	CommandRunner CommandRunner
	// ProgressEvent receives typed numeric progress events. Preferred over ProgressFn.
	ProgressEvent func(ProgressEvent)
	// Deprecated: ProgressFn receives human-readable clone progress messages.
	// Use ProgressEvent instead. Kept for one minor version for backward compatibility.
	ProgressFn      func(string)
	PermissionCache PermissionCacheConfig
	// MaxOpenConns limits sql.DB pool size for this run. Zero or negative uses 5.
	MaxOpenConns int
}

// CloneName replaces "{db}" in template with sourceDB.
func CloneName(sourceDB, template string) string {
	if template == "" {
		template = "{db}_dolly_{n}"
	}
	return strings.ReplaceAll(template, "{db}", sourceDB)
}

// MaxOpenConns is legacy internal bridge state read by sqlOpenDB.
// Callers configure each run through Options.MaxOpenConns; Run snapshots and
// restores this package variable under maxOpenConnsMu. Default is 5.
var MaxOpenConns = 5

var maxOpenConnsMu sync.Mutex

func effectiveRunMaxOpenConns(opts Options) int {
	if opts.MaxOpenConns > 0 {
		return opts.MaxOpenConns
	}
	return 5
}

// sqlOpenDB is overridable for testing CreateDatabase with go-sqlmock.
var sqlOpenDB = func(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(MaxOpenConns)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

// dumpFunc is overridable for testing Run without a live database.
var dumpFunc = dump.Dump

// restoreFunc is overridable for testing Run without a live database.
var restoreFunc = restore.Restore

// preflightFunc is overridable for testing Run ordering without full preflight matrix.
var preflightFunc = Preflight

// quoteIdentifier returns a PostgreSQL double-quoted identifier,
// escaping embedded double quotes by doubling them.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteLiteral returns a PostgreSQL single-quoted string literal,
// escaping embedded single quotes by doubling them.
func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// CreateDatabase connects to the postgres maintenance DB and runs CREATE DATABASE.
func CreateDatabase(ctx context.Context, adminDSN, dbName string) error {
	dbConn, err := sqlOpenDB(adminDSN)
	if err != nil {
		return fmt.Errorf("open admin connection: %w", err)
	}
	defer dbConn.Close()

	_, err = dbConn.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %s`, quoteIdentifier(dbName)))
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	return nil
}

// dropDatabaseFunc is overridable for testing cleanup paths.
var dropDatabaseFunc = dropDatabase

// dropDatabase connects to the admin maintenance DB and drops the named
// database. Errors are best-effort — this is a cleanup helper where the
// primary error (what caused the cleanup) matters more.
func dropDatabase(ctx context.Context, adminDSN, dbName string) error {
	dbConn, err := sqlOpenDB(adminDSN)
	if err != nil {
		return fmt.Errorf("drop database: open admin connection: %w", err)
	}
	defer dbConn.Close()

	_, err = dbConn.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdentifier(dbName)))
	if err != nil {
		return fmt.Errorf("drop database: %w", err)
	}
	return nil
}

// Run orchestrates dump → restore to clone a database.
// When opts.Strategy is non-empty, it dispatches to the resolved strategy.
// Otherwise it falls back to the legacy dump→restore path for backward compatibility.
func Run(ctx context.Context, opts Options) error {
	if opts.SourceDSN == "" {
		return fmt.Errorf("source DSN is required")
	}
	if opts.CloneName == "" {
		return fmt.Errorf("clone name is required")
	}
	if err := ValidateCloneName(opts.CloneName); err != nil {
		return fmt.Errorf("validate clone name: %w", err)
	}

	maxOpenConnsMu.Lock()
	prevMaxOpenConns := MaxOpenConns
	MaxOpenConns = effectiveRunMaxOpenConns(opts)
	defer func() {
		MaxOpenConns = prevMaxOpenConns
		maxOpenConnsMu.Unlock()
	}()

	// Strategy dispatch path. Empty strategy defaults to schema-replay.
	strategy := opts.Strategy
	if strategy == "" {
		strategy = "schema-replay"
	}
	strat, err := Resolve(strategy, opts)
	if err != nil {
		return err
	}
	if err := preflightFunc(ctx, opts, strat); err != nil {
		return err
	}
	return strat.Execute(ctx, opts)
}
