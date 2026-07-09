package clone

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func schemaINClause(schemas []string) (string, []any) {
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, s := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s
	}
	return strings.Join(placeholders, ", "), args
}

func applyExtensions(ctx context.Context, srcDB, tgtDB *sql.DB) error {
	const query = `
		SELECT extname
		FROM pg_extension
		WHERE extname <> 'plpgsql'
		ORDER BY extname`
	rows, err := srcDB.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("list extensions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan extension: %w", err)
		}
		stmt := formatCreateExtension(name)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create extension %q: %w", name, err)
		}
	}
	return rows.Err()
}

type enumType struct {
	schema string
	name   string
	labels []string
}

func loadEnumTypes(ctx context.Context, q *sql.DB, schemas []string) ([]enumType, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, t.typname, e.enumlabel
		FROM pg_type t
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		INNER JOIN pg_enum e ON e.enumtypid = t.oid
		WHERE t.typtype = 'e'
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, t.typname, e.enumsortorder`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list enum types: %w", err)
	}
	defer rows.Close()

	var out []enumType
	byKey := make(map[string]*enumType)
	var order []string
	for rows.Next() {
		var schema, name, label string
		if err := rows.Scan(&schema, &name, &label); err != nil {
			return nil, fmt.Errorf("scan enum type: %w", err)
		}
		key := schema + "\x00" + name
		et, ok := byKey[key]
		if !ok {
			et = &enumType{schema: schema, name: name}
			byKey[key] = et
			order = append(order, key)
		}
		et.labels = append(et.labels, label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list enum types: %w", err)
	}
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, nil
}

func applyEnumTypes(ctx context.Context, tgtDB *sql.DB, enums []enumType) error {
	for _, e := range enums {
		stmt := formatCreateEnumType(e.schema, e.name, e.labels)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create enum %s.%s: %w", e.schema, e.name, err)
		}
	}
	return nil
}

type domainType struct {
	schema      string
	name        string
	baseType    string
	notNull     bool
	defaultExpr string
}

func loadDomainTypes(ctx context.Context, q *sql.DB, schemas []string) ([]domainType, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, t.typname,
		       pg_catalog.format_type(t.typbasetype, t.typtypmod),
		       t.typnotnull,
		       COALESCE(pg_catalog.pg_get_expr(t.typdefaultbin, 0), '')
		FROM pg_type t
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE t.typtype = 'd'
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, t.typname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list domain types: %w", err)
	}
	defer rows.Close()

	var out []domainType
	for rows.Next() {
		var d domainType
		if err := rows.Scan(&d.schema, &d.name, &d.baseType, &d.notNull, &d.defaultExpr); err != nil {
			return nil, fmt.Errorf("scan domain type: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func applyDomainTypes(ctx context.Context, tgtDB *sql.DB, domains []domainType) error {
	for _, d := range domains {
		stmt := formatCreateDomain(d.schema, d.name, d.baseType, d.notNull, d.defaultExpr)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create domain %s.%s: %w", d.schema, d.name, err)
		}
	}
	return nil
}

func loadCompositeTypes(ctx context.Context, q *sql.DB, schemas []string) ([]struct {
	schema string
	name   string
	attrs  []compositeAttr
}, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, t.typname
		FROM pg_type t
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE t.typtype = 'c'
		  AND t.typrelid <> 0
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, t.typname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list composite types: %w", err)
	}
	defer rows.Close()

	var out []struct {
		schema string
		name   string
		attrs  []compositeAttr
	}
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return nil, fmt.Errorf("scan composite type: %w", err)
		}
		attrs, err := loadCompositeAttrs(ctx, q, schema, name)
		if err != nil {
			return nil, err
		}
		out = append(out, struct {
			schema string
			name   string
			attrs  []compositeAttr
		}{schema, name, attrs})
	}
	return out, rows.Err()
}

func loadCompositeAttrs(ctx context.Context, q *sql.DB, schema, typeName string) ([]compositeAttr, error) {
	const query = `
		SELECT a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod)
		FROM pg_type t
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		INNER JOIN pg_class c ON c.oid = t.typrelid
		INNER JOIN pg_attribute a ON a.attrelid = c.oid
		WHERE t.typtype = 'c'
		  AND n.nspname = $1 AND t.typname = $2
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`
	rows, err := q.QueryContext(ctx, query, schema, typeName)
	if err != nil {
		return nil, fmt.Errorf("list composite attrs for %s.%s: %w", schema, typeName, err)
	}
	defer rows.Close()

	var attrs []compositeAttr
	for rows.Next() {
		var a compositeAttr
		if err := rows.Scan(&a.name, &a.typ); err != nil {
			return nil, fmt.Errorf("scan composite attr: %w", err)
		}
		attrs = append(attrs, a)
	}
	return attrs, rows.Err()
}

func applyCompositeTypes(ctx context.Context, tgtDB *sql.DB, types []struct {
	schema string
	name   string
	attrs  []compositeAttr
}) error {
	for _, ct := range types {
		stmt := formatCreateCompositeType(ct.schema, ct.name, ct.attrs)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create composite type %s.%s: %w", ct.schema, ct.name, err)
		}
	}
	return nil
}

type sequenceRow struct {
	schema      string
	name        string
	def         sequenceDef
	ownedSchema string
	ownedTable  string
	ownedColumn string
}

func loadSequences(ctx context.Context, q *sql.DB, schemas []string) ([]sequenceRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT schemaname, sequencename,
		       increment_by, min_value, max_value, start_value, cache_size, cycle
		FROM pg_sequences
		WHERE schemaname IN (%s)
		ORDER BY schemaname, sequencename`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sequences: %w", err)
	}
	defer rows.Close()

	var out []sequenceRow
	for rows.Next() {
		var s sequenceRow
		var cycle bool
		if err := rows.Scan(
			&s.schema, &s.name,
			&s.def.increment, &s.def.minValue, &s.def.maxValue, &s.def.startValue, &s.def.cache, &cycle,
		); err != nil {
			return nil, fmt.Errorf("scan sequence: %w", err)
		}
		s.def.cycle = cycle
		s.def.minValid = true
		s.def.maxValid = true
		s.def.startValid = true
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sequences: %w", err)
	}

	owned, err := loadSequenceOwnership(ctx, q, schemas)
	if err != nil {
		return nil, err
	}
	for i := range out {
		key := out[i].schema + "\x00" + out[i].name
		if o, ok := owned[key]; ok {
			out[i].ownedSchema = o.schema
			out[i].ownedTable = o.table
			out[i].ownedColumn = o.column
		}
	}
	return out, nil
}

type sequenceOwnership struct {
	schema string
	table  string
	column string
}

func loadSequenceOwnership(ctx context.Context, q *sql.DB, schemas []string) (map[string]sequenceOwnership, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT seq_ns.nspname, seq.relname, tbl_ns.nspname, tbl.relname, a.attname
		FROM pg_class seq
		INNER JOIN pg_namespace seq_ns ON seq_ns.oid = seq.relnamespace
		INNER JOIN pg_depend dep ON dep.objid = seq.oid AND dep.deptype = 'a'
		INNER JOIN pg_class tbl ON tbl.oid = dep.refobjid
		INNER JOIN pg_namespace tbl_ns ON tbl_ns.oid = tbl.relnamespace
		INNER JOIN pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = dep.refobjsubid AND NOT a.attisdropped
		WHERE seq.relkind = 'S'
		  AND seq_ns.nspname IN (%s)
		ORDER BY seq_ns.nspname, seq.relname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sequence ownership: %w", err)
	}
	defer rows.Close()

	out := make(map[string]sequenceOwnership)
	for rows.Next() {
		var seqSchema, seqName, tblSchema, tblName, column string
		if err := rows.Scan(&seqSchema, &seqName, &tblSchema, &tblName, &column); err != nil {
			return nil, fmt.Errorf("scan sequence ownership: %w", err)
		}
		out[seqSchema+"\x00"+seqName] = sequenceOwnership{schema: tblSchema, table: tblName, column: column}
	}
	return out, rows.Err()
}

func applySequences(ctx context.Context, tgtDB *sql.DB, seqs []sequenceRow) error {
	for _, s := range seqs {
		stmt := formatCreateSequence(s.schema, s.name, s.def)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create sequence %s.%s: %w", s.schema, s.name, err)
		}
	}
	for _, s := range seqs {
		if s.ownedSchema == "" || s.ownedTable == "" || s.ownedColumn == "" {
			continue
		}
		stmt := formatAlterSequenceOwnedBy(s.schema, s.name, s.ownedSchema, s.ownedTable, s.ownedColumn)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sequence owned by %s.%s: %w", s.schema, s.name, err)
		}
	}
	return nil
}

type checkConstraint struct {
	name string
	def  string
}

func loadCheckConstraints(ctx context.Context, q *sql.DB, schema, table string) ([]checkConstraint, error) {
	const query = `
		SELECT con.conname, pg_get_constraintdef(con.oid, true)
		FROM pg_constraint con
		INNER JOIN pg_class c ON c.oid = con.conrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE con.contype = 'c'
		  AND n.nspname = $1 AND c.relname = $2
		ORDER BY con.conname`
	rows, err := q.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, fmt.Errorf("load check constraints for %q.%q: %w", schema, table, err)
	}
	defer rows.Close()

	var out []checkConstraint
	for rows.Next() {
		var cc checkConstraint
		if err := rows.Scan(&cc.name, &cc.def); err != nil {
			return nil, fmt.Errorf("scan check constraint: %w", err)
		}
		out = append(out, cc)
	}
	return out, rows.Err()
}

type foreignKeyConstraint struct {
	name string
	def  string
}

func loadForeignKeyConstraints(ctx context.Context, q *sql.DB, schema, table string) ([]foreignKeyConstraint, error) {
	const query = `
		SELECT con.conname, pg_get_constraintdef(con.oid, true)
		FROM pg_constraint con
		INNER JOIN pg_class c ON c.oid = con.conrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE con.contype = 'f'
		  AND n.nspname = $1 AND c.relname = $2
		ORDER BY con.conname`
	rows, err := q.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, fmt.Errorf("load foreign keys for %q.%q: %w", schema, table, err)
	}
	defer rows.Close()

	var out []foreignKeyConstraint
	for rows.Next() {
		var fk foreignKeyConstraint
		if err := rows.Scan(&fk.name, &fk.def); err != nil {
			return nil, fmt.Errorf("scan foreign key: %w", err)
		}
		out = append(out, fk)
	}
	return out, rows.Err()
}

func applyForeignKeyConstraints(ctx context.Context, tgtDB *sql.DB, schema, table string, fks []foreignKeyConstraint) error {
	for _, fk := range fks {
		stmt := formatAlterTableAddConstraint(schema, table, fk.name, fk.def)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add foreign key %q on %s: %w", fk.name, quoteQualifiedTable(schema, table), err)
		}
	}
	return nil
}

type indexRow struct {
	schema string
	table  string
	name   string
	def    string
}

func loadIndexes(ctx context.Context, q *sql.DB, schemas []string) ([]indexRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT i.schemaname, i.tablename, i.indexname, i.indexdef
		FROM pg_indexes i
		WHERE i.schemaname IN (%s)
		  AND NOT EXISTS (
		    SELECT 1
		    FROM pg_constraint con
		    INNER JOIN pg_class c ON c.oid = con.conrelid
		    INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		    WHERE con.contype IN ('p', 'u')
		      AND n.nspname = i.schemaname
		      AND c.relname = i.tablename
		      AND con.conname = i.indexname
		  )
		ORDER BY i.schemaname, i.tablename, i.indexname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	defer rows.Close()

	var out []indexRow
	for rows.Next() {
		var idx indexRow
		if err := rows.Scan(&idx.schema, &idx.table, &idx.name, &idx.def); err != nil {
			return nil, fmt.Errorf("scan index: %w", err)
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

func applyIndexes(ctx context.Context, tgtDB *sql.DB, indexes []indexRow) error {
	for _, idx := range indexes {
		if _, err := tgtDB.ExecContext(ctx, idx.def); err != nil {
			return fmt.Errorf("create index %s on %s.%s: %w", idx.name, idx.schema, idx.table, err)
		}
	}
	return nil
}

type viewRow struct {
	schema       string
	name         string
	definition   string
	materialized bool
}

func loadViews(ctx context.Context, q *sql.DB, schemas []string) ([]viewRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, pg_get_viewdef(c.oid, true), c.relkind = 'm'
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('v', 'm')
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	defer rows.Close()

	var out []viewRow
	for rows.Next() {
		var v viewRow
		if err := rows.Scan(&v.schema, &v.name, &v.definition, &v.materialized); err != nil {
			return nil, fmt.Errorf("scan view: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// applyViews creates views in multiple passes to handle simple dependency chains.
func applyViews(ctx context.Context, tgtDB *sql.DB, views []viewRow) error {
	pending := append([]viewRow(nil), views...)
	const maxPasses = 16
	for pass := 0; pass < maxPasses && len(pending) > 0; pass++ {
		var remaining []viewRow
		for _, v := range pending {
			stmt := formatCreateView(v.schema, v.name, v.definition, v.materialized)
			if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
				remaining = append(remaining, v)
				continue
			}
		}
		if len(remaining) == len(pending) {
			v := pending[0]
			stmt := formatCreateView(v.schema, v.name, v.definition, v.materialized)
			if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("create view %s.%s: %w", v.schema, v.name, err)
			}
			remaining = pending[1:]
		}
		pending = remaining
	}
	if len(pending) > 0 {
		v := pending[0]
		return fmt.Errorf("create view %s.%s: unresolved dependencies after %d passes", v.schema, v.name, maxPasses)
	}
	return nil
}

type commentRow struct {
	kind        string
	schema      string
	object      string
	column      string
	description string
}

func loadComments(ctx context.Context, q *sql.DB, schemas []string) ([]commentRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT 'schema', n.nspname, '', '', d.description
		FROM pg_description d
		INNER JOIN pg_namespace n ON n.oid = d.objoid
		WHERE d.classoid = 'pg_namespace'::regclass AND d.objsubid = 0
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT CASE WHEN c.relkind = 'm' THEN 'matview' WHEN c.relkind = 'v' THEN 'view' ELSE 'table' END,
		       n.nspname, c.relname, '', d.description
		FROM pg_description d
		INNER JOIN pg_class c ON c.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE d.classoid = 'pg_class'::regclass AND d.objsubid = 0
		  AND c.relkind IN ('r', 'p', 'v', 'm')
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'column', n.nspname, c.relname, a.attname, d.description
		FROM pg_description d
		INNER JOIN pg_class c ON c.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		INNER JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.objsubid
		WHERE d.classoid = 'pg_class'::regclass AND d.objsubid > 0
		  AND NOT a.attisdropped
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'sequence', n.nspname, c.relname, '', d.description
		FROM pg_description d
		INNER JOIN pg_class c ON c.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE d.classoid = 'pg_class'::regclass AND d.objsubid = 0
		  AND c.relkind = 'S'
		  AND n.nspname IN (%s)
		ORDER BY 1, 2, 3, 4`, inClause, inClause, inClause, inClause)
	args = append(append(append(append([]any{}, args...), args...), args...), args...)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()

	var out []commentRow
	for rows.Next() {
		var c commentRow
		if err := rows.Scan(&c.kind, &c.schema, &c.object, &c.column, &c.description); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		if strings.TrimSpace(c.description) == "" {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func applyComments(ctx context.Context, tgtDB *sql.DB, comments []commentRow) error {
	for _, c := range comments {
		stmt := formatCommentOn(c.kind, c.schema, c.object, c.column, c.description)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("comment on %s %s.%s: %w", c.kind, c.schema, c.object, err)
		}
	}
	return nil
}

type grantRow struct {
	schema     string
	object     string
	grantee    string
	privileges []string
	isSchema   bool
}

func loadGrants(ctx context.Context, q *sql.DB, schemas []string) ([]grantRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT table_schema, table_name, grantee, privilege_type
		FROM information_schema.table_privileges
		WHERE table_schema IN (%s)
		  AND grantee <> 'PUBLIC'
		UNION ALL
		SELECT object_schema, '', grantee, privilege_type
		FROM information_schema.usage_privileges
		WHERE object_type = 'SCHEMA' AND object_schema IN (%s)
		ORDER BY 1, 2, 3`, inClause, inClause)
	args = append(append([]any{}, args...), args...)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer rows.Close()

	type key struct {
		schema, object, grantee string
		isSchema                bool
	}
	byKey := make(map[key][]string)
	var order []key
	for rows.Next() {
		var schema, object, grantee, priv string
		if err := rows.Scan(&schema, &object, &grantee, &priv); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		isSchema := object == ""
		k := key{schema: schema, object: object, grantee: grantee, isSchema: isSchema}
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], strings.ToUpper(priv))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	var out []grantRow
	for _, k := range order {
		out = append(out, grantRow{
			schema:     k.schema,
			object:     k.object,
			grantee:    k.grantee,
			privileges: byKey[k],
			isSchema:   k.isSchema,
		})
	}
	return out, nil
}

func applyGrants(ctx context.Context, tgtDB *sql.DB, grants []grantRow) error {
	for _, g := range grants {
		privs := strings.Join(g.privileges, ", ")
		var stmt string
		if g.isSchema {
			stmt = formatGrantSchema(privs, g.schema, g.grantee)
		} else {
			stmt = formatGrantTable(privs, g.schema, g.object, g.grantee)
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			target := g.schema
			if g.object != "" {
				target += "." + g.object
			}
			return fmt.Errorf("grant on %s: %w", target, err)
		}
	}
	return nil
}

type rlsTable struct {
	schema string
	table  string
	force  bool
}

func loadRLSTables(ctx context.Context, q *sql.DB, schemas []string) ([]rlsTable, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, c.relforcerowsecurity
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relrowsecurity
		  AND c.relkind IN ('r', 'p')
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list rls tables: %w", err)
	}
	defer rows.Close()

	var out []rlsTable
	for rows.Next() {
		var r rlsTable
		if err := rows.Scan(&r.schema, &r.table, &r.force); err != nil {
			return nil, fmt.Errorf("scan rls table: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadPolicies(ctx context.Context, q *sql.DB, schemas []string) ([]struct {
	schema string
	table  string
	pol    policyDef
}, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, pol.polname,
		       CASE pol.polcmd
		         WHEN 'r' THEN 'SELECT'
		         WHEN 'a' THEN 'INSERT'
		         WHEN 'w' THEN 'UPDATE'
		         WHEN 'd' THEN 'DELETE'
		         WHEN '*' THEN 'ALL'
		         ELSE ''
		       END,
		       pol.polpermissive,
		       COALESCE(pg_get_expr(pol.polqual, pol.polrelid), ''),
		       COALESCE(pg_get_expr(pol.polwithcheck, pol.polrelid), ''),
		       COALESCE(array_to_string(ARRAY(
		         SELECT rolname FROM pg_roles r WHERE r.oid = ANY (pol.polroles)
		       ), ','), '')
		FROM pg_policy pol
		INNER JOIN pg_class c ON c.oid = pol.polrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, pol.polname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer rows.Close()

	var out []struct {
		schema string
		table  string
		pol    policyDef
	}
	for rows.Next() {
		var schema, table, rolesCSV string
		var p policyDef
		if err := rows.Scan(&schema, &table, &p.name, &p.command, &p.permissive, &p.using, &p.withCheck, &rolesCSV); err != nil {
			return nil, fmt.Errorf("scan policy: %w", err)
		}
		if rolesCSV != "" {
			p.roles = strings.Split(rolesCSV, ",")
		}
		out = append(out, struct {
			schema string
			table  string
			pol    policyDef
		}{schema, table, p})
	}
	return out, rows.Err()
}

func applyRLS(ctx context.Context, tgtDB *sql.DB, tables []rlsTable, policies []struct {
	schema string
	table  string
	pol    policyDef
}) error {
	for _, r := range tables {
		if _, err := tgtDB.ExecContext(ctx, formatEnableRLS(r.schema, r.table, false)); err != nil {
			return fmt.Errorf("enable rls on %s.%s: %w", r.schema, r.table, err)
		}
		if r.force {
			stmt := fmt.Sprintf("ALTER TABLE %s FORCE ROW LEVEL SECURITY", quoteQualifiedTable(r.schema, r.table))
			if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("force rls on %s.%s: %w", r.schema, r.table, err)
			}
		}
	}
	for _, item := range policies {
		stmt := formatCreatePolicy(item.schema, item.table, item.pol)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create policy %q on %s.%s: %w", item.pol.name, item.schema, item.table, err)
		}
	}
	return nil
}
