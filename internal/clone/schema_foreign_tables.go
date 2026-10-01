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
}

type foreignTableDef struct {
	schema  string
	name    string
	server  string
	columns []foreignTableColumn
	options map[string]string
}

func loadForeignTables(ctx context.Context, q *sql.DB, schemas []string) ([]foreignTableDef, error) {
	inClause, args := schemaINClause(schemas)
	metaQuery := fmt.Sprintf(`
		SELECT n.nspname, c.relname, srv.srvname, opt.option_name, opt.option_value
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		INNER JOIN pg_foreign_table ft ON ft.ftrelid = c.oid
		LEFT JOIN pg_foreign_server srv ON srv.oid = ft.ftserver
		LEFT JOIN LATERAL pg_catalog.pg_options_to_table(ft.ftoptions) opt ON true
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
		if err := metaRows.Scan(&schema, &name, &server, &optName, &optValue); err != nil {
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
	}
	if err := metaRows.Err(); err != nil {
		return nil, fmt.Errorf("list foreign tables: %w", err)
	}

	colQuery := fmt.Sprintf(`
		SELECT n.nspname, c.relname, a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		INNER JOIN pg_foreign_table ft ON ft.ftrelid = c.oid
		INNER JOIN pg_attribute a ON a.attrelid = c.oid
		WHERE c.relkind = 'f'
		  AND a.attnum > 0 AND NOT a.attisdropped
		  AND n.nspname IN (%s)
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_class'::regclass
		      AND d.objid = c.oid
		      AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, c.relname, a.attnum`, inClause)
	colRows, err := q.QueryContext(ctx, colQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list foreign table columns: %w", err)
	}
	defer colRows.Close()

	for colRows.Next() {
		var schema, name, colName, sqlType string
		var nullable bool
		if err := colRows.Scan(&schema, &name, &colName, &sqlType, &nullable); err != nil {
			return nil, fmt.Errorf("scan foreign table column: %w", err)
		}
		key := schema + "\x00" + name
		ft, ok := byKey[key]
		if !ok {
			ft = &foreignTableDef{schema: schema, name: name, options: make(map[string]string)}
			byKey[key] = ft
			order = append(order, key)
		}
		ft.columns = append(ft.columns, foreignTableColumn{
			name:     colName,
			sqlType:  sqlType,
			nullable: nullable,
		})
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
		out = append(out, *ft)
	}
	return out, nil
}

func applyForeignTables(ctx context.Context, tgtDB execer, tables []foreignTableDef) error {
	for _, ft := range tables {
		stmt := formatCreateForeignTable(ft.schema, ft.name, ft.server, ft.columns, ft.options)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create foreign table %s: %w", quoteQualifiedTable(ft.schema, ft.name), err)
		}
	}
	return nil
}

func formatCreateForeignTable(schema, name, server string, cols []foreignTableColumn, options map[string]string) string {
	var colParts []string
	for _, c := range cols {
		part := fmt.Sprintf("%s %s", quoteIdentifier(c.name), c.sqlType)
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
	if len(options) == 0 {
		return stmt
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
	stmt += " OPTIONS (" + strings.Join(optParts, ", ") + ")"
	return stmt
}
