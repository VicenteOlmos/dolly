//go:build integration

package db

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/testutil/pgintegration"
)

var integrationDB *sql.DB

func TestMain(m *testing.M) {
	db, err := pgintegration.SetupMainDB()
	if err != nil {
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
