package dump

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// PreparedDumpPlan is the canonical per-table strategy plan passed to validators.
type PreparedDumpPlan struct {
	Strategies []TableStrategyRecord
}

// PlanValidator validates a prepared dump plan before metadata or table output.
type PlanValidator func(PreparedDumpPlan) error

// ProgressEvent reports table-granularity progress during Dump.
type ProgressEvent struct {
	Phase   string
	Table   string
	Worker  int           // parallel worker id; zero in serial dumps
	Current int           // 1-based table index or completed count in parallel dumps
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
	selection          *SelectionPolicy
	selectionIgnored   []IgnoredFileLine
	chunkPolicy        *ChunkPolicy
	chunkIgnored       []IgnoredFileLine
	workers            int
	planValidator      PlanValidator
	requireSafeKey     bool
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

// WithRequireSafeKey rejects ctid resume. Chunk and slow dumps fail when a
// table has no primary key or eligible unique key.
func WithRequireSafeKey() Option {
	return func(c *config) {
		c.requireSafeKey = true
	}
}

// WithSlowConnection enables chunked keyset-paginated streaming for
// slow or unstable connections. Forces WithoutTransaction internally.
// Each table uses its primary key, an eligible unique index, or ctid
// resume when no safe key exists. ctid can move after VACUUM or updates.
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

// InspectWithoutTransaction reports whether opts skip the read-only transaction wrapper.
func InspectWithoutTransaction(opts ...Option) bool {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.withoutTransaction
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

// WithTableSelection limits dump to exact qualified tables using include/exclude policy.
func WithTableSelection(policy SelectionPolicy, ignored []IgnoredFileLine) Option {
	return func(c *config) {
		cp := policy
		c.selection = &cp
		if len(ignored) > 0 {
			c.selectionIgnored = append([]IgnoredFileLine(nil), ignored...)
		}
	}
}

// WithChunkTables marks exact qualified tables for keyset-chunked streaming.
func WithChunkTables(tables []QualifiedTable) Option {
	if len(tables) == 0 {
		return func(c *config) {}
	}
	requests := make([]SelectorEntry, len(tables))
	for i, t := range tables {
		requests[i] = SelectorEntry{
			Table: t,
			Raw:   t.Normalized(),
			Source: SelectorSource{
				Kind: "programmatic",
				Name: "WithChunkTables",
			},
		}
	}
	return WithChunkTablePolicy(ChunkPolicy{Requests: requests}, nil)
}

// WithChunkTablePolicy sets chunk-table selectors with full provenance sources.
// Active chunk selectors force WithoutTransaction so per-chunk retries and
// checkpoint resume are not tied to a single aborted read-only transaction.
func WithChunkTablePolicy(policy ChunkPolicy, ignored []IgnoredFileLine) Option {
	return func(c *config) {
		cp := policy
		c.chunkPolicy = &cp
		if len(cp.Requests) > 0 {
			c.withoutTransaction = true
		}
		if len(ignored) > 0 {
			c.chunkIgnored = append([]IgnoredFileLine(nil), ignored...)
		}
	}
}

// WithWorkers sets parallel dump worker count. Values above one are rejected when
// chunk or slow-connection policy is active.
func WithWorkers(n int) Option {
	return func(c *config) {
		c.workers = n
	}
}

// WithPlanValidator registers a callback invoked with the canonical strategy plan
// after table selection and dispatch planning and before metadata or table output.
// Serial dumps run validation inside the read-only transaction; no-transaction
// resilient modes validate immediately after planning. A nil validator is a no-op.
func WithPlanValidator(v PlanValidator) Option {
	return func(c *config) {
		c.planValidator = v
	}
}

// InspectChunkPolicy returns the chunk policy captured from opts, or nil.
func InspectChunkPolicy(opts ...Option) *ChunkPolicy {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.chunkPolicy
}

// InspectWorkers returns the worker count captured from opts (0 means unset/default 1).
func InspectWorkers(opts ...Option) int {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.workers
}

// InspectTableSelection returns the selection policy captured from opts, or nil.
func InspectTableSelection(opts ...Option) *SelectionPolicy {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.selection
}

// InspectPlanValidator returns the plan validator captured from opts, or nil.
func InspectPlanValidator(opts ...Option) PlanValidator {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c.planValidator
}

func InspectProvenance(opts ...Option) *Provenance {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if c.provenance == nil {
		return nil
	}
	cp := *c.provenance
	cp.Schemas = append([]string(nil), c.provenance.Schemas...)
	return &cp
}

// WithoutSequences skips sequence state capture during Dump.
// Use in tests where pg_sequences cannot be queried (mock databases).
func WithoutSequences() Option {
	return func(c *config) {
		c.skipSequences = true
	}
}

func dumpSnapshotConsistent(cfg *config) bool {
	return !cfg.slowConnection && !hasChunkPolicy(cfg) && !cfg.withoutTransaction
}

func warnSnapshotInconsistent(cfg *config) {
	if dumpSnapshotConsistent(cfg) {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: dump is not snapshot-consistent; tables are read outside a shared snapshot\n")
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
	p.SnapshotConsistent = dumpSnapshotConsistent(cfg)
	if cfg.withoutTransaction {
		p.NoTransaction = true
	}
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
	if err := validateDumpOptions(&cfg); err != nil {
		return err
	}
	workers := effectiveWorkers(cfg.workers)
	if workers > 1 {
		if err := validateParallelDumpOptions(&cfg, dbConn, workers); err != nil {
			return err
		}
		plan, err := Prepare(ctx, dbConn, outputDir, opts...)
		if err != nil {
			return err
		}
		defer plan.Close()
		return plan.Run(ctx)
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
	if hasChunkPolicy(&cfg) && cfg.subset != nil {
		return fmt.Errorf("chunk-table selectors and subset dump are incompatible")
	}

	tables, err := db.LoadPostgresSchemas(ctx, q, cfg.schemas)
	if err != nil {
		return fmt.Errorf("load schema: %w", err)
	}
	preStripTables := tables
	if cfg.selection != nil {
		filtered, selProv, err := planPartitionTableSelection(tables, cfg.selection, cfg.selectionIgnored)
		if err != nil {
			return err
		}
		tables = filtered
		if cfg.provenance != nil {
			cfg.provenance.TableSelection = &selProv
		}
		for _, w := range selProv.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
	} else {
		if cfg.subset != nil {
			tables = db.WithoutPartitionParents(tables)
		} else {
			tables, cfg.provenance = recordAndStripPartitionParents(tables, cfg.provenance)
		}
	}

	chunkPlans, chunkProv, err := PlanChunkStreaming(tables, cfg.chunkPolicy, cfg.chunkIgnored)
	if err != nil {
		return err
	}
	if cfg.provenance != nil && len(chunkProv.Requested) > 0 {
		cfg.provenance.ChunkTables = &chunkProv
	}

	if err := guardSelectedTables(tables, cfg.schemas); err != nil {
		return err
	}

	if cfg.subset != nil {
		return dumpSubset(ctx, q, tx, tables, outputDir, &cfg)
	}

	sorted := SortTables(tables)
	dispatchPlans := buildDispatchPlans(&cfg, sorted, chunkPlans)
	records := BuildStrategyRecords(sorted, dispatchPlans)
	if cfg.planValidator != nil {
		if err := cfg.planValidator(PreparedDumpPlan{Strategies: copyStrategyRecords(records)}); err != nil {
			return err
		}
	}
	if cfg.provenance != nil && len(records) > 0 {
		cfg.provenance.Strategies = copyStrategyRecords(records)
	}
	if hasResumableDispatch(dispatchPlans) {
		if err := rejectAmbiguousLegacySlowArtifacts(outputDir, sorted); err != nil {
			return err
		}
	}
	assignDataFiles(sorted)

	var sequences []SequenceState
	if !cfg.skipSequences {
		seqTables := sorted
		if cfg.selection == nil && cfg.subset == nil {
			seqTables = tablesForSequenceCapture(preStripTables, sorted)
		}
		seqs, err := captureSequences(ctx, q, seqTables)
		if err != nil {
			return fmt.Errorf("capture sequences: %w", err)
		}
		if err := guardSequenceScope(seqs, cfg.schemas); err != nil {
			return fmt.Errorf("capture sequences: %w", err)
		}
		sequences = seqs
	}

	metaPath, err := writeMetadata(outputDir, sorted, nil, cfg.schemas, sequences, provenanceForWrite(&cfg, sorted))
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	warnSnapshotInconsistent(&cfg)

	for i, table := range sorted {
		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_start",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(sorted),
			Elapsed: time.Since(startedAt),
		})
		var n int64
		var streamErr error
		plan, hasPlan := dispatchPlans[tableKey(table.Schema, table.Name)]
		if hasPlan && plan.Resumable {
			if plan.Strategy == KeyStrategyCTID {
				if cfg.requireSafeKey {
					return fmt.Errorf("table %q has no safe key; --require-safe-key refuses ctid resume",
						qualifiedName(table.Schema, table.Name))
				}
				fmt.Fprintf(os.Stderr, "warning: table %q has no safe key; resuming with ctid (VACUUM or updates can skip or duplicate rows)\n",
					qualifiedName(table.Schema, table.Name))
			}
			n, streamErr = streamTableSlow(ctx, q, table, outputDir, cfg.rowTransform, cfg.slowRetry, cfg.slowChunkSize)
		} else {
			n, streamErr = streamTable(ctx, q, table, outputDir, cfg.rowTransform)
		}
		if streamErr != nil {
			return streamErr
		}
		sorted[i].RowCount = rowCountPtr(n)
		emitProgress(&cfg, ProgressEvent{
			Phase:   "table_end",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(sorted),
			Elapsed: time.Since(startedAt),
		})
	}

	metaPath, err = writeMetadata(outputDir, sorted, nil, cfg.schemas, sequences, provenanceForWrite(&cfg, sorted))
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
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

func hasChunkPolicy(cfg *config) bool {
	return cfg.chunkPolicy != nil && len(cfg.chunkPolicy.Requests) > 0
}

// buildDispatchPlans returns per-table streaming plans for serial dump dispatch.
// Slow-connection mode plans every selected table; chunk-only mode plans requested tables.
func buildDispatchPlans(cfg *config, tables []db.Table, chunkPlans map[string]KeyDescriptor) map[string]KeyDescriptor {
	if cfg.slowConnection {
		plans := make(map[string]KeyDescriptor, len(tables))
		for _, table := range tables {
			key := tableKey(table.Schema, table.Name)
			plans[key] = promoteNoKeyPlan(SelectKeyDescriptor(table))
		}
		return plans
	}
	if hasChunkPolicy(cfg) {
		return chunkPlans
	}
	return nil
}

func hasResumableDispatch(plans map[string]KeyDescriptor) bool {
	for _, plan := range plans {
		if plan.Resumable {
			return true
		}
	}
	return false
}

// executableChunkSet limits current keyset execution to the PK strategy that
// streamTableSlow supports. Later wiring can promote unique-key plans safely.
func executableChunkSet(plans map[string]KeyDescriptor) map[string]struct{} {
	chunkSet := make(map[string]struct{}, len(plans))
	for key, plan := range plans {
		if plan.Strategy == KeyStrategyPrimaryKey {
			chunkSet[key] = struct{}{}
		}
	}
	return chunkSet
}

func usesResilientStreaming(cfg *config, chunkSet map[string]struct{}, table db.Table) bool {
	if cfg.slowConnection {
		return true
	}
	_, ok := chunkSet[tableKey(table.Schema, table.Name)]
	return ok
}

func copyStrategyRecords(records []TableStrategyRecord) []TableStrategyRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]TableStrategyRecord, len(records))
	for i, rec := range records {
		out[i] = TableStrategyRecord{
			Table:       rec.Table,
			Strategy:    rec.Strategy,
			Resumable:   rec.Resumable,
			Fingerprint: rec.Fingerprint,
		}
		if len(rec.KeyColumns) > 0 {
			out[i].KeyColumns = append([]string(nil), rec.KeyColumns...)
		}
	}
	return out
}

func validateDumpOptions(cfg *config) error {
	workers := effectiveWorkers(cfg.workers)
	if workers > maxParallelWorkers {
		return fmt.Errorf("parallel dump workers must be between 1 and %d", maxParallelWorkers)
	}
	if workers > 1 && cfg.planValidator != nil {
		return fmt.Errorf("parallel dump workers are incompatible with dump plan validation")
	}
	if cfg.subset != nil && cfg.planValidator != nil {
		return fmt.Errorf("subset dump is incompatible with dump plan validation")
	}
	if workers > 1 && (cfg.slowConnection || hasChunkPolicy(cfg)) {
		return fmt.Errorf("parallel dump workers are incompatible with chunk or slow-connection mode")
	}
	if workers > 1 && cfg.withoutTransaction {
		return fmt.Errorf("parallel dump workers require a read-only transaction snapshot")
	}
	if workers > 1 && cfg.subset != nil {
		return fmt.Errorf("parallel dump workers are incompatible with subset dump")
	}
	return nil
}

// maxSequenceTablesPerQuery keeps two bind parameters per table under
// PostgreSQL's 65535-parameter limit.
const maxSequenceTablesPerQuery = 16000

// sequenceLookupBatch is the table count per captureSequences query.
// Tests shrink it to prove lookups are split.
var sequenceLookupBatch = maxSequenceTablesPerQuery

// captureSequences reads sequence last values for sequences owned by columns
// on the given tables (pg_depend deptype a/i). Standalone sequences are omitted.
func captureSequences(ctx context.Context, q querier, tables []db.Table) ([]SequenceState, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	batchSize := sequenceLookupBatch
	if batchSize < 1 {
		batchSize = maxSequenceTablesPerQuery
	}
	var seqs []SequenceState
	for start := 0; start < len(tables); start += batchSize {
		end := start + batchSize
		if end > len(tables) {
			end = len(tables)
		}
		batch, err := captureSequenceBatch(ctx, q, tables[start:end])
		if err != nil {
			return nil, err
		}
		seqs = append(seqs, batch...)
	}
	sort.Slice(seqs, func(i, j int) bool {
		if seqs[i].Schema != seqs[j].Schema {
			return seqs[i].Schema < seqs[j].Schema
		}
		return seqs[i].Name < seqs[j].Name
	})
	return seqs, nil
}

func captureSequenceBatch(ctx context.Context, q querier, tables []db.Table) ([]SequenceState, error) {
	predicates, args := tableTuplePredicates(tables)
	query := fmt.Sprintf(`
		SELECT seq_ns.nspname, seq.relname, ps.last_value, ps.start_value,
		       ps.increment_by, ps.min_value, ps.max_value, ps.cache_size, ps.cycle, ps.data_type
		FROM pg_class seq
		JOIN pg_namespace seq_ns ON seq_ns.oid = seq.relnamespace
		JOIN pg_sequences ps ON ps.schemaname = seq_ns.nspname AND ps.sequencename = seq.relname
		JOIN pg_depend dep ON dep.objid = seq.oid AND dep.deptype IN ('a', 'i')
		JOIN pg_class tbl ON tbl.oid = dep.refobjid
		JOIN pg_namespace tbl_ns ON tbl_ns.oid = tbl.relnamespace
		JOIN pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = dep.refobjsubid AND NOT a.attisdropped
		WHERE seq.relkind = 'S'
		  AND (tbl_ns.nspname, tbl.relname) IN (%s)
		ORDER BY seq_ns.nspname, seq.relname`, predicates)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sequences: %w", err)
	}
	defer rows.Close()

	var seqs []SequenceState
	for rows.Next() {
		var schema, seqName, dataType string
		var lastValue sql.NullInt64
		var startValue, increment, minValue, maxValue, cache int64
		var cycle bool
		if err := rows.Scan(
			&schema, &seqName, &lastValue, &startValue,
			&increment, &minValue, &maxValue, &cache, &cycle, &dataType,
		); err != nil {
			return nil, fmt.Errorf("scan sequence: %w", err)
		}
		s := SequenceState{
			Schema:      schema,
			Name:        seqName,
			StartValue:  startValue,
			IsCalled:    lastValue.Valid,
			IncrementBy: &increment,
			MinValue:    &minValue,
			MaxValue:    &maxValue,
			CacheSize:   &cache,
			Cycle:       &cycle,
			DataType:    dataType,
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

func tableTuplePredicates(tables []db.Table) (string, []any) {
	predicates := make([]string, len(tables))
	args := make([]any, 0, len(tables)*2)
	for i, tbl := range tables {
		base := i*2 + 1
		predicates[i] = fmt.Sprintf("($%d,$%d)", base, base+1)
		args = append(args, tbl.Schema, tbl.Name)
	}
	return strings.Join(predicates, ", "), args
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
		byName[tableKey(t.Schema, t.Name)] = t
	}

	var included []db.Table
	for _, name := range plan.tableOrder {
		included = append(included, byName[name])
	}
	if err := guardSelectedTables(included, cfg.schemas); err != nil {
		return err
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
	warnSnapshotInconsistent(cfg)

	for i, table := range included {
		emitProgress(cfg, ProgressEvent{
			Phase:   "table_start",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(included),
			Elapsed: time.Since(startedAt),
		})
		key := tableKey(table.Schema, table.Name)
		tp := plan.tables[key]
		pkCol, pkErr := primaryKeyColumn(table)
		if pkErr != nil && len(tp.compositeFKVals) == 0 {
			return fmt.Errorf("table %q: %w", table.Name, pkErr)
		}

		var clauses []compiledWhere
		if pkErr == nil {
			pkCols[key] = pkCol
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
		n, err := streamTableFiltered(ctx, q, table, outputDir, clauses, cfg.rowTransform, orderCol)
		if err != nil {
			return err
		}
		included[i].RowCount = rowCountPtr(n)
		emitProgress(cfg, ProgressEvent{
			Phase:   "table_end",
			Table:   table.Name,
			Current: i + 1,
			Total:   len(included),
			Elapsed: time.Since(startedAt),
		})
	}

	metaPath, err = writeMetadata(outputDir, included, manifest, cfg.schemas, nil, provenanceForWrite(cfg, included))
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
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
