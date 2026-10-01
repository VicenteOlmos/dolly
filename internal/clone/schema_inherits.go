package clone

import (
	"context"
	"database/sql"
	"fmt"
)

type inheritParent struct {
	schema string
	name   string
}

func loadClassicInherits(ctx context.Context, q *sql.DB, schemas []string) (map[string][]inheritParent, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, pn.nspname, pc.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class pc ON pc.oid = i.inhparent
		JOIN pg_namespace pn ON pn.oid = pc.relnamespace
		WHERE NOT c.relispartition
		  AND pc.relkind = 'r'
		  AND n.nspname IN (%s)
		  AND pn.nspname IN (%s)
		ORDER BY n.nspname, c.relname, i.inhseqno`, inClause, inClause)
	// Both IN lists reuse the same placeholders, so the schema args are passed once.
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list table inheritance: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]inheritParent)
	for rows.Next() {
		var childSchema, childName, parentSchema, parentName string
		if err := rows.Scan(&childSchema, &childName, &parentSchema, &parentName); err != nil {
			return nil, fmt.Errorf("scan table inheritance: %w", err)
		}
		key := childSchema + "." + childName
		out[key] = append(out[key], inheritParent{schema: parentSchema, name: parentName})
	}
	return out, rows.Err()
}

func loadInheritedColumnNames(ctx context.Context, q *sql.DB, schemas []string) (map[string]map[string]bool, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, a.attname
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE a.attnum > 0 AND NOT a.attisdropped
		  AND a.attinhcount > 0 AND NOT a.attislocal
		  AND c.relkind IN ('r', 'p')
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, a.attnum`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list inherited columns: %w", err)
	}
	defer rows.Close()

	out := make(map[string]map[string]bool)
	for rows.Next() {
		var schema, table, column string
		if err := rows.Scan(&schema, &table, &column); err != nil {
			return nil, fmt.Errorf("scan inherited column: %w", err)
		}
		key := schema + "." + table
		if out[key] == nil {
			out[key] = make(map[string]bool)
		}
		out[key][column] = true
	}
	return out, rows.Err()
}

func filterInheritedTableColumns(cols map[string][]schemaColumn, classic map[string][]inheritParent, inherited map[string]map[string]bool) {
	for key, parents := range classic {
		if len(parents) == 0 {
			continue
		}
		inh := inherited[key]
		if len(inh) == 0 {
			continue
		}
		list := cols[key]
		filtered := make([]schemaColumn, 0, len(list))
		for _, c := range list {
			if !inh[c.name] {
				filtered = append(filtered, c)
			}
		}
		cols[key] = filtered
	}
}
