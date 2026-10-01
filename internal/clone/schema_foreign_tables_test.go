package clone

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadForeignTablesRejectsMissingServer(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	mock.ExpectQuery(`pg_foreign_table`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value"}).
			AddRow("app", "remote_data", nil, nil, nil))
	mock.ExpectQuery(`format_type\(a\.atttypid, a\.atttypmod\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "format_type", "attnotnull"}).
			AddRow("app", "remote_data", "id", "integer", true))

	_, err = loadForeignTables(context.Background(), src, []string{"app"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"app"."remote_data"`) || !strings.Contains(err.Error(), "foreign server") {
		t.Fatalf("error = %q", err.Error())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadForeignTablesPreservesColumnNullability(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	mock.ExpectQuery(`pg_foreign_table`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value"}).
			AddRow("app", "remote_data", "srv", nil, nil))
	mock.ExpectQuery(`format_type\(a\.atttypid, a\.atttypmod\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "format_type", "nullable"}).
			AddRow("app", "remote_data", "id", "integer", false).
			AddRow("app", "remote_data", "note", "text", true))

	tables, err := loadForeignTables(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("tables = %+v", tables)
	}
	ft := tables[0]
	got := formatCreateForeignTable(ft.schema, ft.name, ft.server, ft.columns, ft.options)
	want := `CREATE FOREIGN TABLE "app"."remote_data" ("id" integer NOT NULL, "note" text) SERVER "srv"`
	if got != want {
		t.Fatalf("ddl = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
