package restore

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

// quoteIdentifier returns a PostgreSQL double-quoted identifier,
// escaping embedded double quotes by doubling them.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteLiteral returns a PostgreSQL single-quoted string literal,
// escaping embedded single quotes by doubling them.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, `'`, `''`) + "'"
}

// quoteQualifiedTable returns a fully-quoted "schema"."name" string.
func quoteQualifiedTable(schema, name string) string {
	return quoteIdentifier(schema) + "." + quoteIdentifier(name)
}

func schemaSet(schemas []string) map[string]bool {
	if len(schemas) == 0 {
		return nil
	}
	set := make(map[string]bool, len(schemas))
	for _, s := range schemas {
		set[s] = true
	}
	return set
}

// RestoreSequencesFromMetadata reads sequence state from dump metadata and
// applies setval on the target database. This prevents serial/identity
// collisions after a standalone restore. When schemas is non-empty, only
// sequences in those schemas are restored. allDumpTables is the full table
// list from metadata before restore exclusions; pass nil to use meta.Tables.
func RestoreSequencesFromMetadata(ctx context.Context, q execQuerier, meta dump.Metadata, schemas []string, excludedTables map[string]bool, allDumpTables []db.Table) error {
	if len(meta.Sequences) == 0 {
		return nil
	}

	dumpTables := allDumpTables
	if len(dumpTables) == 0 {
		dumpTables = meta.Tables
	}

	schemaFilter := schemaSet(schemas)
	restoredColumns := make(map[string]bool)
	restoredTables := make(map[string]bool, len(meta.Tables))
	for _, table := range meta.Tables {
		restoredTables[table.Schema+"\x00"+table.Name] = true
		for _, column := range table.Columns {
			restoredColumns[table.Schema+"\x00"+table.Name+"\x00"+column.Name] = true
		}
	}
	omittedParents := omittedPartitionParentSet(meta)
	for _, seq := range meta.Sequences {
		if schemaFilter != nil && !schemaFilter[seq.Schema] {
			continue
		}
		if err := validateSequenceDataType(seq.DataType); err != nil {
			return fmt.Errorf("sequence %s.%s: %w", seq.Schema, seq.Name, err)
		}
		if ownerKey, ok := sequenceOwnerTableKeyFromMetadata(seq, dumpTables); ok && excludedTables != nil && excludedTables[ownerKey] {
			continue
		}
		owned, err := validateSequenceOwnership(ctx, q, seq, restoredColumns, restoredTables, excludedTables, omittedParents)
		if err != nil {
			return err
		}
		if !owned {
			continue
		}

		value := seq.StartValue
		isCalled := false
		if seq.LastValue != nil {
			value = *seq.LastValue
			isCalled = seq.IsCalled
		}

		// Read the destination value before changing the definition. An
		// advanced sequence must stay usable when the dump bounds exclude it.
		curLast, _, err := readSequenceValue(ctx, q, seq)
		if err != nil {
			return err
		}
		if sequenceHasDefinitionOptions(seq) {
			if err := applySequenceOptions(ctx, q, reconcileSequenceBounds(seq, curLast)); err != nil {
				return err
			}
		}

		if curLast.Valid && curLast.Int64 >= value {
			fmt.Fprintf(os.Stderr,
				"skipping sequence %s.%s: current value %d >= dump value %d, not lowering\n",
				seq.Schema, seq.Name, curLast.Int64, value,
			)
			continue
		}

		setSQL := fmt.Sprintf(
			"SELECT setval(%s::regclass, %d, %t)",
			quoteLiteral(quoteQualifiedTable(seq.Schema, seq.Name)),
			value,
			isCalled,
		)
		if _, err := q.ExecContext(ctx, setSQL); err != nil {
			return fmt.Errorf("setval %s.%s: %w", seq.Schema, seq.Name, err)
		}
	}
	return nil
}

func omittedPartitionParentSet(meta dump.Metadata) map[string]bool {
	if meta.Provenance == nil || len(meta.Provenance.OmittedPartitionParents) == 0 {
		return nil
	}
	set := make(map[string]bool, len(meta.Provenance.OmittedPartitionParents))
	for _, name := range meta.Provenance.OmittedPartitionParents {
		set[name] = true
	}
	return set
}

// sequenceOwnerTableKeyFromMetadata maps a dump sequence to its owning table
// using PostgreSQL's default serial/identity sequence naming.
func sequenceOwnerTableKeyFromMetadata(seq dump.SequenceState, tables []db.Table) (string, bool) {
	for _, tbl := range tables {
		if tbl.Schema != seq.Schema {
			continue
		}
		for _, col := range tbl.Columns {
			if seq.Name == tbl.Name+"_"+col.Name+"_seq" {
				return tableKey(tbl.Schema, tbl.Name), true
			}
		}
	}
	return "", false
}

func validateSequenceOwnership(ctx context.Context, q execQuerier, seq dump.SequenceState, restoredColumns, restoredTables, excludedTables, omittedParents map[string]bool) (bool, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT tbl_ns.nspname, tbl.relname, a.attname
		FROM pg_class seq
		JOIN pg_namespace seq_ns ON seq_ns.oid = seq.relnamespace
		JOIN pg_depend dep ON dep.objid = seq.oid AND dep.deptype IN ('a', 'i')
		JOIN pg_class tbl ON tbl.oid = dep.refobjid
		JOIN pg_namespace tbl_ns ON tbl_ns.oid = tbl.relnamespace
		JOIN pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = dep.refobjsubid AND NOT a.attisdropped
		WHERE seq.relkind = 'S'
		  AND seq_ns.nspname = %s AND seq.relname = %s`,
		quoteLiteral(seq.Schema), quoteLiteral(seq.Name)))
	if err != nil {
		return false, fmt.Errorf("validate sequence ownership %s.%s: %w", seq.Schema, seq.Name, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("validate sequence ownership %s.%s: %w", seq.Schema, seq.Name, err)
		}
		return false, nil
	}
	var schema, table, column string
	if err := rows.Scan(&schema, &table, &column); err != nil {
		return false, fmt.Errorf("scan sequence ownership %s.%s: %w", seq.Schema, seq.Name, err)
	}
	colKey := schema + "\x00" + table + "\x00" + column
	tableKey := schema + "\x00" + table
	if restoredColumns[colKey] || omittedParents[schema+"."+table] {
		return true, nil
	}
	if excludedTables != nil && excludedTables[tableKey] {
		return false, nil
	}
	if restoredTables[tableKey] {
		return false, fmt.Errorf("sequence %s.%s is not owned by a restored column", seq.Schema, seq.Name)
	}
	return false, fmt.Errorf("sequence %s.%s is not owned by a restored column", seq.Schema, seq.Name)
}

// sequenceSyncBatch is the table count per SyncSequencesToData lookup.
// Two placeholders per table stay under PostgreSQL's 65535-parameter limit.
// Tests shrink it to prove lookups are split.
var sequenceSyncBatch = 16000

type serialCol struct {
	schema, table, column string
}

// SyncSequencesToData advances serial/identity sequences for restored tables
// to the max value of their owning columns.
func SyncSequencesToData(ctx context.Context, q execQuerier, tables []db.Table) error {
	if len(tables) == 0 {
		return nil
	}
	batchSize := sequenceSyncBatch
	if batchSize < 1 {
		batchSize = 16000
	}
	restoredColumns := make(map[string]bool)
	var cols []serialCol
	for start := 0; start < len(tables); start += batchSize {
		end := start + batchSize
		if end > len(tables) {
			end = len(tables)
		}
		batch := tables[start:end]
		for _, tbl := range batch {
			for _, col := range tbl.Columns {
				restoredColumns[tbl.Schema+"\x00"+tbl.Name+"\x00"+col.Name] = true
			}
		}
		found, err := listSerialColumns(ctx, q, batch)
		if err != nil {
			return err
		}
		cols = append(cols, found...)
	}

	for _, c := range cols {
		if !restoredColumns[c.schema+"\x00"+c.table+"\x00"+c.column] {
			continue
		}
		qualifiedTable := quoteQualifiedTable(c.schema, c.table)
		setvalSQL := fmt.Sprintf(
			`SELECT CASE WHEN m.max_value IS NULL THEN NULL ELSE setval(pg_get_serial_sequence(%s, %s), m.max_value, true) END FROM (SELECT max(%s) AS max_value FROM %s) AS m`,
			quoteLiteral(qualifiedTable),
			quoteLiteral(c.column),
			quoteIdentifier(c.column),
			qualifiedTable,
		)
		if _, err := q.ExecContext(ctx, setvalSQL); err != nil {
			return fmt.Errorf("sync sequence for %s.%s: %w", c.schema, c.table, err)
		}
	}
	return nil
}

func listSerialColumns(ctx context.Context, q execQuerier, tables []db.Table) ([]serialCol, error) {
	predicates := make([]string, len(tables))
	args := make([]any, 0, len(tables)*2)
	for i, tbl := range tables {
		base := i*2 + 1
		predicates[i] = fmt.Sprintf("($%d,$%d)", base, base+1)
		args = append(args, tbl.Schema, tbl.Name)
	}
	query := fmt.Sprintf(`
		SELECT table_schema, table_name, column_name
		FROM information_schema.columns
		WHERE (table_schema, table_name) IN (%s)
		  AND (
		    column_default LIKE 'nextval%%'
		    OR identity_generation IS NOT NULL
		  )
		ORDER BY table_schema, table_name, ordinal_position`, strings.Join(predicates, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list serial columns: %w", err)
	}
	defer rows.Close()

	var cols []serialCol
	for rows.Next() {
		var c serialCol
		if err := rows.Scan(&c.schema, &c.table, &c.column); err != nil {
			return nil, fmt.Errorf("scan serial column: %w", err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list serial columns: %w", err)
	}
	return cols, nil
}

func readSequenceValue(ctx context.Context, q execQuerier, seq dump.SequenceState) (sql.NullInt64, bool, error) {
	checkSQL := fmt.Sprintf(
		"SELECT last_value, is_called FROM %s",
		quoteQualifiedTable(seq.Schema, seq.Name),
	)
	checkRows, err := q.QueryContext(ctx, checkSQL)
	if err != nil {
		return sql.NullInt64{}, false, fmt.Errorf("read current value for %s.%s: %w", seq.Schema, seq.Name, err)
	}
	var curLast sql.NullInt64
	var curCalled bool
	if checkRows.Next() {
		if err := checkRows.Scan(&curLast, &curCalled); err != nil {
			checkRows.Close()
			return sql.NullInt64{}, false, fmt.Errorf("scan current value for %s.%s: %w", seq.Schema, seq.Name, err)
		}
	}
	if err := checkRows.Close(); err != nil {
		return sql.NullInt64{}, false, fmt.Errorf("close current value rows for %s.%s: %w", seq.Schema, seq.Name, err)
	}
	return curLast, curCalled, nil
}

func sequenceHasDefinitionOptions(seq dump.SequenceState) bool {
	return seq.IncrementBy != nil || seq.MinValue != nil || seq.MaxValue != nil || seq.CacheSize != nil || seq.Cycle != nil || strings.TrimSpace(seq.DataType) != ""
}

func validateSequenceDataType(raw string) error {
	_, err := normalizeSequenceDataType(raw)
	return err
}

func normalizeSequenceDataType(raw string) (string, error) {
	dt := strings.ToLower(strings.TrimSpace(raw))
	switch dt {
	case "", "smallint", "integer", "bigint":
		return dt, nil
	default:
		return "", fmt.Errorf("data_type %q must be smallint, integer, or bigint", raw)
	}
}

func sequenceTypeFits(dataType string, value int64) bool {
	switch dataType {
	case "smallint":
		return value >= math.MinInt16 && value <= math.MaxInt16
	case "integer":
		return value >= math.MinInt32 && value <= math.MaxInt32
	default:
		return true
	}
}

// reconcileSequenceBounds drops clauses that would make an already-advanced
// destination unusable: a min/max that excludes the current value, CYCLE that
// would wrap at those bounds, and a narrower type that cannot store it.
func reconcileSequenceBounds(seq dump.SequenceState, curLast sql.NullInt64) dump.SequenceState {
	if !curLast.Valid {
		return seq
	}
	value := curLast.Int64
	outOfRange := (seq.MinValue != nil && value < *seq.MinValue) || (seq.MaxValue != nil && value > *seq.MaxValue)
	if outOfRange {
		seq.MinValue = nil
		seq.MaxValue = nil
		if seq.Cycle != nil && *seq.Cycle {
			seq.Cycle = nil
		}
	}
	if dt, err := normalizeSequenceDataType(seq.DataType); err == nil && dt != "" && !sequenceTypeFits(dt, value) {
		seq.DataType = ""
	}
	return seq
}

type sequenceDefinition struct {
	dataType  string
	increment int64
	minValue  int64
	maxValue  int64
	cacheSize int64
	cycle     bool
	found     bool
}

func readTargetSequenceDefinition(ctx context.Context, q execQuerier, seq dump.SequenceState) (sequenceDefinition, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT data_type, increment_by, min_value, max_value, cache_size, cycle
		FROM pg_catalog.pg_sequences
		WHERE schemaname = $1 AND sequencename = $2`, seq.Schema, seq.Name)
	if err != nil {
		return sequenceDefinition{}, fmt.Errorf("read definition for %s.%s: %w", seq.Schema, seq.Name, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return sequenceDefinition{}, fmt.Errorf("read definition for %s.%s: %w", seq.Schema, seq.Name, err)
		}
		return sequenceDefinition{}, nil
	}
	var def sequenceDefinition
	if err := rows.Scan(&def.dataType, &def.increment, &def.minValue, &def.maxValue, &def.cacheSize, &def.cycle); err != nil {
		return sequenceDefinition{}, fmt.Errorf("scan definition for %s.%s: %w", seq.Schema, seq.Name, err)
	}
	def.found = true
	if err := rows.Err(); err != nil {
		return sequenceDefinition{}, fmt.Errorf("read definition for %s.%s: %w", seq.Schema, seq.Name, err)
	}
	return def, nil
}

func sequenceOptionsMatch(target sequenceDefinition, seq dump.SequenceState) bool {
	if !target.found {
		return false
	}
	if dt, err := normalizeSequenceDataType(seq.DataType); err == nil && dt != "" && !strings.EqualFold(target.dataType, dt) {
		return false
	}
	if seq.IncrementBy != nil && *seq.IncrementBy != target.increment {
		return false
	}
	if seq.MinValue != nil && *seq.MinValue != target.minValue {
		return false
	}
	if seq.MaxValue != nil && *seq.MaxValue != target.maxValue {
		return false
	}
	if seq.CacheSize != nil && *seq.CacheSize != target.cacheSize {
		return false
	}
	if seq.Cycle != nil && *seq.Cycle != target.cycle {
		return false
	}
	return true
}

func applySequenceOptions(ctx context.Context, q execQuerier, seq dump.SequenceState) error {
	stmt, ok, err := formatAlterSequenceOptions(seq)
	if err != nil {
		return fmt.Errorf("sequence %s.%s: %w", seq.Schema, seq.Name, err)
	}
	if !ok {
		return nil
	}
	current, err := readTargetSequenceDefinition(ctx, q, seq)
	if err != nil {
		return err
	}
	if sequenceOptionsMatch(current, seq) {
		return nil
	}
	if _, err := q.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("alter sequence %s.%s: %w", seq.Schema, seq.Name, err)
	}
	return nil
}

func formatAlterSequenceOptions(seq dump.SequenceState) (string, bool, error) {
	dt, err := normalizeSequenceDataType(seq.DataType)
	if err != nil {
		return "", false, err
	}
	if seq.IncrementBy == nil && seq.MinValue == nil && seq.MaxValue == nil && seq.CacheSize == nil && seq.Cycle == nil && dt == "" {
		return "", false, nil
	}
	var parts []string
	if dt != "" {
		parts = append(parts, "AS "+dt)
	}
	if seq.IncrementBy != nil {
		parts = append(parts, fmt.Sprintf("INCREMENT BY %d", *seq.IncrementBy))
	}
	if seq.MinValue != nil {
		parts = append(parts, fmt.Sprintf("MINVALUE %d", *seq.MinValue))
	}
	if seq.MaxValue != nil {
		parts = append(parts, fmt.Sprintf("MAXVALUE %d", *seq.MaxValue))
	}
	if seq.CacheSize != nil {
		parts = append(parts, fmt.Sprintf("CACHE %d", *seq.CacheSize))
	}
	if seq.Cycle != nil {
		if *seq.Cycle {
			parts = append(parts, "CYCLE")
		} else {
			parts = append(parts, "NO CYCLE")
		}
	}
	if len(parts) == 0 {
		return "", false, nil
	}
	return "ALTER SEQUENCE " + quoteQualifiedTable(seq.Schema, seq.Name) + " " + strings.Join(parts, " "), true, nil
}
