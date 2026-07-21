package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/jackc/pgx/v5"
)

// copyConn abstracts pgconn COPY operations for testability.
type copyConn interface {
	CopyTo(ctx context.Context, w io.Writer, sql string) error
	CopyFrom(ctx context.Context, r io.Reader, sql string) error
	Close(context.Context) error
}

// pgxCopyConn wraps a *pgx.Conn to implement copyConn.
type pgxCopyConn struct {
	conn *pgx.Conn
}

func (c *pgxCopyConn) CopyTo(ctx context.Context, w io.Writer, sql string) error {
	_, err := c.conn.PgConn().CopyTo(ctx, w, sql)
	return err
}

func (c *pgxCopyConn) CopyFrom(ctx context.Context, r io.Reader, sql string) error {
	_, err := c.conn.PgConn().CopyFrom(ctx, r, sql)
	return err
}

func (c *pgxCopyConn) Close(ctx context.Context) error {
	return c.conn.Close(ctx)
}

// openCopyConn opens a pgx connection for COPY streaming.
// Overridable for testing.
var openCopyConn = func(ctx context.Context, dsn string) (copyConn, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &pgxCopyConn{conn: conn}, nil
}

// loadSchemasFunc wraps db.LoadPostgresSchemas with a concrete *sql.DB parameter.
// Overridable for testing.
var loadSchemasFunc = func(ctx context.Context, q *sql.DB, schemas []string) ([]db.Table, error) {
	return db.LoadPostgresSchemas(ctx, q, schemas)
}

// listSchemaNamesFunc wraps db.ListPostgresSchemaNames.
// Overridable for testing.
var listSchemaNamesFunc = func(ctx context.Context, q *sql.DB) ([]string, error) {
	return db.ListPostgresSchemaNames(ctx, q)
}

// restoreSequencesFunc wraps restoreSequences for testability.
var restoreSequencesFunc = restoreSequences

var copyStreamCleanupTimeout = 10 * time.Second

// CopyStreamStrategy streams table data directly from source to target
// using pgx native COPY without intermediate NDJSON files.
type CopyStreamStrategy struct {
	Runner CommandRunner
}

func (s *CopyStreamStrategy) Name() string { return "logical-stream" }

func (s *CopyStreamStrategy) Execute(ctx context.Context, opts Options) error {
	if opts.SourceDSN == "" {
		return fmt.Errorf("source DSN is required")
	}
	if opts.CloneName == "" {
		return fmt.Errorf("clone name is required")
	}

	startedAt := time.Now()

	targetDSN := opts.TargetDSN
	var err error
	if targetDSN == "" {
		targetDSN, err = RewriteDSN(opts.SourceDSN, opts.CloneName)
		if err != nil {
			return fmt.Errorf("build target DSN: %w", err)
		}
	} else {
		targetDSN, err = RewriteDSN(targetDSN, opts.CloneName)
		if err != nil {
			return fmt.Errorf("build target DSN: %w", err)
		}
	}

	// Create target database first so source enumeration is covered by cleanup.
	var adminDSN string
	if !opts.SkipCreate {
		adminDSN, err = RewriteDSN(targetDSN, "postgres")
		if err != nil {
			return fmt.Errorf("build admin DSN: %w", err)
		}
		step := 1
		totalSteps := 1 // placeholder; recalculated after enumeration
		reportProgressEvent(opts, ProgressEvent{
			Phase:   "creating_target",
			Step:    "creating target database",
			Current: step,
			Total:   totalSteps,
			Elapsed: time.Since(startedAt),
		})
		if err := CreateDatabase(ctx, adminDSN, opts.CloneName); err != nil {
			return fmt.Errorf("create target database: %w", err)
		}
	}

	// Enumerate tables from source. If CreateDatabase succeeded above and
	// enumeration fails, the cleanup wrapper in the error return path below
	// will drop the target database.
	srcDB, err := sqlOpenDB(opts.SourceDSN)
	if err != nil {
		return s.wrapWithCleanup("open source", adminDSN, opts.CloneName, err)
	}
	defer srcDB.Close()

	schemaNames := SchemasFromOptions(opts)
	if len(schemaNames) == 0 {
		schemaNames, err = listSchemaNamesFunc(ctx, srcDB)
		if err != nil {
			return s.wrapWithCleanup("list schemas", adminDSN, opts.CloneName, err)
		}
	}

	tables, err := loadSchemasFunc(ctx, srcDB, schemaNames)
	if err != nil {
		return s.wrapWithCleanup("load schema", adminDSN, opts.CloneName, err)
	}

	sorted := dump.SortTables(tables)

	// Total = 2 (create+replay) + len(sorted) (copy) + 1 (sequences)
	totalSteps := 2 + len(sorted) + 1

	step := 0
	if !opts.SkipCreate {
		step = 1
	}

	// Replay schema and stream data inside cleanup wrapper.
	if !opts.SkipCreate {
		if err := s.postCreate(ctx, opts, srcDB, targetDSN, sorted, startedAt, totalSteps, step); err != nil {
			s.cleanup(adminDSN, opts.CloneName, err)
			return err
		}
		return nil
	}
	return s.postCreate(ctx, opts, srcDB, targetDSN, sorted, startedAt, totalSteps, step)
}

// wrapWithCleanup drops the newly created database when adminDSN is non-empty
// and returns a wrapped error. It is a no-op when adminDSN is empty (SkipCreate).
func (s *CopyStreamStrategy) wrapWithCleanup(label, adminDSN, cloneName string, err error) error {
	if adminDSN == "" {
		return fmt.Errorf("%s: %w", label, err)
	}
	s.cleanup(adminDSN, cloneName, err)
	return fmt.Errorf("%s: %w", label, err)
}

func (s *CopyStreamStrategy) cleanup(adminDSN, cloneName string, primary error) {
	ctx, cancel := context.WithTimeout(context.Background(), copyStreamCleanupTimeout)
	defer cancel()
	if dropErr := dropDatabaseFunc(ctx, adminDSN, cloneName); dropErr != nil {
		fmt.Fprintf(os.Stderr, "warning: cleanup drop database %q failed: %v (original error: %v)\n", cloneName, dropErr, primary)
	}
}

// postCreate handles all steps after database creation (or when SkipCreate is set).
func (s *CopyStreamStrategy) postCreate(ctx context.Context, opts Options, srcDB *sql.DB, targetDSN string, sorted []db.Table, startedAt time.Time, totalSteps, currentStep int) error {
	step := currentStep

	// Replay schema using pg_dump --schema-only | psql
	runner := commandRunnerForProgress(s.Runner, opts.ProgressFn)

	srcCleanDSN, srcPw, cleanErr := StripPassword(opts.SourceDSN)
	if cleanErr != nil {
		return fmt.Errorf("clean source DSN: %w", cleanErr)
	}
	tgtCleanDSN, tgtPw, cleanErr := StripPassword(targetDSN)
	if cleanErr != nil {
		return fmt.Errorf("clean target DSN: %w", cleanErr)
	}
	if srcPw != "" && tgtPw != "" && srcPw != tgtPw {
		return fmt.Errorf("source and target DSNs have different passwords: copy-stream pipe shares a single PGPASSWORD environment; use matching credentials or connect via ~/.pgpass")
	}
	srcArgs := []string{"--schema-only", "--no-owner", "--no-acl", srcCleanDSN}
	tgtArgs := []string{"-v", "ON_ERROR_STOP=1", tgtCleanDSN}
	env := map[string]string{}
	if srcPw != "" {
		env["PGPASSWORD"] = srcPw
	} else if tgtPw != "" {
		env["PGPASSWORD"] = tgtPw
	}
	step++
	reportProgressEvent(opts, ProgressEvent{
		Phase:   "replaying_schema",
		Step:    "replaying schema",
		Current: step,
		Total:   totalSteps,
		Elapsed: time.Since(startedAt),
	})
	if err := runner.PipeWithEnv(ctx, env, "pg_dump", srcArgs, "psql", tgtArgs); err != nil {
		return fmt.Errorf("replay schema: %w", err)
	}

	// Open pgx connections for streaming
	srcConn, err := openCopyConn(ctx, opts.SourceDSN)
	if err != nil {
		return fmt.Errorf("open source copy connection: %w", err)
	}
	defer srcConn.Close(ctx)

	tgtConn, err := openCopyConn(ctx, targetDSN)
	if err != nil {
		return fmt.Errorf("open target copy connection: %w", err)
	}
	defer tgtConn.Close(ctx)

	// Stream each table in FK-safe order
	for _, table := range sorted {
		step++
		reportProgressEvent(opts, ProgressEvent{
			Phase:   "copying_table",
			Step:    "copying table " + quoteQualifiedTable(table.Schema, table.Name),
			Table:   quoteQualifiedTable(table.Schema, table.Name),
			Current: step,
			Total:   totalSteps,
			Elapsed: time.Since(startedAt),
		})
		if err := copyTable(ctx, srcConn, tgtConn, table.Schema, table.Name); err != nil {
			return fmt.Errorf("copy table %q: %w", quoteQualifiedTable(table.Schema, table.Name), err)
		}
	}

	// Open target stdlib connection for sequence restoration
	tgtDB, err := sqlOpenDB(targetDSN)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer tgtDB.Close()

	// Restore sequence values so serial/identity-backed tables won't duplicate IDs
	step++
	reportProgressEvent(opts, ProgressEvent{
		Phase:   "restoring_sequences",
		Step:    "restoring sequences",
		Current: step,
		Total:   totalSteps,
		Elapsed: time.Since(startedAt),
	})
	if err := restoreSequencesFunc(ctx, srcDB, tgtDB); err != nil {
		return fmt.Errorf("restore sequences: %w", err)
	}

	return nil
}

func quoteQualifiedTable(schema, name string) string {
	return quoteIdentifier(schema) + "." + quoteIdentifier(name)
}

func copyTable(ctx context.Context, srcConn, tgtConn copyConn, schema, tableName string) error {
	pr, pw := io.Pipe()

	var srcErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		sql := fmt.Sprintf("COPY %s TO STDOUT", quoteQualifiedTable(schema, tableName))
		if err := srcConn.CopyTo(ctx, pw, sql); err != nil {
			srcErr = err
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()

	sql := fmt.Sprintf("COPY %s FROM STDIN", quoteQualifiedTable(schema, tableName))
	tgtErr := tgtConn.CopyFrom(ctx, pr, sql)
	pr.Close()
	<-done

	if tgtErr != nil {
		if srcErr != nil {
			return fmt.Errorf("copy %s: %w", quoteQualifiedTable(schema, tableName),
				errors.Join(fmt.Errorf("target: %w", tgtErr), fmt.Errorf("source: %w", srcErr)))
		}
		return fmt.Errorf("copy to target: %w", tgtErr)
	}
	if srcErr != nil {
		return fmt.Errorf("copy from source: %w", srcErr)
	}
	return nil
}

// restoreSequences reads user-visible sequence last values from source and
// applies them on target using setval. This prevents duplicate IDs on
// serial/identity-backed tables after a clone.
func restoreSequences(ctx context.Context, srcDB, tgtDB *sql.DB) error {
	const listQuery = `
		SELECT schemaname, sequencename, last_value, start_value
		FROM pg_sequences
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_%'
		ORDER BY schemaname, sequencename`

	rows, err := srcDB.QueryContext(ctx, listQuery)
	if err != nil {
		return fmt.Errorf("list sequences: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schema, seqName string
		var lastValue sql.NullInt64
		var startValue int64
		if err := rows.Scan(&schema, &seqName, &lastValue, &startValue); err != nil {
			return fmt.Errorf("scan sequence: %w", err)
		}

		var value int64
		var isCalled bool
		if lastValue.Valid {
			value = lastValue.Int64
			isCalled = true
		} else {
			value = startValue
			isCalled = false
		}

		setSQL := fmt.Sprintf(
			"SELECT setval(%s::regclass, %d, %t)",
			quoteLiteral(quoteQualifiedTable(schema, seqName)),
			value,
			isCalled,
		)
		if _, err := tgtDB.ExecContext(ctx, setSQL); err != nil {
			return fmt.Errorf("setval %s.%s: %w", schema, seqName, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list sequences: %w", err)
	}
	return nil
}

// Ensure CopyStreamStrategy implements Strategy.
var _ Strategy = (*CopyStreamStrategy)(nil)
