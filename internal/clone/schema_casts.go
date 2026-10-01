package clone

import (
	"context"
	"database/sql"
	"fmt"
)

type castRow struct {
	srcSchema   string
	srcType     string
	tgtSchema   string
	tgtType     string
	castMethod  string
	castContext string
	fnSchema    string
	fnName      string
	fnArgs      string
}

func loadCasts(ctx context.Context, q *sql.DB, schemas []string) ([]castRow, error) {
	if len(schemas) == 0 {
		return nil, nil
	}
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT src_ns.nspname, src_t.typname,
		       tgt_ns.nspname, tgt_t.typname,
		       c.castmethod::text, c.castcontext::text,
		       COALESCE(fn_ns.nspname, ''), COALESCE(fn.proname, ''),
		       CASE WHEN c.castfunc <> 0 THEN pg_catalog.pg_get_function_identity_arguments(c.castfunc) ELSE '' END
		FROM pg_cast c
		JOIN pg_type src_t ON src_t.oid = c.castsource
		JOIN pg_namespace src_ns ON src_ns.oid = src_t.typnamespace
		JOIN pg_type tgt_t ON tgt_t.oid = c.casttarget
		JOIN pg_namespace tgt_ns ON tgt_ns.oid = tgt_t.typnamespace
		LEFT JOIN pg_proc fn ON fn.oid = c.castfunc AND c.castfunc <> 0
		LEFT JOIN pg_namespace fn_ns ON fn_ns.oid = fn.pronamespace
		WHERE src_ns.nspname IN (%s)
		  AND tgt_ns.nspname IN (%s)
		  AND src_ns.nspname <> 'pg_catalog'
		  AND tgt_ns.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_cast'::regclass
		      AND d.objid = c.oid
		      AND d.deptype = 'i'
		  )
		ORDER BY src_ns.nspname, src_t.typname, tgt_ns.nspname, tgt_t.typname`, inClause, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list casts: %w", err)
	}
	defer rows.Close()

	var out []castRow
	for rows.Next() {
		var row castRow
		if err := rows.Scan(
			&row.srcSchema, &row.srcType,
			&row.tgtSchema, &row.tgtType,
			&row.castMethod, &row.castContext,
			&row.fnSchema, &row.fnName, &row.fnArgs,
		); err != nil {
			return nil, fmt.Errorf("scan cast: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func applyCasts(ctx context.Context, tgtDB execer, casts []castRow) error {
	for _, row := range casts {
		stmt, ok := formatCreateCast(row)
		if !ok {
			continue
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create cast (%s.%s AS %s.%s): %w", row.srcSchema, row.srcType, row.tgtSchema, row.tgtType, err)
		}
	}
	return nil
}
