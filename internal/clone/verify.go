package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// runSchemaReplayVerify is overridable for tests.
var runSchemaReplayVerify = SchemaReplayVerify

func formatRowCountMismatch(schema, table string, sourceCount, targetCount int64) string {
	return fmt.Sprintf("verify: %s.%s row count source=%d target=%d", schema, table, sourceCount, targetCount)
}

func formatSequenceLastValueMismatch(schema, seq string, sourceVal, targetVal int64) string {
	return fmt.Sprintf("verify: %s.%s last_value source=%d target=%d", schema, seq, sourceVal, targetVal)
}

func formatSequenceIsCalledMismatch(schema, seq string, sourceCalled, targetCalled bool) string {
	return fmt.Sprintf("verify: %s.%s is_called source=%t target=%t", schema, seq, sourceCalled, targetCalled)
}

// FormatVerifiedTables reports a schema-replay check whose compared tables matched.
func FormatVerifiedTables(n int) string {
	if n == 1 {
		return "verified 1 table"
	}
	return fmt.Sprintf("verified %d tables", n)
}

func verifyGapWarnings(c SchemaReplayGapCounts) []string {
	var out []string
	if c.HypotheticalAggregates > 0 {
		out = append(out, fmt.Sprintf(
			"verify: schema-replay did not copy %d hypothetical aggregate(s); pg_dump is required for those objects",
			c.HypotheticalAggregates,
		))
	}
	return out
}

// SchemaReplayVerify compares row counts and sequence state between source and target
// after a successful schema-replay data restore. Mismatches are returned as warnings only.
func SchemaReplayVerify(ctx context.Context, opts Options, srcDB, tgtDB *sql.DB, usedPgDump bool) ([]string, int, error) {
	schemas := SchemasFromOptions(opts)
	if len(schemas) == 0 {
		names, err := listSchemaNamesFunc(ctx, srcDB)
		if err != nil {
			return nil, 0, fmt.Errorf("verify list schemas: %w", err)
		}
		schemas = names
	}
	scope := canonicalizeEffectiveScope(schemas)

	var warnings []string
	if !usedPgDump {
		counts, err := scanSchemaReplayGapCounts(ctx, srcDB, scope)
		if err != nil {
			return nil, 0, fmt.Errorf("verify schema gaps: %w", err)
		}
		warnings = append(warnings, verifyGapWarnings(counts)...)
	}

	tables, err := db.LoadPostgresSchemas(ctx, srcDB, schemas)
	if err != nil {
		return nil, 0, fmt.Errorf("verify load tables: %w", err)
	}
	tables = db.WithoutPartitionParents(tables)

	for _, table := range tables {
		srcCount, err := countTableRows(ctx, srcDB, table.Schema, table.Name)
		if err != nil {
			return nil, 0, fmt.Errorf("verify source %s.%s: %w", table.Schema, table.Name, err)
		}
		tgtCount, err := countTableRows(ctx, tgtDB, table.Schema, table.Name)
		if err != nil {
			return nil, 0, fmt.Errorf("verify target %s.%s: %w", table.Schema, table.Name, err)
		}
		if srcCount != tgtCount {
			warnings = append(warnings, formatRowCountMismatch(table.Schema, table.Name, srcCount, tgtCount))
		}
	}

	seqWarnings, err := verifySequences(ctx, srcDB, tgtDB, scope)
	if err != nil {
		return nil, 0, err
	}
	warnings = append(warnings, seqWarnings...)

	return warnings, len(tables), nil
}

func countTableRows(ctx context.Context, dbConn *sql.DB, schema, table string) (int64, error) {
	query := fmt.Sprintf(`SELECT COUNT(*)::bigint FROM %s`, quoteQualifiedTable(schema, table))
	var n int64
	if err := dbConn.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

type sequenceState struct {
	schema, name   string
	lastValue      int64
	lastValueKnown bool
	isCalled       bool
	isCalledKnown  bool
}

func formatSequenceStateUnavailable(schema, seq, field string) string {
	return fmt.Sprintf("verify: %s.%s %s unavailable without sequence SELECT", schema, seq, field)
}

func verifySequences(ctx context.Context, srcDB, tgtDB *sql.DB, scope []string) ([]string, error) {
	srcSeqs, err := listSequencesInScope(ctx, srcDB, scope)
	if err != nil {
		return nil, fmt.Errorf("verify list source sequences: %w", err)
	}
	tgtSeqs, err := listSequencesInScope(ctx, tgtDB, scope)
	if err != nil {
		return nil, fmt.Errorf("verify list target sequences: %w", err)
	}

	tgtByKey := make(map[string]sequenceState, len(tgtSeqs))
	for _, seq := range tgtSeqs {
		tgtByKey[seq.schema+"\x00"+seq.name] = seq
	}

	var warnings []string
	for _, src := range srcSeqs {
		key := src.schema + "\x00" + src.name
		tgt, ok := tgtByKey[key]
		if !ok {
			if src.lastValueKnown {
				warnings = append(warnings, formatSequenceLastValueMismatch(src.schema, src.name, src.lastValue, 0))
			} else {
				warnings = append(warnings, formatSequenceStateUnavailable(src.schema, src.name, "last_value"))
			}
			if src.isCalledKnown && src.isCalled {
				warnings = append(warnings, formatSequenceIsCalledMismatch(src.schema, src.name, src.isCalled, false))
			}
			continue
		}
		switch {
		case src.lastValueKnown && tgt.lastValueKnown && src.lastValue != tgt.lastValue:
			warnings = append(warnings, formatSequenceLastValueMismatch(src.schema, src.name, src.lastValue, tgt.lastValue))
		case !src.lastValueKnown || !tgt.lastValueKnown:
			warnings = append(warnings, formatSequenceStateUnavailable(src.schema, src.name, "last_value"))
		}
		switch {
		case src.isCalledKnown && tgt.isCalledKnown && src.isCalled != tgt.isCalled:
			warnings = append(warnings, formatSequenceIsCalledMismatch(src.schema, src.name, src.isCalled, tgt.isCalled))
		case !src.isCalledKnown || !tgt.isCalledKnown:
			warnings = append(warnings, formatSequenceStateUnavailable(src.schema, src.name, "is_called"))
		}
	}
	return warnings, nil
}

func listSequencesInScope(ctx context.Context, dbConn *sql.DB, scope []string) ([]sequenceState, error) {
	names, err := listSequenceNames(ctx, dbConn, scope)
	if err != nil {
		return nil, err
	}
	out := make([]sequenceState, 0, len(names))
	for _, name := range names {
		seq, err := readSequenceState(ctx, dbConn, name.schema, name.name)
		if err != nil {
			if !isInsufficientPrivilege(err) {
				return nil, err
			}
			seq = sequenceState{schema: name.schema, name: name.name}
			last, viewErr := readSequenceViewLastValue(ctx, dbConn, name.schema, name.name)
			if viewErr != nil && !isInsufficientPrivilege(viewErr) && !errors.Is(viewErr, sql.ErrNoRows) {
				return nil, viewErr
			}
			if viewErr == nil && last.Valid {
				seq.lastValue = last.Int64
				seq.lastValueKnown = true
			}
		}
		out = append(out, seq)
	}
	return out, nil
}

// listSequenceNames lists in-scope sequences from pg_class. pg_sequences has no
// is_called column, and its last_value is NULL until the sequence is used.
func listSequenceNames(ctx context.Context, dbConn *sql.DB, scope []string) ([]sequenceState, error) {
	scopePred, scopeArgs := scopedNamespacePredicate("n.nspname", scope)
	query := `
		SELECT n.nspname, c.relname
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'S'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_%'
		  AND c.relname NOT LIKE 'pg_toast_%'` + scopePred + `
		ORDER BY n.nspname, c.relname`
	rows, err := queryContextOptional(ctx, dbConn, query, scopeArgs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []sequenceState
	for rows.Next() {
		var seq sequenceState
		if err := rows.Scan(&seq.schema, &seq.name); err != nil {
			return nil, err
		}
		out = append(out, seq)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func readSequenceState(ctx context.Context, dbConn *sql.DB, schema, name string) (sequenceState, error) {
	query := fmt.Sprintf(`SELECT last_value, is_called FROM %s`, quoteQualifiedTable(schema, name))
	seq := sequenceState{schema: schema, name: name, lastValueKnown: true, isCalledKnown: true}
	if err := dbConn.QueryRowContext(ctx, query).Scan(&seq.lastValue, &seq.isCalled); err != nil {
		return sequenceState{}, err
	}
	return seq, nil
}

func readSequenceViewLastValue(ctx context.Context, dbConn *sql.DB, schema, name string) (sql.NullInt64, error) {
	var last sql.NullInt64
	err := dbConn.QueryRowContext(ctx, `
		SELECT last_value
		FROM pg_sequences
		WHERE schemaname = $1 AND sequencename = $2`, schema, name).Scan(&last)
	if err != nil {
		return sql.NullInt64{}, err
	}
	return last, nil
}

func isInsufficientPrivilege(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}
