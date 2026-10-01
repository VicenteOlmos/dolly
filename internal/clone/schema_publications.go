package clone

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type publicationSpec struct {
	name     string
	insert   bool
	update   bool
	delete   bool
	truncate bool
	tables   []publicationTable
}

type publicationTable struct {
	schema string
	name   string
}

func loadPublications(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT p.pubname, p.pubinsert, p.pubupdate, p.pubdelete, p.pubtruncate,
		       n.nspname, c.relname
		FROM pg_publication p
		INNER JOIN pg_publication_rel pr ON pr.prpubid = p.oid
		INNER JOIN pg_class c ON c.oid = pr.prrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN (%s)
		ORDER BY p.pubname, n.nspname, c.relname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	defer rows.Close()

	byName := map[string]*publicationSpec{}
	var order []string
	for rows.Next() {
		var pubName, schema, table string
		var insert, update, delete, truncate bool
		if err := rows.Scan(&pubName, &insert, &update, &delete, &truncate, &schema, &table); err != nil {
			return nil, fmt.Errorf("scan publication: %w", err)
		}
		spec, ok := byName[pubName]
		if !ok {
			spec = &publicationSpec{
				name:     pubName,
				insert:   insert,
				update:   update,
				delete:   delete,
				truncate: truncate,
			}
			byName[pubName] = spec
			order = append(order, pubName)
		}
		spec.tables = append(spec.tables, publicationTable{schema: schema, name: table})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}

	var out []string
	for _, name := range order {
		spec := byName[name]
		if len(spec.tables) == 0 {
			continue
		}
		out = append(out, formatCreatePublication(*spec))
	}
	return out, nil
}

func formatCreatePublication(p publicationSpec) string {
	var tableParts []string
	for _, tbl := range p.tables {
		tableParts = append(tableParts, "ONLY "+quoteQualifiedTable(tbl.schema, tbl.name))
	}
	stmt := fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", quoteIdentifier(p.name), strings.Join(tableParts, ", "))
	if opts := publicationPublishOptions(p.insert, p.update, p.delete, p.truncate); opts != "" {
		stmt += " WITH (publish = " + quoteLiteral(opts) + ")"
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
