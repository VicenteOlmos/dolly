package clone

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type foreignTableColumn struct {
	name     string
	sqlType  string
	nullable bool
	options  map[string]string
}

type foreignTableDef struct {
	schema                string
	name                  string
	server                string
	columns               []foreignTableColumn
	options               map[string]string
	partitionParentSchema string
	partitionParentName   string
	partitionBound        string
}

const foreignTableColumnTypeSQL = `
		  CASE WHEN type_ns.nspname <> 'pg_catalog' THEN
		    pg_catalog.quote_ident(type_ns.nspname) || '.' || pg_catalog.quote_ident(COALESCE(elem.typname, t.typname)) ||
		    CASE WHEN a.atttypmod >= 0 THEN COALESCE(substring(pg_catalog.format_type(a.atttypid, a.atttypmod) from '[(][^)]*[)]'), '') ELSE '' END ||
		    CASE WHEN elem.oid IS NOT NULL THEN repeat('[]', greatest(a.attndims, 1)) ELSE '' END
		  ELSE pg_catalog.format_type(a.atttypid, a.atttypmod) END`

func loadForeignTables(ctx context.Context, q *sql.DB, schemas []string) ([]foreignTableDef, error) {
	inClause, args := schemaINClause(schemas)
	metaQuery := fmt.Sprintf(`
		SELECT n.nspname, c.relname, srv.srvname, opt.option_name, opt.option_value,
		       c.relispartition,
		       CASE WHEN c.relispartition THEN COALESCE(pg_get_expr(c.relpartbound, c.oid), '') ELSE '' END,
		       CASE WHEN c.relispartition THEN COALESCE(pn.nspname, '') ELSE '' END,
		       CASE WHEN c.relispartition THEN COALESCE(p.relname, '') ELSE '' END
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		INNER JOIN pg_foreign_table ft ON ft.ftrelid = c.oid
		LEFT JOIN pg_foreign_server srv ON srv.oid = ft.ftserver
		LEFT JOIN LATERAL pg_catalog.pg_options_to_table(ft.ftoptions) opt ON true
		LEFT JOIN pg_inherits i ON c.relispartition AND i.inhrelid = c.oid
		LEFT JOIN pg_class p ON p.oid = i.inhparent
		LEFT JOIN pg_namespace pn ON pn.oid = p.relnamespace
		WHERE c.relkind = 'f'
		  AND n.nspname IN (%s)
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_class'::regclass
		      AND d.objid = c.oid
		      AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, c.relname, opt.option_name`, inClause)
	metaRows, err := q.QueryContext(ctx, metaQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list foreign tables: %w", err)
	}
	defer metaRows.Close()

	byKey := make(map[string]*foreignTableDef)
	var order []string
	for metaRows.Next() {
		var schema, name string
		var server sql.NullString
		var optName, optValue sql.NullString
		var isPartition bool
		var bound, parentSchema, parentName string
		if err := metaRows.Scan(&schema, &name, &server, &optName, &optValue, &isPartition, &bound, &parentSchema, &parentName); err != nil {
			return nil, fmt.Errorf("scan foreign table: %w", err)
		}
		key := schema + "\x00" + name
		ft, ok := byKey[key]
		if !ok {
			ft = &foreignTableDef{schema: schema, name: name, options: make(map[string]string)}
			if server.Valid {
				ft.server = server.String
			}
			byKey[key] = ft
			order = append(order, key)
		} else if ft.server == "" && server.Valid && server.String != "" {
			ft.server = server.String
		}
		if optName.Valid && optValue.Valid {
			ft.options[optName.String] = optValue.String
		}
		if isPartition {
			ft.partitionParentSchema = parentSchema
			ft.partitionParentName = parentName
			ft.partitionBound = bound
		}
	}
	if err := metaRows.Err(); err != nil {
		return nil, fmt.Errorf("list foreign tables: %w", err)
	}

	colQuery := fmt.Sprintf(`
		SELECT n.nspname, c.relname, a.attname,%s, NOT a.attnotnull,
		       colopt.option_name, colopt.option_value
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		INNER JOIN pg_foreign_table ft ON ft.ftrelid = c.oid
		INNER JOIN pg_attribute a ON a.attrelid = c.oid
		JOIN pg_type t ON t.oid = a.atttypid
		LEFT JOIN pg_type elem ON elem.oid = t.typelem AND t.typlen = -1
		JOIN pg_namespace type_ns ON type_ns.oid = t.typnamespace
		LEFT JOIN LATERAL pg_catalog.pg_options_to_table(a.attfdwoptions) colopt ON true
		WHERE c.relkind = 'f'
		  AND a.attnum > 0 AND NOT a.attisdropped
		  AND n.nspname IN (%s)
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_class'::regclass
		      AND d.objid = c.oid
		      AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, c.relname, a.attnum, colopt.option_name`, foreignTableColumnTypeSQL, inClause)
	colRows, err := q.QueryContext(ctx, colQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list foreign table columns: %w", err)
	}
	defer colRows.Close()

	colIndex := make(map[string]map[string]int)
	for colRows.Next() {
		var schema, name, colName, sqlType string
		var nullable bool
		var optName, optValue sql.NullString
		if err := colRows.Scan(&schema, &name, &colName, &sqlType, &nullable, &optName, &optValue); err != nil {
			return nil, fmt.Errorf("scan foreign table column: %w", err)
		}
		key := schema + "\x00" + name
		ft, ok := byKey[key]
		if !ok {
			ft = &foreignTableDef{schema: schema, name: name, options: make(map[string]string)}
			byKey[key] = ft
			order = append(order, key)
		}
		if colIndex[key] == nil {
			colIndex[key] = make(map[string]int)
		}
		idx, ok := colIndex[key][colName]
		if !ok {
			ft.columns = append(ft.columns, foreignTableColumn{
				name:     colName,
				sqlType:  sqlType,
				nullable: nullable,
				options:  make(map[string]string),
			})
			idx = len(ft.columns) - 1
			colIndex[key][colName] = idx
		}
		if optName.Valid && optValue.Valid {
			ft.columns[idx].options[optName.String] = optValue.String
		}
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("list foreign table columns: %w", err)
	}

	out := make([]foreignTableDef, 0, len(order))
	for _, key := range order {
		ft := byKey[key]
		if strings.TrimSpace(ft.server) == "" {
			return nil, fmt.Errorf("foreign table %s has no foreign server", quoteQualifiedTable(ft.schema, ft.name))
		}
		if len(ft.columns) == 0 {
			return nil, fmt.Errorf("foreign table %s has no columns", quoteQualifiedTable(ft.schema, ft.name))
		}
		if ft.partitionParentSchema != "" || ft.partitionParentName != "" {
			if strings.TrimSpace(ft.partitionParentSchema) == "" || strings.TrimSpace(ft.partitionParentName) == "" {
				return nil, fmt.Errorf("foreign partition %s has incomplete parent", quoteQualifiedTable(ft.schema, ft.name))
			}
			if strings.TrimSpace(ft.partitionBound) == "" {
				return nil, fmt.Errorf("foreign partition %s has no partition bound", quoteQualifiedTable(ft.schema, ft.name))
			}
		}
		out = append(out, *ft)
	}
	return out, nil
}

func applyForeignTables(ctx context.Context, tgtDB execer, tables []foreignTableDef) error {
	for _, ft := range tables {
		stmt := formatCreateForeignTable(ft)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create foreign table %s: %w", quoteQualifiedTable(ft.schema, ft.name), err)
		}
	}
	return nil
}

func formatCreateForeignTable(ft foreignTableDef) string {
	return formatCreateForeignTableParts(ft.schema, ft.name, ft.server, ft.columns, ft.options, ft.partitionParentSchema, ft.partitionParentName, ft.partitionBound)
}

func formatCreateForeignTableParts(schema, name, server string, cols []foreignTableColumn, options map[string]string, partitionParentSchema, partitionParentName, partitionBound string) string {
	if partitionParentSchema != "" && partitionParentName != "" {
		return formatCreateForeignPartition(schema, name, server, cols, options, partitionParentSchema, partitionParentName, partitionBound)
	}
	var colParts []string
	for _, c := range cols {
		part := fmt.Sprintf("%s %s", quoteIdentifier(c.name), c.sqlType)
		if opt := formatFDWOptionsClause(c.options); opt != "" {
			part += opt
		}
		if !c.nullable {
			part += " NOT NULL"
		}
		colParts = append(colParts, part)
	}
	stmt := fmt.Sprintf(
		"CREATE FOREIGN TABLE %s (%s) SERVER %s",
		quoteQualifiedTable(schema, name),
		strings.Join(colParts, ", "),
		quoteIdentifier(server),
	)
	if opt := formatFDWOptionsClause(options); opt != "" {
		stmt += opt
	}
	return stmt
}

// formatCreateForeignPartition emits PostgreSQL 16 partition syntax:
// CREATE FOREIGN TABLE name PARTITION OF parent [ (col [WITH OPTIONS] [NOT NULL]) ]
// FOR VALUES ... SERVER server [OPTIONS (...)].
// Column types come from the parent and must not be repeated.
func formatCreateForeignPartition(schema, name, server string, cols []foreignTableColumn, options map[string]string, partitionParentSchema, partitionParentName, partitionBound string) string {
	stmt := fmt.Sprintf(
		"CREATE FOREIGN TABLE %s PARTITION OF %s",
		quoteQualifiedTable(schema, name),
		quoteQualifiedTable(partitionParentSchema, partitionParentName),
	)
	var colParts []string
	for _, c := range cols {
		var extras []string
		if opt := formatFDWOptionsClause(c.options); opt != "" {
			extras = append(extras, "WITH"+opt)
		}
		if !c.nullable {
			extras = append(extras, "NOT NULL")
		}
		if len(extras) == 0 {
			continue
		}
		colParts = append(colParts, quoteIdentifier(c.name)+" "+strings.Join(extras, " "))
	}
	if len(colParts) > 0 {
		stmt += " (" + strings.Join(colParts, ", ") + ")"
	}
	if bound := strings.TrimSpace(partitionBound); bound != "" {
		stmt += " " + bound
	}
	stmt += " SERVER " + quoteIdentifier(server)
	if opt := formatFDWOptionsClause(options); opt != "" {
		stmt += opt
	}
	return stmt
}

func formatFDWOptionsClause(options map[string]string) string {
	if len(options) == 0 {
		return ""
	}
	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var optParts []string
	for _, k := range keys {
		optParts = append(optParts, fmt.Sprintf("%s %s", k, quoteLiteral(options[k])))
	}
	return " OPTIONS (" + strings.Join(optParts, ", ") + ")"
}
