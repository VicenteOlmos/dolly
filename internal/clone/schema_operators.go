package clone

import (
	"context"
	"database/sql"
	"fmt"
)

type operatorSpec struct {
	schema     string
	name       string
	funcSchema string
	funcName   string
	leftType   string
	rightType  string
}

func loadOperators(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, o.oprname,
		       pn.nspname, p.proname,
		       format_type(o.oprleft, NULL),
		       format_type(o.oprright, NULL)
		FROM pg_operator o
		INNER JOIN pg_namespace n ON n.oid = o.oprnamespace
		INNER JOIN pg_proc p ON p.oid = o.oprcode
		INNER JOIN pg_namespace pn ON pn.oid = p.pronamespace
		WHERE n.nspname IN (%s)
		  AND o.oprleft <> 0 AND o.oprright <> 0
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_operator'::regclass
		      AND d.objid = o.oid
		      AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, o.oprname, o.oid`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list operators: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var spec operatorSpec
		if err := rows.Scan(
			&spec.schema, &spec.name,
			&spec.funcSchema, &spec.funcName,
			&spec.leftType, &spec.rightType,
		); err != nil {
			return nil, fmt.Errorf("scan operator: %w", err)
		}
		if spec.funcName == "" {
			continue
		}
		out = append(out, formatCreateOperator(spec))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list operators: %w", err)
	}
	return out, nil
}

func formatCreateOperator(o operatorSpec) string {
	return fmt.Sprintf(
		"CREATE OPERATOR %s (\n\tFUNCTION = %s,\n\tLEFTARG = %s,\n\tRIGHTARG = %s\n)",
		quoteQualifiedTable(o.schema, o.name),
		quoteQualifiedTable(o.funcSchema, o.funcName),
		o.leftType,
		o.rightType,
	)
}

func applyOperators(ctx context.Context, tgtDB execer, stmts []string) error {
	for _, stmt := range stmts {
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create operator: %w", err)
		}
	}
	return nil
}
