package restore

import (
	"context"
	"fmt"
	"strings"

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
// sequences in those schemas are restored.
func RestoreSequencesFromMetadata(ctx context.Context, q execQuerier, meta dump.Metadata, schemas []string) error {
	if len(meta.Sequences) == 0 {
		return nil
	}

	schemaFilter := schemaSet(schemas)
	restoredColumns := make(map[string]bool)
	for _, table := range meta.Tables {
		for _, column := range table.Columns {
			restoredColumns[table.Schema+"\x00"+table.Name+"\x00"+column.Name] = true
		}
	}
	for _, seq := range meta.Sequences {
		if schemaFilter != nil && !schemaFilter[seq.Schema] {
			continue
		}
		owned, err := validateSequenceOwnership(ctx, q, seq, restoredColumns)
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

func validateSequenceOwnership(ctx context.Context, q execQuerier, seq dump.SequenceState, restoredColumns map[string]bool) (bool, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT tbl_ns.nspname, tbl.relname, a.attname
		FROM pg_class seq
		JOIN pg_depend dep ON dep.objid = seq.oid AND dep.deptype IN ('a', 'i')
		JOIN pg_class tbl ON tbl.oid = dep.refobjid
		JOIN pg_namespace tbl_ns ON tbl_ns.oid = tbl.relnamespace
		JOIN pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = dep.refobjsubid AND NOT a.attisdropped
		WHERE seq.oid = %s::regclass`, quoteLiteral(quoteQualifiedTable(seq.Schema, seq.Name))))
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
	if !restoredColumns[schema+"\x00"+table+"\x00"+column] {
		return false, fmt.Errorf("sequence %s.%s is not owned by a restored column", seq.Schema, seq.Name)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("validate sequence ownership %s.%s: %w", seq.Schema, seq.Name, err)
	}
	return true, nil
}

// SyncSequencesToData advances every serial/identity sequence on the target
// database to the max value of its owning column. This is necessary when data
// was loaded with explicit IDs (bypassing nextval), because pg_sequences
// tracks nextval calls, not the actual max ID in the table.
func SyncSequencesToData(ctx context.Context, q execQuerier, schemas []string) error {
	if len(schemas) == 0 {
		return nil
	}
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, s := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s
	}
	query := fmt.Sprintf(`
		SELECT table_schema, table_name, column_name
		FROM information_schema.columns
		WHERE table_schema IN (%s)
		  AND (
		    column_default LIKE 'nextval%%'
		    OR identity_generation IS NOT NULL
		  )
		ORDER BY table_schema, table_name, ordinal_position`, strings.Join(placeholders, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list serial columns: %w", err)
	}
	defer rows.Close()

	type serialCol struct {
		schema, table, column string
	}
	var cols []serialCol
	for rows.Next() {
		var c serialCol
		if err := rows.Scan(&c.schema, &c.table, &c.column); err != nil {
			return fmt.Errorf("scan serial column: %w", err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list serial columns: %w", err)
	}

	for _, c := range cols {
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
