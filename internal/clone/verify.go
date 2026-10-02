package clone

import (
	"context"
	"database/sql"
	"fmt"

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

func verifyGapWarnings(c SchemaReplayGapCounts) []string {
	var out []string
	if c.HypotheticalAggregates > 0 {
		out = append(out, fmt.Sprintf(
			"verify: schema-replay did not copy %d hypothetical aggregate(s); pg_dump is required for those objects",
			c.HypotheticalAggregates,
		))
	}
	if c.UserOperatorClasses > 0 {
		out = append(out, fmt.Sprintf(
			"verify: schema-replay did not copy %d user operator class(es); pg_dump is required for those objects",
			c.UserOperatorClasses,
		))
	}
	return out
}

// SchemaReplayVerify compares row counts and sequence state between source and target
// after a successful schema-replay data restore. Mismatches are returned as warnings only.
func SchemaReplayVerify(ctx context.Context, opts Options, srcDB, tgtDB *sql.DB, usedPgDump bool) ([]string, error) {
	schemas := SchemasFromOptions(opts)
	if len(schemas) == 0 {
		names, err := listSchemaNamesFunc(ctx, srcDB)
		if err != nil {
			return nil, fmt.Errorf("verify list schemas: %w", err)
		}
		schemas = names
	}
	scope := canonicalizeEffectiveScope(schemas)

	var warnings []string
	if !usedPgDump {
		counts, err := scanSchemaReplayGapCounts(ctx, srcDB, scope)
		if err != nil {
			return nil, fmt.Errorf("verify schema gaps: %w", err)
		}
		warnings = append(warnings, verifyGapWarnings(counts)...)
	}

	tables, err := db.LoadPostgresSchemas(ctx, srcDB, schemas)
	if err != nil {
		return nil, fmt.Errorf("verify load tables: %w", err)
	}
	tables = db.WithoutPartitionParents(tables)

	for _, table := range tables {
		srcCount, err := countTableRows(ctx, srcDB, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("verify source %s.%s: %w", table.Schema, table.Name, err)
		}
		tgtCount, err := countTableRows(ctx, tgtDB, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("verify target %s.%s: %w", table.Schema, table.Name, err)
		}
		if srcCount != tgtCount {
			warnings = append(warnings, formatRowCountMismatch(table.Schema, table.Name, srcCount, tgtCount))
		}
	}

	seqWarnings, err := verifySequences(ctx, srcDB, tgtDB, scope)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, seqWarnings...)

	return warnings, nil
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
	schema, name string
	lastValue    int64
	isCalled     bool
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
			warnings = append(warnings, formatSequenceLastValueMismatch(src.schema, src.name, src.lastValue, 0))
			if src.isCalled {
				warnings = append(warnings, formatSequenceIsCalledMismatch(src.schema, src.name, src.isCalled, false))
			}
			continue
		}
		if src.lastValue != tgt.lastValue {
			warnings = append(warnings, formatSequenceLastValueMismatch(src.schema, src.name, src.lastValue, tgt.lastValue))
		}
		if src.isCalled != tgt.isCalled {
			warnings = append(warnings, formatSequenceIsCalledMismatch(src.schema, src.name, src.isCalled, tgt.isCalled))
		}
	}
	return warnings, nil
}

func listSequencesInScope(ctx context.Context, dbConn *sql.DB, scope []string) ([]sequenceState, error) {
	scopePred, scopeArgs := scopedNamespacePredicate("ps.schemaname", scope)
	query := `
		SELECT ps.schemaname, ps.sequencename, ps.last_value, ps.is_called
		FROM pg_sequences ps
		WHERE ps.schemaname NOT IN ('pg_catalog', 'information_schema')
		  AND ps.schemaname NOT LIKE 'pg_temp_%'
		  AND ps.schemaname NOT LIKE 'pg_toast_%'
		  AND ps.sequencename NOT LIKE 'pg_toast_%'` + scopePred + `
		ORDER BY ps.schemaname, ps.sequencename`
	rows, err := queryContextOptional(ctx, dbConn, query, scopeArgs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []sequenceState
	for rows.Next() {
		var seq sequenceState
		if err := rows.Scan(&seq.schema, &seq.name, &seq.lastValue, &seq.isCalled); err != nil {
			return nil, err
		}
		out = append(out, seq)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
