package clone

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
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

func applyExtensions(ctx context.Context, srcDB *sql.DB, tgtDB execer) error {
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

func applyEnumTypes(ctx context.Context, tgtDB execer, enums []enumType) error {
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

func applyDomainTypes(ctx context.Context, tgtDB execer, domains []domainType) error {
	for _, d := range domains {
		stmt := formatCreateDomain(d.schema, d.name, d.baseType, d.notNull, d.defaultExpr)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create domain %s.%s: %w", d.schema, d.name, err)
		}
	}
	return nil
}

type domainCheckConstraint struct {
	schema     string
	domain     string
	name       string
	constraint string
}

func loadDomainCheckConstraints(ctx context.Context, q *sql.DB, schemas []string) ([]domainCheckConstraint, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, t.typname, c.conname, pg_get_constraintdef(c.oid, true)
		FROM pg_constraint c
		JOIN pg_type t ON t.oid = c.contypid
		JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE t.typtype = 'd' AND c.contype = 'c' AND n.nspname IN (%s)
		ORDER BY n.nspname, t.typname, c.conname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list domain check constraints: %w", err)
	}
	defer rows.Close()

	var out []domainCheckConstraint
	for rows.Next() {
		var dc domainCheckConstraint
		if err := rows.Scan(&dc.schema, &dc.domain, &dc.name, &dc.constraint); err != nil {
			return nil, fmt.Errorf("scan domain check constraint: %w", err)
		}
		out = append(out, dc)
	}
	return out, rows.Err()
}

func applyDomainCheckConstraints(ctx context.Context, tgtDB execer, checks []domainCheckConstraint) error {
	for _, dc := range checks {
		stmt := formatAlterDomainAddConstraint(dc.schema, dc.domain, dc.name, dc.constraint)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add domain check %q on %s.%s: %w", dc.name, dc.schema, dc.domain, err)
		}
	}
	return nil
}

type collationRow struct {
	schema          string
	name            string
	provider        string
	icuLocale       string
	icuRules        string
	libcCollate     string
	libcCtype       string
	isDeterministic bool
}

func loadCollations(ctx context.Context, q *sql.DB, schemas []string) ([]collationRow, error) {
	major, err := scanServerMajor(ctx, q)
	if err != nil {
		return nil, err
	}
	icuLocale := "coll.collcollate"
	icuRules := "''"
	if major >= 15 {
		icuLocale = "coll.colliculocale"
		icuRules = "COALESCE(coll.collicurules, '')"
	}
	if major >= 17 {
		icuLocale = "coll.colllocale"
	}
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, coll.collname, coll.collprovider::text,
		       COALESCE(%s, ''), %s,
		       COALESCE(coll.collcollate, ''),
		       COALESCE(coll.collctype, ''),
		       coll.collisdeterministic
		FROM pg_collation coll
		INNER JOIN pg_namespace n ON n.oid = coll.collnamespace
		WHERE coll.collprovider IN ('c', 'i')
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, coll.collname`, icuLocale, icuRules, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list collations: %w", err)
	}
	defer rows.Close()

	var out []collationRow
	for rows.Next() {
		var row collationRow
		if err := rows.Scan(
			&row.schema, &row.name, &row.provider,
			&row.icuLocale, &row.icuRules, &row.libcCollate, &row.libcCtype, &row.isDeterministic,
		); err != nil {
			return nil, fmt.Errorf("scan collation: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func applyCollations(ctx context.Context, tgtDB execer, rows []collationRow) error {
	for _, row := range rows {
		stmt, ok := formatCreateCollation(
			row.schema, row.name, row.provider,
			row.icuLocale, row.icuRules, row.libcCollate, row.libcCtype, row.isDeterministic,
		)
		if !ok {
			continue
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create collation %s.%s: %w", row.schema, row.name, err)
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
		INNER JOIN pg_class c ON c.oid = t.typrelid
		WHERE t.typtype = 'c'
		  AND c.relkind = 'c'
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
		SELECT a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod),
		  CASE WHEN a.attcollation <> 0 AND a.attcollation <> typ.typcollation THEN coll_ns.nspname ELSE '' END,
		  CASE WHEN a.attcollation <> 0 AND a.attcollation <> typ.typcollation THEN coll.collname ELSE '' END
		FROM pg_type t
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		INNER JOIN pg_class c ON c.oid = t.typrelid
		INNER JOIN pg_attribute a ON a.attrelid = c.oid
		INNER JOIN pg_type typ ON typ.oid = a.atttypid
		LEFT JOIN pg_collation coll ON coll.oid = a.attcollation
		LEFT JOIN pg_namespace coll_ns ON coll_ns.oid = coll.collnamespace
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
		if err := rows.Scan(&a.name, &a.typ, &a.collSchema, &a.collName); err != nil {
			return nil, fmt.Errorf("scan composite attr: %w", err)
		}
		attrs = append(attrs, a)
	}
	return attrs, rows.Err()
}

func applyCompositeTypes(ctx context.Context, tgtDB execer, types []struct {
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
	identity    bool
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

	seqTypes, err := loadSequenceDataTypes(ctx, q, schemas)
	if err != nil {
		return nil, err
	}
	for i := range out {
		key := out[i].schema + "\x00" + out[i].name
		if typ, ok := seqTypes[key]; ok {
			out[i].def.dataType = typ
		}
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
			out[i].identity = o.identity
		}
	}
	return out, nil
}

func loadSequenceDataTypes(ctx context.Context, q *sql.DB, schemas []string) (map[string]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, pg_catalog.format_type(s.seqtypid, NULL)
		FROM pg_sequence s
		JOIN pg_class c ON c.oid = s.seqrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN (%s)`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sequence data types: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var schema, name, dataType string
		if err := rows.Scan(&schema, &name, &dataType); err != nil {
			return nil, fmt.Errorf("scan sequence data type: %w", err)
		}
		out[schema+"\x00"+name] = dataType
	}
	return out, rows.Err()
}

type sequenceOwnership struct {
	schema   string
	table    string
	column   string
	identity bool
}

func loadSequenceOwnership(ctx context.Context, q *sql.DB, schemas []string) (map[string]sequenceOwnership, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT seq_ns.nspname, seq.relname, tbl_ns.nspname, tbl.relname, a.attname, dep.deptype = 'i'
		FROM pg_class seq
		INNER JOIN pg_namespace seq_ns ON seq_ns.oid = seq.relnamespace
		INNER JOIN pg_depend dep ON dep.objid = seq.oid AND dep.deptype IN ('a', 'i')
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
		var identity bool
		if err := rows.Scan(&seqSchema, &seqName, &tblSchema, &tblName, &column, &identity); err != nil {
			return nil, fmt.Errorf("scan sequence ownership: %w", err)
		}
		out[seqSchema+"\x00"+seqName] = sequenceOwnership{schema: tblSchema, table: tblName, column: column, identity: identity}
	}
	return out, rows.Err()
}

func applySequences(ctx context.Context, tgtDB execer, seqs []sequenceRow) error {
	for _, s := range seqs {
		if s.identity {
			continue
		}
		stmt := formatCreateSequence(s.schema, s.name, s.def)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create sequence %s.%s: %w", s.schema, s.name, err)
		}
	}
	for _, s := range seqs {
		if s.identity || s.ownedSchema == "" || s.ownedTable == "" || s.ownedColumn == "" {
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

func applyForeignKeyConstraints(ctx context.Context, tgtDB execer, schema, table string, fks []foreignKeyConstraint) error {
	for _, fk := range fks {
		stmt := formatAlterTableAddConstraint(schema, table, fk.name, fk.def)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add foreign key %q on %s: %w", fk.name, quoteQualifiedTable(schema, table), err)
		}
	}
	return nil
}

type indexRow struct {
	schema    string
	table     string
	name      string
	def       string
	inherited bool
}

func loadIndexes(ctx context.Context, q *sql.DB, schemas []string) ([]indexRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT i.schemaname, i.tablename, i.indexname, i.indexdef,
		  EXISTS (
		    SELECT 1
		    FROM pg_class ic
		    JOIN pg_namespace idx_ns ON idx_ns.oid = ic.relnamespace
		    JOIN pg_inherits inh ON inh.inhrelid = ic.oid
		    WHERE ic.relkind = 'i'
		      AND idx_ns.nspname = i.schemaname
		      AND ic.relname = i.indexname
		  ) AS inherited
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
		if err := rows.Scan(&idx.schema, &idx.table, &idx.name, &idx.def, &idx.inherited); err != nil {
			return nil, fmt.Errorf("scan index: %w", err)
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

func applyIndexes(ctx context.Context, tgtDB execer, indexes []indexRow) error {
	for _, idx := range indexes {
		if _, err := tgtDB.ExecContext(ctx, idx.def); err != nil {
			return fmt.Errorf("create index %s on %s.%s: %w", idx.name, idx.schema, idx.table, err)
		}
	}
	return nil
}

type replicaIdentityRow struct {
	schema    string
	table     string
	ident     string
	indexName string
}

func loadReplicaIdentities(ctx context.Context, q *sql.DB, schemas []string) ([]replicaIdentityRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, c.relreplident::text,
		       COALESCE(idx_class.relname, '')
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_index ix ON ix.indrelid = c.oid AND ix.indisreplident
		LEFT JOIN pg_class idx_class ON idx_class.oid = ix.indexrelid
		WHERE c.relkind IN ('r', 'p')
		  AND NOT c.relispartition
		  AND c.relreplident <> 'd'
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list replica identities: %w", err)
	}
	defer rows.Close()

	var out []replicaIdentityRow
	for rows.Next() {
		var row replicaIdentityRow
		if err := rows.Scan(&row.schema, &row.table, &row.ident, &row.indexName); err != nil {
			return nil, fmt.Errorf("scan replica identity: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func applyReplicaIdentities(ctx context.Context, tgtDB execer, rows []replicaIdentityRow) error {
	for _, row := range rows {
		stmt, ok := formatAlterTableReplicaIdentity(row.schema, row.table, row.ident, row.indexName)
		if !ok {
			continue
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("replica identity on %s.%s: %w", row.schema, row.table, err)
		}
	}
	return nil
}

type columnStorageRow struct {
	schema  string
	table   string
	column  string
	storage string
}

func loadColumnStorageOverrides(ctx context.Context, q *sql.DB, schemas []string) ([]columnStorageRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, a.attname, a.attstorage::text
		FROM pg_attribute a
		INNER JOIN pg_class c ON c.oid = a.attrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		INNER JOIN pg_type t ON t.oid = a.atttypid
		WHERE a.attnum > 0 AND NOT a.attisdropped
		  AND c.relkind IN ('r', 'p')
		  AND a.attstorage <> t.typstorage
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, a.attnum`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list column storage overrides: %w", err)
	}
	defer rows.Close()

	var out []columnStorageRow
	for rows.Next() {
		var row columnStorageRow
		if err := rows.Scan(&row.schema, &row.table, &row.column, &row.storage); err != nil {
			return nil, fmt.Errorf("scan column storage: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func applyColumnStorageOverrides(ctx context.Context, tgtDB execer, rows []columnStorageRow) error {
	for _, row := range rows {
		stmt, ok := formatAlterColumnStorage(row.schema, row.table, row.column, row.storage)
		if !ok {
			continue
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("column storage on %s.%s.%s: %w", row.schema, row.table, row.column, err)
		}
	}
	return nil
}

type columnCompressionRow struct {
	schema string
	table  string
	column string
	codec  string
}

func loadColumnCompressionOverrides(ctx context.Context, q *sql.DB, schemas []string) ([]columnCompressionRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, a.attname, a.attcompression::text
		FROM pg_attribute a
		INNER JOIN pg_class c ON c.oid = a.attrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE a.attnum > 0 AND NOT a.attisdropped
		  AND c.relkind IN ('r', 'p')
		  AND a.attcompression IN ('l', 'p')
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, a.attnum`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list column compression overrides: %w", err)
	}
	defer rows.Close()

	var out []columnCompressionRow
	for rows.Next() {
		var row columnCompressionRow
		if err := rows.Scan(&row.schema, &row.table, &row.column, &row.codec); err != nil {
			return nil, fmt.Errorf("scan column compression: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func applyColumnCompressionOverrides(ctx context.Context, tgtDB execer, rows []columnCompressionRow) error {
	for _, row := range rows {
		stmt, ok := formatAlterColumnCompression(row.schema, row.table, row.column, row.codec)
		if !ok {
			continue
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("column compression on %s.%s.%s: %w", row.schema, row.table, row.column, err)
		}
	}
	return nil
}

type tableFillfactorRow struct {
	schema     string
	table      string
	fillfactor int
}

func loadTableFillfactors(ctx context.Context, q *sql.DB, schemas []string) ([]tableFillfactorRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, opt.option_value
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL pg_catalog.pg_options_to_table(c.reloptions) opt
		WHERE c.relkind IN ('r', 'p')
		  AND c.reloptions IS NOT NULL
		  AND opt.option_name = 'fillfactor'
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list table fillfactors: %w", err)
	}
	defer rows.Close()

	var out []tableFillfactorRow
	for rows.Next() {
		var row tableFillfactorRow
		var value string
		if err := rows.Scan(&row.schema, &row.table, &value); err != nil {
			return nil, fmt.Errorf("scan table fillfactor: %w", err)
		}
		ff, err := parseFillfactorOption(value)
		if err != nil {
			continue
		}
		row.fillfactor = ff
		out = append(out, row)
	}
	return out, rows.Err()
}

func parseFillfactorOption(value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 10 || n > 100 {
		return 0, fmt.Errorf("invalid fillfactor")
	}
	return n, nil
}

func applyTableFillfactors(ctx context.Context, tgtDB execer, rows []tableFillfactorRow) error {
	for _, row := range rows {
		stmt := formatAlterTableFillfactor(row.schema, row.table, row.fillfactor)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("fillfactor on %s.%s: %w", row.schema, row.table, err)
		}
	}
	return nil
}

func loadStatistics(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT pg_catalog.pg_get_statisticsobjdef(s.oid)
		FROM pg_statistic_ext s
		INNER JOIN pg_namespace n ON n.oid = s.stxnamespace
		WHERE n.nspname IN (%s)
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.objid = s.oid AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, s.stxname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list statistics: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var def string
		if err := rows.Scan(&def); err != nil {
			return nil, fmt.Errorf("scan statistics: %w", err)
		}
		def = strings.TrimSpace(def)
		if def == "" {
			continue
		}
		out = append(out, def)
	}
	return out, rows.Err()
}

func applyStatistics(ctx context.Context, tgtDB execer, defs []string) error {
	for _, def := range defs {
		if _, err := tgtDB.ExecContext(ctx, def); err != nil {
			return fmt.Errorf("create statistics: %w", err)
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
func applyViews(ctx context.Context, tgtDB execer, views []viewRow) error {
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
		UNION ALL
		SELECT CASE WHEN p.prokind = 'p' THEN 'procedure' ELSE 'function' END, n.nspname,
		       p.proname || '(' || pg_catalog.pg_get_function_identity_arguments(p.oid) || ')',
		       '', d.description
		FROM pg_description d
		INNER JOIN pg_proc p ON p.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE d.classoid = 'pg_proc'::regclass AND d.objsubid = 0
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'index', n.nspname, c.relname, '', d.description
		FROM pg_description d
		INNER JOIN pg_class c ON c.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE d.classoid = 'pg_class'::regclass AND d.objsubid = 0
		  AND c.relkind = 'i'
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'constraint', n.nspname, c.relname, con.conname, d.description
		FROM pg_description d
		INNER JOIN pg_constraint con ON con.oid = d.objoid
		INNER JOIN pg_class c ON c.oid = con.conrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE d.classoid = 'pg_constraint'::regclass AND d.objsubid = 0
		  AND ((con.contype = 'u' AND con.conparentid = 0)
		    OR (con.contype = 'c' AND con.coninhcount = 0)
		    OR (con.contype = 'f' AND con.conparentid = 0))
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'domain_constraint', n.nspname, t.typname, con.conname, d.description
		FROM pg_description d
		INNER JOIN pg_constraint con ON con.oid = d.objoid
		INNER JOIN pg_type t ON t.oid = con.contypid
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE d.classoid = 'pg_constraint'::regclass AND d.objsubid = 0
		  AND t.typtype = 'd' AND con.contype = 'c'
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'domain', n.nspname, t.typname, '', d.description
		FROM pg_description d
		INNER JOIN pg_type t ON t.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE d.classoid = 'pg_type'::regclass AND d.objsubid = 0
		  AND t.typtype = 'd'
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'type', n.nspname, t.typname, '', d.description
		FROM pg_description d
		INNER JOIN pg_type t ON t.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE d.classoid = 'pg_type'::regclass AND d.objsubid = 0
		  AND t.typtype = 'e'
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'type', n.nspname, t.typname, '', d.description
		FROM pg_description d
		INNER JOIN pg_type t ON t.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE d.classoid = 'pg_type'::regclass AND d.objsubid = 0
		  AND t.typtype = 'c'
		  AND EXISTS (SELECT 1 FROM pg_class c WHERE c.oid = t.typrelid AND c.relkind = 'c')
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'collation', n.nspname, coll.collname, '', d.description
		FROM pg_description d
		INNER JOIN pg_collation coll ON coll.oid = d.objoid
		INNER JOIN pg_namespace n ON n.oid = coll.collnamespace
		WHERE d.classoid = 'pg_collation'::regclass AND d.objsubid = 0
		  AND n.nspname IN (%s)
		UNION ALL
		SELECT 'policy', n.nspname, c.relname, pol.polname, d.description
		FROM pg_description d
		INNER JOIN pg_policy pol ON pol.oid = d.objoid
		INNER JOIN pg_class c ON c.oid = pol.polrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE d.classoid = 'pg_policy'::regclass AND d.objsubid = 0
		  AND n.nspname IN (%s)
		ORDER BY 1, 2, 3, 4`, inClause, inClause, inClause, inClause, inClause, inClause, inClause, inClause, inClause, inClause, inClause, inClause, inClause)
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

func applyComments(ctx context.Context, tgtDB execer, comments []commentRow) error {
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

func applyGrants(ctx context.Context, tgtDB execer, grants []grantRow) error {
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

type columnGrantRow struct {
	schema     string
	table      string
	column     string
	grantee    string
	privileges []string
	grantable  bool
}

func loadColumnGrants(ctx context.Context, q *sql.DB, schemas []string) ([]columnGrantRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, a.attname, COALESCE(r.rolname, 'PUBLIC'),
		       priv.privilege_type, priv.is_grantable
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL aclexplode(a.attacl) priv
		LEFT JOIN pg_roles r ON r.oid = priv.grantee
		WHERE c.relkind IN ('r', 'p', 'v', 'm') AND a.attnum > 0 AND NOT a.attisdropped
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, a.attname, 4`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list column grants: %w", err)
	}
	defer rows.Close()

	type key struct {
		schema, table, column, grantee string
		grantable                      bool
	}
	byKey := make(map[key][]string)
	var order []key
	for rows.Next() {
		var schema, table, column, grantee, priv string
		var grantable bool
		if err := rows.Scan(&schema, &table, &column, &grantee, &priv, &grantable); err != nil {
			return nil, fmt.Errorf("scan column grant: %w", err)
		}
		k := key{schema: schema, table: table, column: column, grantee: grantee, grantable: grantable}
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], strings.ToUpper(priv))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list column grants: %w", err)
	}
	var out []columnGrantRow
	for _, k := range order {
		out = append(out, columnGrantRow{
			schema:     k.schema,
			table:      k.table,
			column:     k.column,
			grantee:    k.grantee,
			privileges: byKey[k],
			grantable:  k.grantable,
		})
	}
	return out, nil
}

func applyColumnGrants(ctx context.Context, tgtDB execer, grants []columnGrantRow) error {
	for _, g := range grants {
		privs := strings.Join(g.privileges, ", ")
		stmt := formatGrantColumn(privs, g.schema, g.table, g.column, g.grantee)
		if g.grantable {
			stmt += " WITH GRANT OPTION"
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("grant on column %s.%s.%s: %w", g.schema, g.table, g.column, err)
		}
	}
	return nil
}

type sequenceGrantRow struct {
	schema     string
	sequence   string
	grantee    string
	privileges []string
	grantable  bool
}

func loadSequenceGrants(ctx context.Context, q *sql.DB, schemas []string) ([]sequenceGrantRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, COALESCE(r.rolname, 'PUBLIC'), priv.privilege_type, priv.is_grantable
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL aclexplode(c.relacl) priv
		LEFT JOIN pg_roles r ON r.oid = priv.grantee
		WHERE c.relkind = 'S' AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, 3`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sequence grants: %w", err)
	}
	defer rows.Close()

	type key struct {
		schema, sequence, grantee string
		grantable                 bool
	}
	byKey := make(map[key][]string)
	var order []key
	for rows.Next() {
		var schema, sequence, grantee, priv string
		var grantable bool
		if err := rows.Scan(&schema, &sequence, &grantee, &priv, &grantable); err != nil {
			return nil, fmt.Errorf("scan sequence grant: %w", err)
		}
		k := key{schema: schema, sequence: sequence, grantee: grantee, grantable: grantable}
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], strings.ToUpper(priv))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sequence grants: %w", err)
	}
	var out []sequenceGrantRow
	for _, k := range order {
		out = append(out, sequenceGrantRow{
			schema:     k.schema,
			sequence:   k.sequence,
			grantee:    k.grantee,
			privileges: byKey[k],
			grantable:  k.grantable,
		})
	}
	return out, nil
}

func applySequenceGrants(ctx context.Context, tgtDB execer, grants []sequenceGrantRow) error {
	for _, g := range grants {
		privs := strings.Join(g.privileges, ", ")
		stmt := formatGrantSequence(privs, g.schema, g.sequence, g.grantee)
		if g.grantable {
			stmt += " WITH GRANT OPTION"
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("grant on sequence %s.%s: %w", g.schema, g.sequence, err)
		}
	}
	return nil
}

type routineGrantRow struct {
	schema       string
	name         string
	args         string
	kind         string
	grantee      string
	grantable    bool
	revokePublic bool
}

func loadRoutineGrants(ctx context.Context, q *sql.DB, schemas []string) ([]routineGrantRow, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, p.proname, pg_get_function_identity_arguments(p.oid),
		       CASE WHEN p.prokind = 'p' THEN 'PROCEDURE' ELSE 'FUNCTION' END,
		       COALESCE(r.rolname, 'PUBLIC'), COALESCE(priv.privilege_type, ''),
		       COALESCE(priv.is_grantable, false),
		       NOT EXISTS (SELECT 1 FROM aclexplode(p.proacl) acl
		                   WHERE acl.grantee = 0 AND acl.privilege_type = 'EXECUTE')
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		LEFT JOIN LATERAL aclexplode(p.proacl) priv ON true
		LEFT JOIN pg_roles r ON r.oid = priv.grantee
		WHERE p.proacl IS NOT NULL
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, p.proname, 3, 5`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list routine grants: %w", err)
	}
	defer rows.Close()

	var out []routineGrantRow
	seen := make(map[string]bool)
	for rows.Next() {
		var g routineGrantRow
		var priv string
		var missingPublic bool
		if err := rows.Scan(&g.schema, &g.name, &g.args, &g.kind, &g.grantee, &priv, &g.grantable, &missingPublic); err != nil {
			return nil, fmt.Errorf("scan routine grant: %w", err)
		}
		key := g.schema + "\x00" + g.name + "\x00" + g.args
		if missingPublic && !seen[key] {
			out = append(out, routineGrantRow{schema: g.schema, name: g.name, args: g.args, kind: g.kind, revokePublic: true})
		}
		seen[key] = true
		if !strings.EqualFold(priv, "EXECUTE") {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func applyRoutineGrants(ctx context.Context, tgtDB execer, grants []routineGrantRow) error {
	for _, g := range grants {
		stmt := formatGrantRoutine(g.schema, g.name, g.args, g.kind, g.grantee)
		if g.revokePublic {
			stmt = fmt.Sprintf("REVOKE EXECUTE ON %s %s(%s) FROM PUBLIC", g.kind, quoteQualifiedType(g.schema, g.name), g.args)
		} else if g.grantable {
			stmt += " WITH GRANT OPTION"
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("replay execute privileges on %s.%s: %w", g.schema, g.name, err)
		}
	}
	return nil
}

type typeGrantRow struct {
	schema       string
	typeName     string
	grantee      string
	grantable    bool
	revokePublic bool
}

func loadTypeGrants(ctx context.Context, q *sql.DB, schemas []string) ([]typeGrantRow, error) {
	inClause, args := schemaINClause(schemas)
	// Effective ACL: NULL typacl is the built-in default (PUBLIC USAGE), not a revoke.
	// Standalone composites have a pg_class row with relkind 'c'; table row types do not.
	query := fmt.Sprintf(`
		SELECT n.nspname, t.typname, COALESCE(r.rolname, 'PUBLIC'),
		       COALESCE(priv.privilege_type, ''), COALESCE(priv.is_grantable, false),
		       NOT EXISTS (SELECT 1 FROM aclexplode(COALESCE(t.typacl, acldefault('T', t.typowner))) acl
		                   WHERE acl.grantee = 0 AND acl.privilege_type = 'USAGE')
		FROM pg_type t
		JOIN pg_namespace n ON n.oid = t.typnamespace
		LEFT JOIN LATERAL aclexplode(COALESCE(t.typacl, acldefault('T', t.typowner))) priv ON true
		LEFT JOIN pg_roles r ON r.oid = priv.grantee
		WHERE t.typtype IN ('e', 'd', 'c')
		  AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.reltype = t.oid AND c.relkind <> 'c')
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, t.typname, 3`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list type grants: %w", err)
	}
	defer rows.Close()

	var out []typeGrantRow
	seen := make(map[string]bool)
	for rows.Next() {
		var g typeGrantRow
		var priv string
		var missingPublic bool
		if err := rows.Scan(&g.schema, &g.typeName, &g.grantee, &priv, &g.grantable, &missingPublic); err != nil {
			return nil, fmt.Errorf("scan type grant: %w", err)
		}
		key := g.schema + "\x00" + g.typeName
		if missingPublic && !seen[key] {
			out = append(out, typeGrantRow{schema: g.schema, typeName: g.typeName, revokePublic: true})
		}
		seen[key] = true
		if !strings.EqualFold(priv, "USAGE") {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func applyTypeGrants(ctx context.Context, tgtDB execer, grants []typeGrantRow) error {
	for _, g := range grants {
		stmt := formatGrantType(g.schema, g.typeName, g.grantee)
		if g.revokePublic {
			stmt = fmt.Sprintf("REVOKE USAGE ON TYPE %s FROM PUBLIC", quoteQualifiedType(g.schema, g.typeName))
		} else if g.grantable {
			stmt += " WITH GRANT OPTION"
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("grant on type %s.%s: %w", g.schema, g.typeName, err)
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

func applyRLS(ctx context.Context, tgtDB execer, tables []rlsTable, policies []struct {
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
