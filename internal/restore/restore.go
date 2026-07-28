// Package restore loads dolly dump artifacts (metadata.json and per-table .ndjson)
// into an existing PostgreSQL schema.
//
// Tables are sorted by foreign-key dependency (parents before children) before
// insert, regardless of metadata.json order. Cyclic foreign-key graphs are
// appended deterministically; restore may still fail with constraint errors
// unless the database uses deferrable constraints.
package restore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/schemasql"
)

type execQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type config struct {
	policy             ConflictPolicy
	replace            bool
	withoutTransaction bool
	schemas            []string
	onProgress         func(ProgressEvent)
	dsn                string // pgx connection string for COPY path
	schemaSQL          bool   // auto-apply schema.sql when target tables missing
	workers            int
	partialStatePath   string
}

// Option configures Restore behavior.
type Option func(*config)

// WithConflictPolicy sets row-level conflict handling (ignored when WithReplace is set).
func WithConflictPolicy(p ConflictPolicy) Option {
	return func(c *config) {
		c.policy = p
	}
}

// WithReplace truncates tables in reverse dependency order before insert.
func WithReplace() Option {
	return func(c *config) {
		c.replace = true
	}
}

// WithoutTransaction commits after each table instead of one transaction.
func WithoutTransaction() Option {
	return func(c *config) {
		c.withoutTransaction = true
	}
}

// WithSchemas limits target schema introspection to the given PostgreSQL schemas.
// When empty, schemas are derived from dump metadata.
func WithSchemas(schemas []string) Option {
	return func(c *config) {
		c.schemas = append([]string(nil), schemas...)
	}
}

// WithDSN provides a PostgreSQL connection string for the CopyFrom fast path.
// When set and the conflict policy allows it (error-conflict or replace), COPY is
// used instead of INSERT-per-row for a ~10-100x throughput improvement.
// COPY runs on a separate pgx.Conn and does not participate in the sql.DB
// transaction. Without this option, INSERT-per-row is always used.
func WithDSN(dsn string) Option {
	return func(c *config) {
		c.dsn = dsn
	}
}

// WithTrustedSchemaSQL permits replaying a reviewed schema.sql artifact.
// schemasql.Sanitize is compatibility filtering, not security validation.
func WithTrustedSchemaSQL() Option {
	return func(c *config) {
		c.schemaSQL = true
		c.withoutTransaction = true
	}
}

// restoreApplySchemaSQL is a test seam for running psql to apply schema.sql.
var restoreApplySchemaSQL = applySchemaSQL

var psqlLookPath = exec.LookPath

var runPSQLSchema = func(ctx context.Context, args []string, env []string, stderr *strings.Builder) error {
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = env
	cmd.Stderr = stderr
	return cmd.Run()
}

// ProgressEvent reports table-granularity progress during Restore.
type ProgressEvent struct {
	Phase   string
	Table   string
	Worker  int // parallel worker id; zero in serial restore
	Current int
	Total   int
	Elapsed time.Duration
}

// WithProgress registers a callback invoked at each table boundary.
// A nil callback is a no-op.
func WithProgress(fn func(ProgressEvent)) Option {
	return func(c *config) {
		c.onProgress = fn
	}
}

func emitProgress(cfg *config, ev ProgressEvent) {
	if cfg.onProgress == nil {
		return
	}
	defer func() { _ = recover() }()
	cfg.onProgress(ev)
}

// InspectSchemas returns schema filter names captured from opts, or nil.
func InspectSchemas(opts ...Option) []string {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if len(c.schemas) == 0 {
		return nil
	}
	return append([]string(nil), c.schemas...)
}

// InspectWorkers returns the worker count captured from opts (0 means unset/default 1).
func InspectWorkers(opts ...Option) int {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.workers
}

// InspectPartialStateManifest returns the partial-state manifest path from opts.
func InspectPartialStateManifest(opts ...Option) string {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.partialStatePath
}

// Restore loads dump artifacts from inputDir into an existing PostgreSQL schema.
func Restore(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...Option) error {
	var cfg config
	cfg.policy = ConflictError
	for _, opt := range opts {
		opt(&cfg)
	}

	startedAt := time.Now()

	meta, err := dump.ReadMetadata(inputDir)
	if err != nil {
		return err
	}
	meta.Tables = dump.SortTables(meta.Tables)
	dataPaths, err := verifyNDJSONFiles(meta, inputDir)
	if err != nil {
		return err
	}
	if cfg.replace && cfg.withoutTransaction {
		return fmt.Errorf("replace requires transactions")
	}

	insertPolicy := cfg.policy
	if cfg.replace {
		insertPolicy = ConflictError
	}

	workers := effectiveRestoreWorkers(cfg.workers)
	var parallelLevels []RestoreLevel
	if workers > 1 {
		if err := validateParallelRestoreOptions(&cfg); err != nil {
			return err
		}
		var err error
		parallelLevels, err = BuildRestoreLevels(meta.Tables)
		if err != nil {
			return err
		}
	}

	// Schema validation (outside transaction — schema.sql may need psql).
	schemaFilter := cfg.schemas
	if len(schemaFilter) == 0 {
		schemaFilter = schemasFromMetadata(meta)
	}
	target, err := db.LoadPostgresSchemas(ctx, dbConn, schemaFilter)
	if err != nil {
		return fmt.Errorf("load target schema: %w", err)
	}
	if err := validateSchema(meta.Tables, target); err != nil {
		if cfg.schemaSQL && isTableNotFoundErr(err) {
			if !cfg.withoutTransaction {
				return fmt.Errorf("validate schema: automatic schema application requires WithoutTransaction")
			}
			applyErr := restoreApplySchemaSQL(ctx, cfg.dsn, inputDir)
			if applyErr != nil {
				return fmt.Errorf("validate schema: %w (schema.sql apply failed: %v)", err, applyErr)
			}
			target, err = db.LoadPostgresSchemas(ctx, dbConn, schemaFilter)
			if err != nil {
				return fmt.Errorf("load target schema after schema.sql: %w", err)
			}
			if err := validateSchema(meta.Tables, target); err != nil {
				return fmt.Errorf("validate schema after schema.sql: %w", err)
			}
		} else {
			return fmt.Errorf("validate schema: %w", err)
		}
	}

	var q execQuerier = dbConn
	var tx *sql.Tx

	if !cfg.withoutTransaction {
		tx, err = dbConn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		q = tx
	}

	if cfg.replace {
		if err := truncateTables(ctx, q, meta.Tables); err != nil {
			return err
		}
	}

	if workers > 1 {
		return runParallelRestore(ctx, &cfg, dbConn, meta, dataPaths, parallelLevels, schemaFilter, workers, startedAt)
	}

	if len(meta.Tables) == 0 {
		// Empty dump: emit a single event so consumers can clear spinners.
		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_start",
			Current: 1,
			Total:   1,
			Elapsed: time.Since(startedAt),
		})
		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_end",
			Current: 1,
			Total:   1,
			Elapsed: time.Since(startedAt),
		})
	}

	for i, table := range meta.Tables {
		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_start",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(meta.Tables),
			Elapsed: time.Since(startedAt),
		})

		// ponytail: COPY fast path when withoutTransaction and policy allows it.
		// COPY on a fresh pgx.Conn is atomic per-table at the PG level but does
		// not participate in the sql.DB transaction. For WITH-transaction mode,
		// fall back to INSERT-per-row so truncation and inserts stay in the tx.
		if cfg.withoutTransaction && canUseCopy(insertPolicy) && cfg.dsn != "" {
			if err := loadTableCopy(ctx, cfg.dsn, table, dataPaths[i]); err != nil {
				return err
			}
		} else {
			var tq execQuerier = q
			var tableTx *sql.Tx

			if cfg.withoutTransaction {
				tableTx, err = dbConn.BeginTx(ctx, nil)
				if err != nil {
					return fmt.Errorf("begin transaction for table %q: %w", table.Name, err)
				}
				tq = tableTx
			}

			if err := loadTable(ctx, tq, table, dataPaths[i], insertPolicy); err != nil {
				if tableTx != nil {
					_ = tableTx.Rollback()
				}
				return err
			}

			if tableTx != nil {
				if err := tableTx.Commit(); err != nil {
					return fmt.Errorf("commit table %q: %w", table.Name, err)
				}
			}
		}

		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_end",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(meta.Tables),
			Elapsed: time.Since(startedAt),
		})
	}

	// Restore/sync sequences before committing the main transaction so sequence
	// failures roll back loaded data in the default transactional mode. With
	// WithoutTransaction, table loads have already committed by design.
	seqQ := execQuerier(dbConn)
	if tx != nil {
		seqQ = tx
	}
	if err := RestoreSequencesFromMetadata(ctx, seqQ, meta, schemaFilter); err != nil {
		return fmt.Errorf("restore sequences: %w", err)
	}
	if err := SyncSequencesToData(ctx, seqQ, schemaFilter); err != nil {
		return fmt.Errorf("sync sequences to data: %w", err)
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	return nil
}

// isTableNotFoundErr returns true when the error is from validateSchema
// reporting that a table from metadata is missing in the target schema.
func isTableNotFoundErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found in target schema")
}

// applySchemaSQL runs psql -f schema.sql against the target DSN if the file exists.
func applySchemaSQL(ctx context.Context, dsn, inputDir string) error {
	schemaPath := filepath.Join(inputDir, "schema.sql")
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		return errors.New("schema.sql not found in input directory")
	}
	if _, err := psqlLookPath("psql"); err != nil {
		return fmt.Errorf("psql not on PATH, cannot apply schema.sql")
	}
	rawSchema, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read schema.sql: %w", err)
	}
	sanitizedSchema, err := schemasql.Sanitize(rawSchema)
	if err != nil {
		return fmt.Errorf("sanitize schema.sql: %w", err)
	}
	tmp, err := os.CreateTemp(inputDir, ".schema-*.sql")
	if err != nil {
		return fmt.Errorf("create sanitized schema.sql: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(sanitizedSchema); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write sanitized schema.sql: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close sanitized schema.sql: %w", err)
	}
	cleanDSN, password, err := connections.SubprocessDSN(dsn)
	if err != nil {
		return err
	}
	args := []string{"-v", "ON_ERROR_STOP=1", "-f", tmpPath, cleanDSN}
	env := stripSensitiveEnv(os.Environ())
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}
	var stderr strings.Builder
	if err := runPSQLSchema(ctx, args, env, &stderr); err != nil {
		return fmt.Errorf("psql schema.sql: %w (stderr: %s)", err, connections.RedactMessage(stderr.String()))
	}
	return nil
}

func stripSensitiveEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		switch strings.ToUpper(key) {
		case "PGPASSWORD", "PGSSLKEY", "PGSSLCERT":
			continue
		}
		out = append(out, kv)
	}
	return out
}
