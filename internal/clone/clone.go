package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/VicenteOlmos/dolly/internal/db" // registers pgx driver
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
	"github.com/jackc/pgx/v5/pgconn"
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

const (
	cleanupDropMaxAttempts = 3
	cleanupDropBase        = 100 * time.Millisecond
	cleanupDropBackoffCap  = time.Second
)

const terminateBackendsSQL = `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`

func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func terminateDatabaseBackends(ctx context.Context, dbConn *sql.DB, dbName string) {
	rows, err := dbConn.QueryContext(ctx, terminateBackendsSQL, dbName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: terminate backends on %q: %v\n", dbName, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var terminated sql.NullBool
		if err := rows.Scan(&terminated); err != nil {
			fmt.Fprintf(os.Stderr, "warning: terminate backends on %q: %v\n", dbName, err)
			return
		}
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: terminate backends on %q: %v\n", dbName, err)
	}
}

func cleanupDropBackoff(ctx context.Context, attempt int) error {
	delay := cleanupDropBase << attempt
	if delay > cleanupDropBackoffCap {
		delay = cleanupDropBackoffCap
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// dropDatabase terminates sessions scoped to dbName, then drops the database
// with bounded retries for races that reconnect before DROP completes.
func dropDatabase(ctx context.Context, adminDSN, dbName string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}
	dbConn, err := sqlOpenDB(adminDSN)
	if err != nil {
		return fmt.Errorf("drop database: open admin connection: %w", err)
	}
	defer dbConn.Close()

	var lastErr error
	for attempt := 0; attempt < cleanupDropMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("drop database: %w", err)
		}
		terminateDatabaseBackends(ctx, dbConn, dbName)
		_, lastErr = dbConn.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdentifier(dbName)))
		if lastErr == nil {
			return nil
		}
		if pgErrorCode(lastErr) != "55006" || attempt == cleanupDropMaxAttempts-1 {
			break
		}
		if err := cleanupDropBackoff(ctx, attempt); err != nil {
			return fmt.Errorf("drop database: %w", err)
		}
	}
	return fmt.Errorf("drop database: %w", lastErr)
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
