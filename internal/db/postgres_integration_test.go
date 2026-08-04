//go:build integration

package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/testutil/pgintegration"
)

var integrationUniqueSchemaSeq uint64

var integrationDB *sql.DB

func TestMain(m *testing.M) {
	db, err := pgintegration.SetupMainDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres integration setup: %v\n", err)
		os.Exit(1)
	}
	integrationDB = db
	code := m.Run()
	if integrationDB != nil {
		_ = integrationDB.Close()
	}
	os.Exit(code)
}

func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	if integrationDB == nil {
		db := pgintegration.Open(t)
		pgintegration.ApplyFixtures(t, db)
		return db
	}
	pgintegration.ApplyFixtures(t, integrationDB)
	return integrationDB
}

func tableByName(tables []Table, name string) (Table, bool) {
	for _, tbl := range tables {
		if tbl.Name == name {
			return tbl, true
		}
	}
	return Table{}, false
}

func columnByName(cols []Column, name string) (Column, bool) {
	for _, col := range cols {
		if col.Name == name {
			return col, true
		}
	}
	return Column{}, false
}

func TestIntegrationLoadPostgresPublicSchemaFixtureTables(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()

	tables, err := LoadPostgresPublicSchema(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"departments", "tbl_a", "empty_audit"} {
		tbl, ok := tableByName(tables, name)
		if !ok {
			t.Fatalf("missing table %q", name)
		}
		if len(tbl.Columns) == 0 {
			t.Fatalf("table %q has no columns", name)
		}
	}

	dept, _ := tableByName(tables, "departments")
	idCol, ok := columnByName(dept.Columns, "id")
	if !ok || !idCol.PrimaryKey || idCol.OrdinalPosition != 1 {
		t.Fatalf("departments.id: want PK ordinal 1, got %+v", idCol)
	}

	emp, _ := tableByName(tables, "tbl_a")
	nick, ok := columnByName(emp.Columns, "nickname")
	if !ok || !nick.IsNullable {
		t.Fatalf("tbl_a.nickname should be nullable")
	}
}

func TestIntegrationLoadPostgresPublicSchemaForeignKeys(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()

	tables, err := LoadPostgresPublicSchema(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}

	emp, ok := tableByName(tables, "tbl_a")
	if !ok {
		t.Fatal("missing tbl_a")
	}
	if len(emp.ForeignKeys) == 0 {
		t.Fatal("expected foreign keys on tbl_a")
	}

	var found bool
	for _, fk := range emp.ForeignKeys {
		if fk.ReferencedTableName == "departments" && fk.ColumnName == "department_id" {
			found = true
			if fk.ConstraintName == "" {
				t.Fatal("expected constraint name on foreign key")
			}
			if fk.ReferencedTableSchema != "public" {
				t.Fatalf("referenced schema = %q, want public", fk.ReferencedTableSchema)
			}
		}
	}
	if !found {
		t.Fatal("tbl_a -> departments FK not found")
	}
}

func TestIntegrationLoadPostgresPublicSchemaStableOrder(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()

	first, err := LoadPostgresPublicSchema(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadPostgresPublicSchema(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}

	emp1, _ := tableByName(first, "tbl_a")
	emp2, _ := tableByName(second, "tbl_a")
	if len(emp1.ForeignKeys) != len(emp2.ForeignKeys) {
		t.Fatalf("FK count drift: %d vs %d", len(emp1.ForeignKeys), len(emp2.ForeignKeys))
	}
	for i := range emp1.ForeignKeys {
		if emp1.ForeignKeys[i].ConstraintName != emp2.ForeignKeys[i].ConstraintName {
			t.Fatalf("FK order drift at %d", i)
		}
	}

	for i := range emp1.Columns {
		if emp1.Columns[i].Name != emp2.Columns[i].Name {
			t.Fatalf("column order drift at %d", i)
		}
		if emp1.Columns[i].OrdinalPosition != emp2.Columns[i].OrdinalPosition {
			t.Fatalf("ordinal drift for %s", emp1.Columns[i].Name)
		}
	}
}

func TestIntegrationLoadPostgresPublicSchemaRowCountPolicy(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()

	tables, err := LoadPostgresPublicSchema(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}

	audit, ok := tableByName(tables, "empty_audit")
	if !ok {
		t.Fatal("missing empty_audit")
	}
	if audit.RowCount != nil && *audit.RowCount < 0 {
		t.Fatalf("negative row count: %d", *audit.RowCount)
	}

	for _, name := range []string{"departments", "tbl_a"} {
		tbl, _ := tableByName(tables, name)
		if tbl.RowCount != nil && *tbl.RowCount < 0 {
			t.Fatalf("%s: negative row count", name)
		}
	}
}

func TestIntegrationLoadPostgresPublicSchemaClosedConnection(t *testing.T) {
	conn := pgintegration.Open(t)
	pgintegration.ApplyFixtures(t, conn)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPostgresPublicSchema(context.Background(), conn)
	if err == nil {
		t.Fatal("expected error from closed connection")
	}
}

func integrationUniqueIndexSchema(t *testing.T) string {
	t.Helper()
	seq := atomic.AddUint64(&integrationUniqueSchemaSeq, 1)
	name := fmt.Sprintf("dolly_uix_%d_%d", os.Getpid(), seq)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func uniqueIndexByName(tbl Table, name string) (UniqueIndexInfo, bool) {
	for _, idx := range tbl.UniqueIndexes {
		if idx.IndexName == name {
			return idx, true
		}
	}
	return UniqueIndexInfo{}, false
}

func requireUniqueIndex(t *testing.T, tbl Table, name string) UniqueIndexInfo {
	t.Helper()
	idx, ok := uniqueIndexByName(tbl, name)
	if !ok {
		t.Fatalf("table %s: missing index %q (have %d)", tbl.Name, name, len(tbl.UniqueIndexes))
	}
	return idx
}

func assertKeyColumn(t *testing.T, col UniqueIndexColumn, pos int, name string, nullable bool) {
	t.Helper()
	if col.Position != pos || col.Name != name || col.IsNullable != nullable {
		t.Fatalf("key column: got pos=%d name=%q nullable=%v, want pos=%d name=%q nullable=%v",
			col.Position, col.Name, col.IsNullable, pos, name, nullable)
	}
	if col.Attnum <= 0 {
		t.Fatalf("key column %q: attnum %d", name, col.Attnum)
	}
	if col.OpclassOID == 0 {
		t.Fatalf("key column %q: zero opclass OID", name)
	}
}

func TestIntegrationLoadPostgresSchemasUniqueIndexDescriptorsPG16(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	schema := integrationUniqueIndexSchema(t)

	setup := fmt.Sprintf(`
		CREATE SCHEMA %s;
		CREATE TABLE %s.simple (id integer PRIMARY KEY, name text);
		CREATE TABLE %s.codes (id integer PRIMARY KEY, code text UNIQUE NOT NULL);
		CREATE TABLE %s.composite (a integer, b integer, PRIMARY KEY (a, b));
		CREATE TABLE %s.with_include (id integer, key_col text NOT NULL, extra_col text);
		CREATE UNIQUE INDEX uix_include ON %s.with_include (key_col) INCLUDE (extra_col);
		CREATE TABLE %s.nullable_uq (id integer PRIMARY KEY, opt text);
		CREATE UNIQUE INDEX uix_nullable ON %s.nullable_uq (opt);
		CREATE TABLE %s.partial (id integer PRIMARY KEY, tag text);
		CREATE UNIQUE INDEX uix_partial ON %s.partial (tag) WHERE tag IS NOT NULL;
		CREATE TABLE %s.expr (id integer PRIMARY KEY, name text);
		CREATE UNIQUE INDEX uix_expr ON %s.expr ((lower(name)));
		CREATE TABLE %s.mixed (id integer NOT NULL, name text);
		CREATE UNIQUE INDEX uix_mixed ON %s.mixed ((lower(name)), id);
		CREATE TABLE %s.meta (id integer PRIMARY KEY, label_a text, label_b text);
		CREATE UNIQUE INDEX uix_notready ON %s.meta (label_b);
		CREATE TABLE %s.exclude (id integer PRIMARY KEY, name text);
		CREATE INDEX uix_hash ON %s.exclude USING hash (name);
		CREATE INDEX uix_nonunique ON %s.exclude (name);
	`, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema, schema)

	if _, err := conn.ExecContext(ctx, setup); err != nil {
		t.Fatalf("setup unique-index catalog: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
	})

	if _, err := conn.ExecContext(ctx, fmt.Sprintf(
		`CREATE UNIQUE INDEX CONCURRENTLY uix_invalid ON %s.meta (label_a)`, schema)); err != nil {
		t.Fatalf("create concurrent index: %v", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`
		UPDATE pg_index SET indisvalid = false
		WHERE indexrelid = '%s.uix_invalid'::regclass
	`, schema)); err != nil {
		t.Fatalf("mark index invalid: %v", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`
		UPDATE pg_index SET indisready = false
		WHERE indexrelid = '%s.uix_notready'::regclass
	`, schema)); err != nil {
		t.Fatalf("mark index not ready: %v", err)
	}

	tables, err := LoadPostgresSchemas(ctx, conn, []string{schema})
	if err != nil {
		t.Fatal(err)
	}

	simple, ok := tableByName(tables, "simple")
	if !ok {
		t.Fatal("missing simple table")
	}
	pk := requireUniqueIndex(t, simple, "simple_pkey")
	if !pk.IsPrimary || !pk.IsValid || !pk.IsReady || pk.HasPredicate || pk.IsExpression {
		t.Fatalf("simple_pkey flags: %+v", pk)
	}
	if len(pk.KeyColumns) != 1 {
		t.Fatalf("simple_pkey columns: %+v", pk.KeyColumns)
	}
	assertKeyColumn(t, pk.KeyColumns[0], 1, "id", false)

	codes, ok := tableByName(tables, "codes")
	if !ok {
		t.Fatal("missing codes table")
	}
	uq := requireUniqueIndex(t, codes, "codes_code_key")
	if uq.IsPrimary || !uq.IsValid || !uq.IsReady {
		t.Fatalf("codes_code_key flags: %+v", uq)
	}
	assertKeyColumn(t, uq.KeyColumns[0], 1, "code", false)

	composite, ok := tableByName(tables, "composite")
	if !ok {
		t.Fatal("missing composite table")
	}
	compPK := requireUniqueIndex(t, composite, "composite_pkey")
	if len(compPK.KeyColumns) != 2 {
		t.Fatalf("composite_pkey columns: %+v", compPK.KeyColumns)
	}
	assertKeyColumn(t, compPK.KeyColumns[0], 1, "a", false)
	assertKeyColumn(t, compPK.KeyColumns[1], 2, "b", false)

	includeTbl, ok := tableByName(tables, "with_include")
	if !ok {
		t.Fatal("missing with_include table")
	}
	incl := requireUniqueIndex(t, includeTbl, "uix_include")
	if len(incl.KeyColumns) != 1 || incl.KeyColumns[0].Name != "key_col" {
		t.Fatalf("INCLUDE index key columns: %+v, want only key_col", incl.KeyColumns)
	}
	for _, col := range incl.KeyColumns {
		if col.Name == "extra_col" {
			t.Fatal("INCLUDE column extra_col must not appear in key columns")
		}
	}

	nullableTbl, ok := tableByName(tables, "nullable_uq")
	if !ok {
		t.Fatal("missing nullable_uq table")
	}
	nullIdx := requireUniqueIndex(t, nullableTbl, "uix_nullable")
	if len(nullIdx.KeyColumns) != 1 || !nullIdx.KeyColumns[0].IsNullable {
		t.Fatalf("nullable key: %+v", nullIdx.KeyColumns)
	}
	assertKeyColumn(t, nullIdx.KeyColumns[0], 1, "opt", true)

	partialTbl, ok := tableByName(tables, "partial")
	if !ok {
		t.Fatal("missing partial table")
	}
	partIdx := requireUniqueIndex(t, partialTbl, "uix_partial")
	if !partIdx.HasPredicate || partIdx.IsExpression {
		t.Fatalf("partial index flags: HasPredicate=%v IsExpression=%v", partIdx.HasPredicate, partIdx.IsExpression)
	}

	exprTbl, ok := tableByName(tables, "expr")
	if !ok {
		t.Fatal("missing expr table")
	}
	exprIdx := requireUniqueIndex(t, exprTbl, "uix_expr")
	if !exprIdx.IsExpression || len(exprIdx.KeyColumns) != 0 {
		t.Fatalf("pure expression index: IsExpression=%v columns=%+v", exprIdx.IsExpression, exprIdx.KeyColumns)
	}

	mixedTbl, ok := tableByName(tables, "mixed")
	if !ok {
		t.Fatal("missing mixed table")
	}
	mixedIdx := requireUniqueIndex(t, mixedTbl, "uix_mixed")
	if !mixedIdx.IsExpression || len(mixedIdx.KeyColumns) != 1 {
		t.Fatalf("mixed expression index: IsExpression=%v columns=%+v", mixedIdx.IsExpression, mixedIdx.KeyColumns)
	}
	assertKeyColumn(t, mixedIdx.KeyColumns[0], 2, "id", false)

	metaTbl, ok := tableByName(tables, "meta")
	if !ok {
		t.Fatal("missing meta table")
	}
	invalidIdx := requireUniqueIndex(t, metaTbl, "uix_invalid")
	if invalidIdx.IsValid {
		t.Fatal("invalid index reported as valid")
	}
	notReadyIdx := requireUniqueIndex(t, metaTbl, "uix_notready")
	if notReadyIdx.IsReady {
		t.Fatal("not-ready index reported as ready")
	}

	excludeTbl, ok := tableByName(tables, "exclude")
	if !ok {
		t.Fatal("missing exclude table")
	}
	for _, idx := range excludeTbl.UniqueIndexes {
		if idx.IndexName == "uix_hash" || idx.IndexName == "uix_nonunique" {
			t.Fatalf("non-unique index %q should be excluded", idx.IndexName)
		}
	}
}
