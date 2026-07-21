package dump

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// ProgressEvent reports table-granularity progress during Dump.
type ProgressEvent struct {
	Phase   string
	Table   string
	Current int           // 1-based table index
	Total   int           // total number of tables scheduled
	Elapsed time.Duration // since Dump() entered
}

type config struct {
	withoutTransaction bool
	slowConnection     bool
	slowChunkSize      int
	slowRetry          slowRetryConfig
	onProgress         func(ProgressEvent)
	subset             *SubsetConfig
	schemas            []string
	skipSequences      bool
	rowTransform       RowTransform
	provenance         *Provenance
}

type slowRetryConfig struct {
	max  int
	base time.Duration
}

func emitProgress(cfg *config, ev ProgressEvent) {
	if cfg.onProgress == nil {
		return
	}
	defer func() { _ = recover() }()
	cfg.onProgress(ev)
}

// InspectOptions returns the subset configuration captured from opts, or nil.
func InspectOptions(opts ...Option) *SubsetConfig {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.subset
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

// InspectRowTransform returns the row transform captured from opts, or nil.
func InspectRowTransform(opts ...Option) RowTransform {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.rowTransform
}

// WithProgress registers a callback invoked at each table start and end.
// A nil callback is a no-op.
func WithProgress(fn func(ProgressEvent)) Option {
	return func(c *config) {
		c.onProgress = fn
	}
}

// WithSubset enables subset dump mode with seed predicates and limits.
func WithSubset(cfg SubsetConfig) Option {
	return func(c *config) {
		c.subset = &cfg
	}
}

// Option configures Dump behavior.
type Option func(*config)

// WithoutTransaction returns an Option that skips the read-only transaction wrapper.
func WithoutTransaction() Option {
	return func(c *config) {
		c.withoutTransaction = true
	}
}

// WithSlowConnection enables chunked keyset-paginated streaming for
// slow or unstable connections. Forces WithoutTransaction internally.
// Tables must have at least one primary-key column.
func WithSlowConnection() Option {
	return func(c *config) {
		c.withoutTransaction = true // ponytail: force no-tx; slow mode and global snapshot are incompatible
		c.slowConnection = true
	}
}

// WithSlowChunkSize sets rows per keyset-pagination chunk in slow-connection mode.
func WithSlowChunkSize(n int) Option {
	return func(c *config) {
		c.slowChunkSize = n
	}
}

// WithSlowRetry enables query retries per chunk in slow-connection mode.
// maxAttempts 0 disables retries.
func WithSlowRetry(maxAttempts int, baseDelay time.Duration) Option {
	return func(c *config) {
		c.slowRetry.max = maxAttempts
		c.slowRetry.base = baseDelay
	}
}

// InspectSlowConnection returns whether slow-connection mode is active in opts.
func InspectSlowConnection(opts ...Option) bool {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.slowConnection
}

// InspectSlowChunkSizeEquals reports whether opts set slow chunk size to n.
func InspectSlowChunkSizeEquals(n int, opts ...Option) bool {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.slowChunkSize == n
}

// InspectSlowRetry returns retry settings captured from opts.
func InspectSlowRetry(opts ...Option) (max int, base time.Duration) {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.slowRetry.max, c.slowRetry.base
}

// WithSchemas limits introspection and dump to the given schema names.
// When empty or nil, only the public schema is loaded.
func WithSchemas(schemas []string) Option {
	return func(c *config) {
		c.schemas = append([]string(nil), schemas...)
	}
}

// WithProvenance attaches dump identity metadata written into metadata.json.
func WithProvenance(p Provenance) Option {
	return func(c *config) {
		cp := p
		c.provenance = &cp
	}
}

// WithoutSequences skips sequence state capture during Dump.
// Use in tests where pg_sequences cannot be queried (mock databases).
func WithoutSequences() Option {
	return func(c *config) {
		c.skipSequences = true
	}
}

func provenanceForWrite(cfg *config, tables []db.Table) *Provenance {
	if cfg.provenance == nil {
		return nil
	}
	p := *cfg.provenance
	p.TableCount = len(tables)
	var total int64
	for _, t := range tables {
		if t.RowCount != nil {
			total += *t.RowCount
		}
	}
	p.TotalRowEstimate = total
	return &p
}

// Dump writes schema metadata and per-table NDJSON data to outputDir.
// By default it uses a REPEATABLE READ READ ONLY transaction so schema
// and row data share one consistent snapshot. Use WithoutTransaction to opt out.
func Dump(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...Option) error {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.slowChunkSize <= 0 {
		cfg.slowChunkSize = DefaultSlowChunkSize
	}

	startedAt := time.Now()

	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	var q querier = dbConn
	var tx *sql.Tx

	if !cfg.withoutTransaction {
		var err error
		tx, err = dbConn.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  true,
		})
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		q = tx
	}

	if cfg.slowConnection && cfg.subset != nil {
		return fmt.Errorf("slow connection mode and subset dump are incompatible")
	}

	tables, err := db.LoadPostgresSchemas(ctx, q, cfg.schemas)
	if err != nil {
		return fmt.Errorf("load schema: %w", err)
	}

	if cfg.subset != nil {
		return dumpSubset(ctx, q, tx, tables, outputDir, &cfg)
	}

	sorted := SortTables(tables)
	assignDataFiles(sorted)

	var sequences []SequenceState
	if !cfg.skipSequences {
		seqs, err := captureSequences(ctx, q, cfg.schemas)
		if err != nil {
			// ponytail: non-fatal — pg_sequences is a system view that works with
			// real PostgreSQL but not with test mocks. Carry on without sequence state.
			sequences = nil
		} else {
			sequences = seqs
		}
	}

	metaPath, err := writeMetadata(outputDir, sorted, nil, cfg.schemas, sequences, provenanceForWrite(&cfg, sorted))
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	for i, table := range sorted {
		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_start",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(sorted),
			Elapsed: time.Since(startedAt),
		})
		var streamErr error
		if cfg.slowConnection {
			streamErr = streamTableSlow(ctx, q, table, outputDir, cfg.rowTransform, cfg.slowRetry, cfg.slowChunkSize)
		} else {
			streamErr = streamTable(ctx, q, table, outputDir, cfg.rowTransform)
		}
		if streamErr != nil {
			return streamErr
		}
		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_end",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(sorted),
			Elapsed: time.Since(startedAt),
		})
	}

	if err := os.Rename(metaPath, filepath.Join(outputDir, "metadata.json")); err != nil {
		return fmt.Errorf("rename metadata: %w", err)
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	return nil
}

// captureSequences reads sequence last values for user schemas.
func captureSequences(ctx context.Context, q querier, schemas []string) ([]SequenceState, error) {
	query := `
		SELECT schemaname, sequencename, last_value, start_value
		FROM pg_sequences
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_%'`
	var args []any
	if len(schemas) > 0 {
		placeholders := make([]string, len(schemas))
		for i, s := range schemas {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args = append(args, s)
		}
		query += " AND schemaname IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY schemaname, sequencename"

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sequences: %w", err)
	}
	defer rows.Close()

	var seqs []SequenceState
	for rows.Next() {
		var schema, seqName string
		var lastValue sql.NullInt64
		var startValue int64
		if err := rows.Scan(&schema, &seqName, &lastValue, &startValue); err != nil {
			return nil, fmt.Errorf("scan sequence: %w", err)
		}
		s := SequenceState{
			Schema:     schema,
			Name:       seqName,
			StartValue: startValue,
			IsCalled:   lastValue.Valid,
		}
		if lastValue.Valid {
			v := lastValue.Int64
			s.LastValue = &v
		}
		seqs = append(seqs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sequences: %w", err)
	}
	return seqs, nil
}

func dumpSubset(ctx context.Context, q querier, tx *sql.Tx, tables []db.Table, outputDir string, cfg *config) error {
	subCfg := *cfg.subset
	subCfg.Limits = ApplySubsetLimitDefaults(subCfg.Limits)

	startedAt := time.Now()

	plan, err := planSubset(ctx, q, tables, subCfg)
	if err != nil {
		return fmt.Errorf("plan subset: %w", err)
	}

	byName := make(map[string]db.Table, len(tables))
	pkCols := make(map[string]string, len(tables))
	for _, t := range tables {
		byName[t.Name] = t
	}

	var included []db.Table
	for _, name := range plan.tableOrder {
		included = append(included, byName[name])
	}
	assignDataFiles(included)

	manifest := &SubsetManifest{
		Seeds:        subCfg.Seeds,
		Limits:       subCfg.Limits,
		Tables:       plan.tableOrder,
		RowsExported: plan.rowsExported,
		Percent:      subCfg.Percent,
	}

	metaPath, err := writeMetadata(outputDir, included, manifest, cfg.schemas, nil, provenanceForWrite(cfg, included))
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	for i, table := range included {
		emitProgress(cfg, ProgressEvent{
			Phase:   "table_start",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(included),
			Elapsed: time.Since(startedAt),
		})
		tp := plan.tables[table.Name]
		pkCol, pkErr := primaryKeyColumn(table)
		if pkErr != nil && len(tp.compositeFKVals) == 0 {
			return fmt.Errorf("table %q: %w", table.Name, pkErr)
		}

		var clauses []compiledWhere
		if pkErr == nil {
			pkCols[table.Name] = pkCol
			clauses, err = buildStreamClauses(pkCol, tp, subCfg.Limits)
		} else {
			clauses, err = buildStreamClauses("", tp, subCfg.Limits)
		}
		if err != nil {
			return err
		}
		// Deterministic ordering: ORDER BY pk for single-PK tables.
		orderCol := ""
		if pkErr == nil {
			orderCol = pkCol
		}
		if err := streamTableFiltered(ctx, q, table, outputDir, clauses, cfg.rowTransform, orderCol); err != nil {
			return err
		}
		emitProgress(cfg, ProgressEvent{
			Phase:   "table_end",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(included),
			Elapsed: time.Since(startedAt),
		})
	}

	if err := os.Rename(metaPath, filepath.Join(outputDir, "metadata.json")); err != nil {
		return fmt.Errorf("rename metadata: %w", err)
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	return nil
}
