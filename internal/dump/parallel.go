package dump

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
)

const maxParallelWorkers = 16

const parallelStagingPrefix = ".parallel-staging-"

var snapshotIDPattern = regexp.MustCompile(`^[0-9A-F]+-[0-9A-F]+-[0-9]+$`)

// ParallelPlan is a prepared parallel dump. Call Run to execute and Close to release resources.
type ParallelPlan struct {
	cfg         config
	db          *sql.DB
	outputDir   string
	tables      []db.Table
	sequences   []SequenceState
	metaTmpPath string
	stagingDir  string
	startedAt   time.Time
	coordinator *snapshotCoordinator
	published   bool
	progressMu  sync.Mutex
}

// Prepare builds a parallel dump plan. Effective workers must be greater than one.
func Prepare(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...Option) (*ParallelPlan, error) {
	cfg, workers, err := applyDumpConfig(opts...)
	if err != nil {
		return nil, err
	}
	if workers <= 1 {
		return nil, fmt.Errorf("parallel prepare requires workers > 1")
	}
	if err := validateParallelDumpOptions(&cfg, dbConn, workers); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	coordinator, err := newSnapshotCoordinator(ctx, dbConn)
	if err != nil {
		return nil, err
	}

	tables, sequences, err := introspectParallelPlan(ctx, coordinator.q, &cfg)
	if err != nil {
		coordinator.close()
		return nil, err
	}

	sorted := SortTables(tables)
	assignDataFiles(sorted)

	stagingDir, err := os.MkdirTemp(outputDir, parallelStagingPrefix)
	if err != nil {
		coordinator.close()
		return nil, fmt.Errorf("create parallel staging directory: %w", err)
	}

	metaTmpPath, err := writeMetadata(outputDir, sorted, nil, cfg.schemas, sequences, provenanceForWrite(&cfg, sorted))
	if err != nil {
		coordinator.close()
		_ = os.RemoveAll(stagingDir)
		return nil, fmt.Errorf("write metadata: %w", err)
	}

	return &ParallelPlan{
		cfg:         cfg,
		db:          dbConn,
		outputDir:   outputDir,
		tables:      sorted,
		sequences:   sequences,
		metaTmpPath: metaTmpPath,
		stagingDir:  stagingDir,
		startedAt:   time.Now(),
		coordinator: coordinator,
	}, nil
}

// Run executes the prepared parallel dump.
func (p *ParallelPlan) Run(ctx context.Context) error {
	if p == nil {
		return fmt.Errorf("parallel plan is nil")
	}
	workers := effectiveWorkers(p.cfg.workers)
	err := runParallelDump(ctx, p, workers)
	if err != nil {
		cleanupParallelArtifacts(p.outputDir, p.stagingDir, p.metaTmpPath, p.tables)
		return err
	}
	if err := publishParallelArtifacts(p); err != nil {
		cleanupParallelArtifacts(p.outputDir, p.stagingDir, p.metaTmpPath, p.tables)
		return err
	}
	p.published = true
	return nil
}

// Close releases snapshot coordinator resources and cleans unpublished run-owned artifacts.
func (p *ParallelPlan) Close() error {
	if p == nil {
		return nil
	}
	if !p.published {
		cleanupParallelArtifacts(p.outputDir, p.stagingDir, p.metaTmpPath, p.tables)
	}
	if p.coordinator == nil {
		return nil
	}
	return p.coordinator.close()
}

func applyDumpConfig(opts ...Option) (config, int, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.slowChunkSize <= 0 {
		cfg.slowChunkSize = DefaultSlowChunkSize
	}
	if err := validateDumpOptions(&cfg); err != nil {
		return config{}, 0, err
	}
	return cfg, effectiveWorkers(cfg.workers), nil
}

func effectiveWorkers(workers int) int {
	if workers <= 0 {
		return 1
	}
	return workers
}

func validateParallelDumpOptions(cfg *config, dbConn *sql.DB, workers int) error {
	if workers > maxParallelWorkers {
		return fmt.Errorf("parallel dump workers must be between 1 and %d", maxParallelWorkers)
	}
	if cfg.withoutTransaction {
		return fmt.Errorf("parallel dump workers require a read-only transaction snapshot")
	}
	if cfg.subset != nil {
		return fmt.Errorf("parallel dump workers are incompatible with subset dump")
	}
	if cfg.slowConnection || hasChunkPolicy(cfg) {
		return fmt.Errorf("parallel dump workers are incompatible with chunk or slow-connection mode")
	}
	if dbConn != nil {
		maxConns := dbConn.Stats().MaxOpenConnections
		if maxConns > 0 && maxConns < workers+1 {
			return fmt.Errorf("parallel dump requires db max_open_conns >= %d (got %d)", workers+1, maxConns)
		}
	}
	return nil
}

func quoteSnapshotLiteral(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !snapshotIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid snapshot id %q", id)
	}
	return "'" + strings.ReplaceAll(id, "'", "''") + "'", nil
}

type snapshotCoordinator struct {
	conn        *sql.Conn
	tx          *sql.Tx
	q           querier
	snapshotLit string
	snapshotRaw string
	exported    bool
}

func newSnapshotCoordinator(ctx context.Context, dbConn *sql.DB) (*snapshotCoordinator, error) {
	conn, err := dbConn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("pin exporter connection: %w", err)
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("begin exporter transaction: %w", err)
	}
	var snapshotID string
	if err := tx.QueryRowContext(ctx, "SELECT pg_export_snapshot()").Scan(&snapshotID); err != nil {
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	lit, err := quoteSnapshotLiteral(snapshotID)
	if err != nil {
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, err
	}
	if parallelTestHooks.onSnapshotExported != nil {
		parallelTestHooks.onSnapshotExported()
	}
	return &snapshotCoordinator{
		conn:        conn,
		tx:          tx,
		q:           tx,
		snapshotLit: lit,
		snapshotRaw: snapshotID,
		exported:    true,
	}, nil
}

func (c *snapshotCoordinator) close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	var err error
	if c.tx != nil {
		err = c.tx.Rollback()
		c.tx = nil
	}
	if c.conn != nil {
		if closeErr := c.conn.Close(); err == nil {
			err = closeErr
		}
		c.conn = nil
	}
	return err
}

type workerSession struct {
	conn *sql.Conn
	tx   *sql.Tx
	q    querier
}

func newWorkerSession(ctx context.Context, dbConn *sql.DB, snapshotLit string) (*workerSession, error) {
	conn, err := dbConn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("pin worker connection: %w", err)
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("begin worker transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SET TRANSACTION SNAPSHOT "+snapshotLit); err != nil {
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, fmt.Errorf("import snapshot: %w", err)
	}
	return &workerSession{conn: conn, tx: tx, q: tx}, nil
}

func (s *workerSession) close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.tx != nil {
		err = s.tx.Rollback()
		s.tx = nil
	}
	if s.conn != nil {
		if closeErr := s.conn.Close(); err == nil {
			err = closeErr
		}
		s.conn = nil
	}
	return err
}

// parallelWorkerSessionOpener pins worker sessions in production; tests override.
var parallelWorkerSessionOpener = func(ctx context.Context, dbConn *sql.DB, snapshotLit string) (querier, func() error, error) {
	session, err := newWorkerSession(ctx, dbConn, snapshotLit)
	if err != nil {
		return nil, nil, err
	}
	return session.q, session.close, nil
}

func introspectParallelPlan(ctx context.Context, q querier, cfg *config) ([]db.Table, []SequenceState, error) {
	tables, err := db.LoadPostgresSchemas(ctx, q, cfg.schemas)
	if err != nil {
		return nil, nil, fmt.Errorf("load schema: %w", err)
	}
	if cfg.selection != nil {
		filtered, selProv, err := PlanTableSelection(tables, cfg.selection, cfg.selectionIgnored)
		if err != nil {
			return nil, nil, err
		}
		tables = filtered
		if cfg.provenance != nil {
			cfg.provenance.TableSelection = &selProv
		}
		for _, w := range selProv.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
	}
	if hasChunkPolicy(cfg) {
		return nil, nil, fmt.Errorf("parallel dump workers are incompatible with chunk or slow-connection mode")
	}

	seqSchemas := cfg.schemas
	if cfg.selection != nil && (len(cfg.selection.Includes) > 0 || len(cfg.selection.Excludes) > 0) {
		seqSchemas = schemasFromTables(tables)
	}
	var sequences []SequenceState
	if !cfg.skipSequences {
		seqs, err := captureSequences(ctx, q, seqSchemas)
		if err == nil {
			sequences = seqs
		}
	}
	return tables, sequences, nil
}

type parallelTableJob struct {
	index int
	table db.Table
}

func runParallelDump(ctx context.Context, plan *ParallelPlan, workers int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan parallelTableJob, len(plan.tables))
	var wg sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once
	var completed atomic.Int32

	recordErr := func(err error) {
		if err == nil {
			return
		}
		firstErrOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}
	emit := func(ev ProgressEvent) {
		plan.progressMu.Lock()
		emitProgress(&plan.cfg, ev)
		plan.progressMu.Unlock()
	}

	for workerID := 1; workerID <= workers; workerID++ {
		wg.Add(1)
		go func(workerNum int) {
			defer wg.Done()
			q, closeSession, err := parallelWorkerSessionOpener(ctx, plan.db, plan.coordinator.snapshotLit)
			if err != nil {
				recordErr(err)
				return
			}
			defer closeSession()

			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				qualified := qualifiedName(job.table.Schema, job.table.Name)
				done := int(completed.Load())
				emit(ProgressEvent{
					Phase:   "table_start",
					Table:   qualified,
					Worker:  workerNum,
					Current: done + 1,
					Total:   len(plan.tables),
					Elapsed: time.Since(plan.startedAt),
				})
				stagingPath := parallelStagingPath(plan.stagingDir, job.table)
				if err := parallelStreamTable(ctx, q, job.table, stagingPath, plan.cfg.rowTransform); err != nil {
					recordErr(err)
					return
				}
				n := completed.Add(1)
				emit(ProgressEvent{
					Phase:   "table_end",
					Table:   qualified,
					Worker:  workerNum,
					Current: int(n),
					Total:   len(plan.tables),
					Elapsed: time.Since(plan.startedAt),
				})
			}
		}(workerID)
	}

	go func() {
		for i, table := range plan.tables {
			select {
			case <-ctx.Done():
				close(jobs)
				return
			case jobs <- parallelTableJob{index: i, table: table}:
			}
		}
		close(jobs)
	}()

	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return plan.coordinator.close()
}

func parallelStagingPath(stagingDir string, table db.Table) string {
	name := hex.EncodeToString([]byte(table.Schema)) + "." + hex.EncodeToString([]byte(table.Name)) + ".ndjson"
	return filepath.Join(stagingDir, name)
}

func publishParallelArtifacts(plan *ParallelPlan) error {
	for _, table := range plan.tables {
		src := parallelStagingPath(plan.stagingDir, table)
		dst := tableDataPath(plan.outputDir, table)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("publish table %q: %w", table.Name, err)
		}
	}
	if err := os.Rename(plan.metaTmpPath, filepath.Join(plan.outputDir, "metadata.json")); err != nil {
		return fmt.Errorf("rename metadata: %w", err)
	}
	_ = os.RemoveAll(plan.stagingDir)
	return nil
}

func cleanupParallelArtifacts(outputDir, stagingDir, metaTmpPath string, tables []db.Table) {
	_ = os.RemoveAll(stagingDir)
	_ = os.Remove(metaTmpPath)
	_ = os.Remove(filepath.Join(outputDir, "metadata.json"))
	for _, table := range tables {
		_ = os.Remove(tableDataPath(outputDir, table))
		_ = os.Remove(tableDataPath(outputDir, table) + ".tmp")
	}
}

// parallelStreamTable is the streaming seam used by parallel workers (overridable in tests).
var parallelStreamTable = streamTableToPath

// parallelTestHooks supports deterministic integration tests without sleeps.
var parallelTestHooks struct {
	onSnapshotExported func()
}
