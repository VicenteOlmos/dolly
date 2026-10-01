package clone

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type publicationSpec struct {
	name                    string
	insert                  bool
	update                  bool
	delete                  bool
	truncate                bool
	allTables               bool
	publishViaPartitionRoot bool
	schemas                 []string
	tables                  []publicationTable
}

type publicationTable struct {
	schema  string
	name    string
	columns []string
	qual    string
}

func loadPublications(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)

	pubQuery := fmt.Sprintf(`
		SELECT DISTINCT p.pubname, p.pubinsert, p.pubupdate, p.pubdelete, p.pubtruncate,
		       p.puballtables, p.pubviaroot
		FROM pg_publication p
		WHERE p.puballtables
		   OR EXISTS (
		     SELECT 1 FROM pg_publication_rel pr
		     JOIN pg_class c ON c.oid = pr.prrelid
		     JOIN pg_namespace n ON n.oid = c.relnamespace
		     WHERE pr.prpubid = p.oid AND n.nspname IN (%s)
		   )
		   OR EXISTS (
		     SELECT 1 FROM pg_publication_namespace pn
		     JOIN pg_namespace n ON n.oid = pn.pnnspid
		     WHERE pn.pnpubid = p.oid AND n.nspname IN (%s)
		   )
		ORDER BY p.pubname`, inClause, inClause)
	pubRows, err := q.QueryContext(ctx, pubQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	defer pubRows.Close()

	byName := map[string]*publicationSpec{}
	var order []string
	for pubRows.Next() {
		var pubName string
		var insert, update, delete, truncate, allTables, viaRoot bool
		if err := pubRows.Scan(&pubName, &insert, &update, &delete, &truncate, &allTables, &viaRoot); err != nil {
			return nil, fmt.Errorf("scan publication: %w", err)
		}
		spec := &publicationSpec{
			name:                    pubName,
			insert:                  insert,
			update:                  update,
			delete:                  delete,
			truncate:                truncate,
			allTables:               allTables,
			publishViaPartitionRoot: viaRoot,
		}
		byName[pubName] = spec
		order = append(order, pubName)
	}
	if err := pubRows.Err(); err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}

	nsQuery := fmt.Sprintf(`
		SELECT p.pubname, n.nspname
		FROM pg_publication p
		JOIN pg_publication_namespace pn ON pn.pnpubid = p.oid
		JOIN pg_namespace n ON n.oid = pn.pnnspid
		WHERE n.nspname IN (%s)
		ORDER BY p.pubname, n.nspname`, inClause)
	nsRows, err := q.QueryContext(ctx, nsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list publication schemas: %w", err)
	}
	defer nsRows.Close()
	for nsRows.Next() {
		var pubName, schema string
		if err := nsRows.Scan(&pubName, &schema); err != nil {
			return nil, fmt.Errorf("scan publication schema: %w", err)
		}
		spec, ok := byName[pubName]
		if !ok {
			spec = &publicationSpec{name: pubName}
			byName[pubName] = spec
			order = append(order, pubName)
		}
		spec.schemas = append(spec.schemas, schema)
	}
	if err := nsRows.Err(); err != nil {
		return nil, fmt.Errorf("list publication schemas: %w", err)
	}

	tableQuery := fmt.Sprintf(`
		SELECT p.pubname, n.nspname, c.relname,
		       pg_get_expr(pr.prqual, c.oid),
		       (SELECT string_agg(a.attname, E'\x1f' ORDER BY u.ord)
		        FROM unnest(pr.prattrs) WITH ORDINALITY AS u(attnum, ord)
		        JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = u.attnum AND NOT a.attisdropped
		       )
		FROM pg_publication p
		JOIN pg_publication_rel pr ON pr.prpubid = p.oid
		JOIN pg_class c ON c.oid = pr.prrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN (%s)
		ORDER BY p.pubname, n.nspname, c.relname`, inClause)
	tableRows, err := q.QueryContext(ctx, tableQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list publication tables: %w", err)
	}
	defer tableRows.Close()
	for tableRows.Next() {
		var pubName, schema, table string
		var qual, colNames sql.NullString
		if err := tableRows.Scan(&pubName, &schema, &table, &qual, &colNames); err != nil {
			return nil, fmt.Errorf("scan publication table: %w", err)
		}
		spec, ok := byName[pubName]
		if !ok {
			spec = &publicationSpec{name: pubName}
			byName[pubName] = spec
			order = append(order, pubName)
		}
		tbl := publicationTable{schema: schema, name: table}
		if qual.Valid && qual.String != "" {
			tbl.qual = qual.String
		}
		if colNames.Valid && colNames.String != "" {
			tbl.columns = strings.Split(colNames.String, "\x1f")
		}
		spec.tables = append(spec.tables, tbl)
	}
	if err := tableRows.Err(); err != nil {
		return nil, fmt.Errorf("list publication tables: %w", err)
	}

	var out []string
	for _, name := range order {
		spec := byName[name]
		if !spec.allTables && len(spec.schemas) == 0 && len(spec.tables) == 0 {
			continue
		}
		out = append(out, formatCreatePublication(*spec))
	}
	return out, nil
}

func formatCreatePublication(p publicationSpec) string {
	var parts []string
	if p.allTables {
		parts = append(parts, "ALL TABLES")
	}
	for _, schema := range p.schemas {
		parts = append(parts, "TABLES IN SCHEMA "+quoteIdentifier(schema))
	}
	for _, tbl := range p.tables {
		part := "ONLY " + quoteQualifiedTable(tbl.schema, tbl.name)
		if len(tbl.columns) > 0 {
			var cols []string
			for _, col := range tbl.columns {
				cols = append(cols, quoteIdentifier(col))
			}
			part += " (" + strings.Join(cols, ", ") + ")"
		}
		if tbl.qual != "" {
			part += " WHERE (" + tbl.qual + ")"
		}
		parts = append(parts, part)
	}
	stmt := fmt.Sprintf("CREATE PUBLICATION %s FOR %s", quoteIdentifier(p.name), strings.Join(parts, ", "))
	var withOpts []string
	if opts := publicationPublishOptions(p.insert, p.update, p.delete, p.truncate); opts != "" {
		withOpts = append(withOpts, "publish = "+quoteLiteral(opts))
	}
	if p.publishViaPartitionRoot {
		withOpts = append(withOpts, "publish_via_partition_root = true")
	}
	if len(withOpts) > 0 {
		stmt += " WITH (" + strings.Join(withOpts, ", ") + ")"
	}
	return stmt
}

func publicationPublishOptions(insert, update, delete, truncate bool) string {
	if insert && update && delete && truncate {
		return ""
	}
	var parts []string
	if insert {
		parts = append(parts, "insert")
	}
	if update {
		parts = append(parts, "update")
	}
	if delete {
		parts = append(parts, "delete")
	}
	if truncate {
		parts = append(parts, "truncate")
	}
	return strings.Join(parts, ", ")
}

func applyPublications(ctx context.Context, tgtDB execer, stmts []string) error {
	for _, stmt := range stmts {
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create publication: %w", err)
		}
	}
	return nil
}
