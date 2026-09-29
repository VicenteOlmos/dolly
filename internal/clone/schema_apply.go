package clone

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

// execer is the target side of catalog replay. *sql.DB satisfies it, and a
// script recorder does too so schema.sql can be written without pg_dump.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type schemaColumn struct {
	name            string
	sqlType         string
	nullable        bool
	defaultExpr     sql.NullString
	ordinalPosition int
	generatedExpr   string
}

type uniqueConstraint struct {
	name    string
	columns []string
}

// ApplySchemasFromSource creates selected schemas and database objects on target to match
// source introspection, without invoking pg_dump or psql subprocesses.
//
// Ordering mirrors pg_dump --schema-only where practical: extensions, types, sequences,
// functions, tables (with inline checks), foreign keys, indexes, views (dependency
// order), triggers, rules, comments, grants, and RLS.
//
// Limitations (prefer pg_dump when it is on PATH):
//   - Ordered-set and hypothetical aggregates, exclusion constraints, and operator classes are not replayed.
//   - Functions, triggers, and rules that belong to extensions are skipped.
//   - Indexes that exist only on a partition (not the parent) are omitted.
func ApplySchemasFromSource(ctx context.Context, srcDB, tgtDB *sql.DB, schemas []string) error {
	return applySchemas(ctx, srcDB, tgtDB, schemas, true)
}

// RenderCatalogSchema replays the catalog into a SQL script. Privileges are
// included only when includePrivileges is true. schema.sql capture passes false,
// matching pg_dump --no-acl.
func RenderCatalogSchema(ctx context.Context, srcDB *sql.DB, schemas []string, includePrivileges bool) (string, error) {
	rec := &scriptExec{}
	if err := applySchemas(ctx, srcDB, rec, schemas, includePrivileges); err != nil {
		return "", err
	}
	return rec.String(), nil
}

type scriptExec struct {
	b strings.Builder
}

func (s *scriptExec) String() string { return s.b.String() }

func (s *scriptExec) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("catalog schema script cannot bind parameters")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return scriptResult{}, nil
	}
	if !strings.HasSuffix(q, ";") {
		q += ";"
	}
	s.b.WriteString(q)
	s.b.WriteByte('\n')
	return scriptResult{}, nil
}

type scriptResult struct{}

func (scriptResult) LastInsertId() (int64, error) { return 0, nil }
func (scriptResult) RowsAffected() (int64, error) { return 0, nil }

func applySchemas(ctx context.Context, srcDB *sql.DB, tgtDB execer, schemas []string, includePrivileges bool) error {
	if len(schemas) == 0 {
		return fmt.Errorf("schemas are required")
	}

	for _, schema := range schemas {
		stmt := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quoteIdentifier(schema))
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create schema %q: %w", schema, err)
		}
	}

	if err := applyExtensions(ctx, srcDB, tgtDB); err != nil {
		return err
	}

	enums, err := loadEnumTypes(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applyEnumTypes(ctx, tgtDB, enums); err != nil {
		return err
	}

	domains, err := loadDomainTypes(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applyDomainTypes(ctx, tgtDB, domains); err != nil {
		return err
	}

	composites, err := loadCompositeTypes(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applyCompositeTypes(ctx, tgtDB, composites); err != nil {
		return err
	}

	seqs, err := loadSequences(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applySequences(ctx, tgtDB, seqs); err != nil {
		return err
	}

	routines, aggregates, err := loadRoutines(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	routineDeps, err := loadRoutineDeps(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	routines, err = orderRoutines(routines, routineDeps)
	if err != nil {
		return err
	}
	routineDefs := make([]string, len(routines))
	for i, routine := range routines {
		routineDefs[i] = routine.def
	}
	if err := applySQLDefs(ctx, tgtDB, routineDefs, "function"); err != nil {
		return err
	}
	if err := applySQLDefs(ctx, tgtDB, aggregates, "aggregate"); err != nil {
		return err
	}

	tables, err := db.LoadPostgresSchemasBatched(ctx, srcDB, schemas)
	if err != nil {
		return fmt.Errorf("load source schema: %w", err)
	}
	sorted := orderPartitionParentsFirst(dump.SortTables(tables))

	// Batch all per-table queries (4 queries total, not 4N).
	// Only run when there are tables to avoid unnecessary queries.
	if len(sorted) > 0 {
		schemaCols, err := loadAllSchemaColumns(ctx, srcDB, schemas)
		if err != nil {
			return err
		}
		if err := mergeGeneratedExpressions(ctx, srcDB, schemas, schemaCols); err != nil {
			return err
		}
		uniqueMap, err := loadAllUniqueConstraints(ctx, srcDB, schemas)
		if err != nil {
			return err
		}
		checkMap, err := loadAllCheckConstraints(ctx, srcDB, schemas)
		if err != nil {
			return err
		}
		fkMap, err := loadAllForeignKeyConstraints(ctx, srcDB, schemas)
		if err != nil {
			return err
		}

		for _, table := range sorted {
			key := table.Schema + "." + table.Name
			cols := schemaCols[key]
			if err := createTable(ctx, tgtDB, table, cols, uniqueMap[key], checkMap[key]); err != nil {
				return err
			}
		}

		for _, table := range sorted {
			key := table.Schema + "." + table.Name
			if err := applyForeignKeyConstraints(ctx, tgtDB, table.Schema, table.Name, fkMap[key]); err != nil {
				return err
			}
		}
	} // end if len(sorted) > 0

	indexes, err := loadIndexes(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applyIndexes(ctx, tgtDB, indexesForReplay(indexes, sorted)); err != nil {
		return err
	}

	views, err := loadViews(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	viewDeps, err := loadViewDeps(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	views, err = orderViews(views, viewDeps)
	if err != nil {
		return err
	}
	if err := applyViews(ctx, tgtDB, views); err != nil {
		return err
	}

	triggers, err := loadTriggers(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applySQLDefs(ctx, tgtDB, triggers, "trigger"); err != nil {
		return err
	}
	rules, err := loadRules(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applySQLDefs(ctx, tgtDB, rules, "rule"); err != nil {
		return err
	}

	comments, err := loadComments(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applyComments(ctx, tgtDB, comments); err != nil {
		return err
	}

	if includePrivileges {
		grants, err := loadGrants(ctx, srcDB, schemas)
		if err != nil {
			return err
		}
		if err := applyGrants(ctx, tgtDB, grants); err != nil {
			return err
		}
	}

	rlsTables, err := loadRLSTables(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	policies, err := loadPolicies(ctx, srcDB, schemas)
	if err != nil {
		return err
	}
	if err := applyRLS(ctx, tgtDB, rlsTables, policies); err != nil {
		return err
	}

	return nil
}

func loadSchemaColumns(ctx context.Context, q *sql.DB, schema, table string) ([]schemaColumn, error) {
	const query = `
		SELECT column_name, data_type, is_nullable, column_default, ordinal_position,
		       character_maximum_length, numeric_precision, numeric_scale, udt_name, udt_schema
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position;
	`
	rows, err := q.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, fmt.Errorf("load columns for %q.%q: %w", schema, table, err)
	}
	defer rows.Close()

	var cols []schemaColumn
	for rows.Next() {
		var c schemaColumn
		var nullable string
		var charMax, numPrec, numScale sql.NullInt64
		var udtName, udtSchema sql.NullString
		if err := rows.Scan(
			&c.name, &c.sqlType, &nullable, &c.defaultExpr, &c.ordinalPosition,
			&charMax, &numPrec, &numScale, &udtName, &udtSchema,
		); err != nil {
			return nil, fmt.Errorf("scan column for %q.%q: %w", schema, table, err)
		}
		c.nullable = nullable == "YES"
		c.sqlType = columnSQLType(c.sqlType, charMax, numPrec, numScale, udtName, udtSchema)
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load columns for %q.%q: %w", schema, table, err)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %q.%q has no columns", schema, table)
	}
	return cols, nil
}

// loadAllSchemaColumns loads columns for all tables in the given schemas in a
// single query. Returns a map keyed by "schema.table".
func loadAllSchemaColumns(ctx context.Context, q *sql.DB, schemas []string) (map[string][]schemaColumn, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT table_schema, table_name, column_name, data_type, is_nullable,
		       column_default, ordinal_position,
		       character_maximum_length, numeric_precision, numeric_scale,
		       udt_name, udt_schema
		FROM information_schema.columns
		WHERE table_schema IN (%s)
		ORDER BY table_schema, table_name, ordinal_position;
	`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load all columns: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]schemaColumn)
	for rows.Next() {
		var schema, table string
		var c schemaColumn
		var nullable string
		var charMax, numPrec, numScale sql.NullInt64
		var udtName, udtSchema sql.NullString
		if err := rows.Scan(
			&schema, &table, &c.name, &c.sqlType, &nullable, &c.defaultExpr, &c.ordinalPosition,
			&charMax, &numPrec, &numScale, &udtName, &udtSchema,
		); err != nil {
			return nil, fmt.Errorf("scan all columns: %w", err)
		}
		c.nullable = nullable == "YES"
		c.sqlType = columnSQLType(c.sqlType, charMax, numPrec, numScale, udtName, udtSchema)
		key := schema + "." + table
		out[key] = append(out[key], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load all columns: %w", err)
	}
	return out, nil
}

// loadAllUniqueConstraints loads UNIQUE constraints for all tables in the given
// schemas in a single query. Returns a map keyed by "schema.table".
func loadAllUniqueConstraints(ctx context.Context, q *sql.DB, schemas []string) (map[string][]uniqueConstraint, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT tc.table_schema, tc.table_name, tc.constraint_name,
		       kcu.column_name, kcu.ordinal_position
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		  AND tc.table_schema = kcu.table_schema
		  AND tc.table_name = kcu.table_name
		WHERE tc.constraint_type = 'UNIQUE'
		  AND tc.table_schema IN (%s)
		ORDER BY tc.table_schema, tc.table_name, tc.constraint_name, kcu.ordinal_position;
	`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load all unique constraints: %w", err)
	}
	defer rows.Close()

	// ponytail: rows ordered by constraint_name, so columns per constraint are contiguous.
	// Track the last constraint name to detect boundaries.
	type trackedKey struct {
		key     string
		conName string
	}
	out := make(map[string][]uniqueConstraint)
	var last trackedKey
	var cur *uniqueConstraint
	for rows.Next() {
		var schema, table, conName, colName string
		var ord int
		if err := rows.Scan(&schema, &table, &conName, &colName, &ord); err != nil {
			return nil, fmt.Errorf("scan unique: %w", err)
		}
		key := schema + "." + table
		if cur == nil || last.key != key || last.conName != conName {
			cur = &uniqueConstraint{name: conName}
			out[key] = append(out[key], *cur)
			cur = &out[key][len(out[key])-1]
		}
		cur.columns = append(cur.columns, colName)
		last = trackedKey{key, conName}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load all unique constraints: %w", err)
	}
	return out, nil
}

// loadAllCheckConstraints loads CHECK constraints for all tables in the given
// schemas in a single query. Returns a map keyed by "schema.table".
func loadAllCheckConstraints(ctx context.Context, q *sql.DB, schemas []string) (map[string][]checkConstraint, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, con.conname, pg_get_constraintdef(con.oid, true)
		FROM pg_constraint con
		INNER JOIN pg_class c ON c.oid = con.conrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE con.contype = 'c'
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, con.conname;
	`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load all check constraints: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]checkConstraint)
	for rows.Next() {
		var schema, table string
		var cc checkConstraint
		if err := rows.Scan(&schema, &table, &cc.name, &cc.def); err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		key := schema + "." + table
		out[key] = append(out[key], cc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load all check constraints: %w", err)
	}
	return out, nil
}

// loadAllForeignKeyConstraints loads FK constraints for all tables in the given
// schemas in a single query. Returns a map keyed by "schema.table".
func loadAllForeignKeyConstraints(ctx context.Context, q *sql.DB, schemas []string) (map[string][]foreignKeyConstraint, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, con.conname, pg_get_constraintdef(con.oid, true)
		FROM pg_constraint con
		INNER JOIN pg_class c ON c.oid = con.conrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE con.contype = 'f'
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, con.conname;
	`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load all foreign keys: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]foreignKeyConstraint)
	for rows.Next() {
		var schema, table string
		var fk foreignKeyConstraint
		if err := rows.Scan(&schema, &table, &fk.name, &fk.def); err != nil {
			return nil, fmt.Errorf("scan fk: %w", err)
		}
		key := schema + "." + table
		out[key] = append(out[key], fk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load all foreign keys: %w", err)
	}
	return out, nil
}

func columnSQLType(dataType string, charMax, numPrec, numScale sql.NullInt64, udtName, udtSchema sql.NullString) string {
	switch dataType {
	case "character varying", "character":
		if charMax.Valid {
			return fmt.Sprintf("%s(%d)", dataType, charMax.Int64)
		}
	case "numeric":
		if numPrec.Valid {
			if numScale.Valid {
				return fmt.Sprintf("numeric(%d,%d)", numPrec.Int64, numScale.Int64)
			}
			return fmt.Sprintf("numeric(%d)", numPrec.Int64)
		}
	case "USER-DEFINED":
		if udtName.Valid && udtName.String != "" {
			if udtSchema.Valid && udtSchema.String != "" && udtSchema.String != "pg_catalog" {
				return quoteQualifiedType(udtSchema.String, udtName.String)
			}
			return quoteIdentifier(udtName.String)
		}
	}
	return dataType
}

func quoteQualifiedType(schema, name string) string {
	return quoteIdentifier(schema) + "." + quoteIdentifier(name)
}

func loadUniqueConstraints(ctx context.Context, q *sql.DB, schema, table string) ([]uniqueConstraint, error) {
	const query = `
		SELECT tc.constraint_name, kcu.column_name, kcu.ordinal_position
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		  AND tc.table_schema = kcu.table_schema
		  AND tc.table_name = kcu.table_name
		WHERE tc.constraint_type = 'UNIQUE'
		  AND tc.table_schema = $1 AND tc.table_name = $2
		ORDER BY tc.constraint_name, kcu.ordinal_position;
	`
	rows, err := q.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, fmt.Errorf("load unique constraints for %q.%q: %w", schema, table, err)
	}
	defer rows.Close()

	byName := make(map[string][]string)
	var order []string
	for rows.Next() {
		var name, col string
		var ord int
		if err := rows.Scan(&name, &col, &ord); err != nil {
			return nil, fmt.Errorf("scan unique constraint for %q.%q: %w", schema, table, err)
		}
		if _, ok := byName[name]; !ok {
			order = append(order, name)
		}
		byName[name] = append(byName[name], col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load unique constraints for %q.%q: %w", schema, table, err)
	}
	out := make([]uniqueConstraint, 0, len(order))
	for _, name := range order {
		out = append(out, uniqueConstraint{name: name, columns: byName[name]})
	}
	return out, nil
}

func createTable(ctx context.Context, tgtDB execer, table db.Table, cols []schemaColumn, uniques []uniqueConstraint, checks []checkConstraint) error {
	stmt, err := formatCreateTable(table, cols, uniques, checks)
	if err != nil {
		return err
	}
	if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("create table %s: %w", quoteQualifiedTable(table.Schema, table.Name), err)
	}
	return nil
}

func formatCreateTable(table db.Table, cols []schemaColumn, uniques []uniqueConstraint, checks []checkConstraint) (string, error) {
	qual := quoteQualifiedTable(table.Schema, table.Name)
	if table.PartitionOf != "" {
		parentSchema, parentName, ok := splitQualified(table.PartitionOf)
		if !ok {
			return "", fmt.Errorf("partition %s has invalid parent %q", qual, table.PartitionOf)
		}
		bound := strings.TrimSpace(table.PartitionBound)
		if bound == "" {
			return "", fmt.Errorf("partition %s has no partition bound", qual)
		}
		return fmt.Sprintf("CREATE TABLE %s PARTITION OF %s %s", qual, quoteQualifiedTable(parentSchema, parentName), bound), nil
	}

	var parts []string
	for _, c := range cols {
		var part string
		if c.generatedExpr != "" {
			part = fmt.Sprintf("%s %s GENERATED ALWAYS AS (%s) STORED", quoteIdentifier(c.name), c.sqlType, c.generatedExpr)
		} else {
			part = fmt.Sprintf("%s %s", quoteIdentifier(c.name), c.sqlType)
			if c.defaultExpr.Valid && c.defaultExpr.String != "" {
				part += " DEFAULT " + c.defaultExpr.String
			}
		}
		if !c.nullable {
			part += " NOT NULL"
		}
		parts = append(parts, part)
	}

	pkCols := primaryKeyColumnNames(table.Columns)
	if len(pkCols) > 0 {
		quoted := make([]string, len(pkCols))
		for i, name := range pkCols {
			quoted[i] = quoteIdentifier(name)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quoted, ", ")))
	}

	for _, uq := range uniques {
		if len(uq.columns) == 0 {
			continue
		}
		quoted := make([]string, len(uq.columns))
		for i, name := range uq.columns {
			quoted[i] = quoteIdentifier(name)
		}
		parts = append(parts, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)", quoteIdentifier(uq.name), strings.Join(quoted, ", ")))
	}

	for _, chk := range checks {
		parts = append(parts, formatTableCheckConstraint(chk.name, chk.def))
	}

	stmt := fmt.Sprintf("CREATE TABLE %s (%s)", qual, strings.Join(parts, ", "))
	if table.RelKind == "p" {
		partBy := strings.TrimSpace(table.PartitionBy)
		if partBy == "" {
			return "", fmt.Errorf("partitioned table %s has no partition key", qual)
		}
		stmt += " PARTITION BY " + partBy
	}
	return stmt, nil
}

func splitQualified(name string) (string, string, bool) {
	schema, table, ok := strings.Cut(name, ".")
	if !ok || schema == "" || table == "" {
		return "", "", false
	}
	return schema, table, true
}

func mergeGeneratedExpressions(ctx context.Context, q *sql.DB, schemas []string, cols map[string][]schemaColumn) error {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT table_schema, table_name, column_name, generation_expression
		FROM information_schema.columns
		WHERE is_generated = 'ALWAYS'
		  AND table_schema IN (%s)
	`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("load generated expressions: %w", err)
	}
	defer rows.Close()
	type exprKey struct{ table, column string }
	exprs := map[exprKey]string{}
	for rows.Next() {
		var schema, table, column string
		var expr sql.NullString
		if err := rows.Scan(&schema, &table, &column, &expr); err != nil {
			return fmt.Errorf("load generated expressions: %w", err)
		}
		if !expr.Valid || strings.TrimSpace(expr.String) == "" {
			continue
		}
		exprs[exprKey{schema + "." + table, column}] = expr.String
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("load generated expressions: %w", err)
	}
	for key, list := range cols {
		for i := range list {
			if expr, ok := exprs[exprKey{key, list[i].name}]; ok {
				list[i].generatedExpr = expr
			}
		}
		cols[key] = list
	}
	return nil
}

func orderPartitionParentsFirst(tables []db.Table) []db.Table {
	if len(tables) < 2 {
		return tables
	}
	byName := make(map[string]db.Table, len(tables))
	orig := make(map[string]int, len(tables))
	for i, table := range tables {
		key := table.Schema + "." + table.Name
		byName[key] = table
		orig[key] = i
	}
	depth := make(map[string]int, len(tables))
	var walk func(key string, stack map[string]bool) int
	walk = func(key string, stack map[string]bool) int {
		if d, ok := depth[key]; ok {
			return d
		}
		table, ok := byName[key]
		if !ok || table.PartitionOf == "" || stack[key] {
			depth[key] = 0
			return 0
		}
		stack[key] = true
		d := walk(table.PartitionOf, stack) + 1
		delete(stack, key)
		depth[key] = d
		return d
	}
	order := append([]db.Table(nil), tables...)
	for _, table := range order {
		walk(table.Schema+"."+table.Name, map[string]bool{})
	}
	sort.SliceStable(order, func(i, j int) bool {
		di := depth[order[i].Schema+"."+order[i].Name]
		dj := depth[order[j].Schema+"."+order[j].Name]
		if di != dj {
			return di < dj
		}
		return orig[order[i].Schema+"."+order[i].Name] < orig[order[j].Schema+"."+order[j].Name]
	})
	return order
}

func indexesForReplay(indexes []indexRow, tables []db.Table) []indexRow {
	skip := map[string]bool{}
	for _, table := range tables {
		if table.PartitionOf != "" {
			skip[table.Schema+"."+table.Name] = true
		}
	}
	if len(skip) == 0 {
		return indexes
	}
	out := make([]indexRow, 0, len(indexes))
	for _, idx := range indexes {
		if skip[idx.schema+"."+idx.table] {
			continue
		}
		out = append(out, idx)
	}
	return out
}

func primaryKeyColumnNames(cols []db.Column) []string {
	var out []string
	for _, c := range cols {
		if c.PrimaryKey {
			out = append(out, c.Name)
		}
	}
	return out
}
