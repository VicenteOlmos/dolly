package clone

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func expectForeignTableMetaRows(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery(`pg_foreign_table`).WillReturnRows(rows)
}

func expectForeignTableColumnRows(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery(`attfdwoptions`).WillReturnRows(rows)
}

func TestLoadForeignTablesRejectsMissingServer(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	expectForeignTableMetaRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value", "relispartition", "bound", "parent_schema", "parent_name"}).
			AddRow("app", "remote_data", nil, nil, nil, false, "", "", ""))
	expectForeignTableColumnRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "sql_type", "nullable", "option_name", "option_value"}).
			AddRow("app", "remote_data", "id", "integer", true, nil, nil))

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

	expectForeignTableMetaRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value", "relispartition", "bound", "parent_schema", "parent_name"}).
			AddRow("app", "remote_data", "srv", nil, nil, false, "", "", ""))
	expectForeignTableColumnRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "sql_type", "nullable", "option_name", "option_value"}).
			AddRow("app", "remote_data", "id", "integer", false, nil, nil).
			AddRow("app", "remote_data", "note", "text", true, nil, nil))

	tables, err := loadForeignTables(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("tables = %+v", tables)
	}
	ft := tables[0]
	got := formatCreateForeignTable(ft)
	want := `CREATE FOREIGN TABLE "app"."remote_data" ("id" integer NOT NULL, "note" text) SERVER "srv"`
	if got != want {
		t.Fatalf("ddl = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadForeignTablesPreservesColumnFDWOptions(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	expectForeignTableMetaRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value", "relispartition", "bound", "parent_schema", "parent_name"}).
			AddRow("app", "remote_data", "srv", nil, nil, false, "", "", ""))
	expectForeignTableColumnRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "sql_type", "nullable", "option_name", "option_value"}).
			AddRow("app", "remote_data", "local_id", "integer", true, "column_name", "remote_id"))

	tables, err := loadForeignTables(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	got := formatCreateForeignTable(tables[0])
	want := `CREATE FOREIGN TABLE "app"."remote_data" ("local_id" integer OPTIONS (column_name 'remote_id')) SERVER "srv"`
	if got != want {
		t.Fatalf("ddl = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadForeignTablesForeignPartition(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	expectForeignTableMetaRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value", "relispartition", "bound", "parent_schema", "parent_name"}).
			AddRow("app", "f_part", "srv", nil, nil, true, "FOR VALUES FROM (0) TO (10)", "app", "parent"))
	expectForeignTableColumnRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "sql_type", "nullable", "option_name", "option_value"}).
			AddRow("app", "f_part", "id", "integer", true, nil, nil))

	tables, err := loadForeignTables(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	got := formatCreateForeignTable(tables[0])
	want := `CREATE FOREIGN TABLE "app"."f_part" PARTITION OF "app"."parent" FOR VALUES FROM (0) TO (10) SERVER "srv"`
	if got != want {
		t.Fatalf("ddl = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFormatCreateForeignPartitionColumnOptions(t *testing.T) {
	t.Parallel()
	cols := []foreignTableColumn{
		{name: "id", sqlType: "integer", nullable: false, options: map[string]string{"column_name": "remote_id"}},
		{name: "note", sqlType: "text", nullable: true},
	}
	got := formatCreateForeignTableParts("app", "f_part", "srv", cols, map[string]string{"schema_name": "public"}, "app", "parent", "FOR VALUES FROM (0) TO (10)")
	want := `CREATE FOREIGN TABLE "app"."f_part" PARTITION OF "app"."parent" ("id" WITH OPTIONS (column_name 'remote_id') NOT NULL) FOR VALUES FROM (0) TO (10) SERVER "srv" OPTIONS (schema_name 'public')`
	if got != want {
		t.Fatalf("ddl = %q, want %q", got, want)
	}
}

func TestLoadForeignTablesQualifiesUserDefinedColumnType(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	expectForeignTableMetaRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value", "relispartition", "bound", "parent_schema", "parent_name"}).
			AddRow("app", "remote_data", "srv", nil, nil, false, "", "", ""))
	expectForeignTableColumnRows(mock,
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "sql_type", "nullable", "option_name", "option_value"}).
			AddRow("app", "remote_data", "m", `"app"."mood"`, true, nil, nil))

	tables, err := loadForeignTables(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	got := formatCreateForeignTable(tables[0])
	if !strings.Contains(got, `"app"."mood"`) {
		t.Fatalf("ddl = %q, want schema-qualified mood type", got)
	}
	if strings.Contains(got, " mood") && !strings.Contains(got, `"app"."mood"`) {
		t.Fatalf("ddl = %q, must not use bare mood type name", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFormatCreateForeignTableQualifiedUDTType(t *testing.T) {
	t.Parallel()
	cols := []foreignTableColumn{
		{name: "m", sqlType: `"app"."mood"`, nullable: true},
	}
	got := formatCreateForeignTableParts("app", "remote_data", "srv", cols, nil, "", "", "")
	if !strings.Contains(got, `"app"."mood"`) {
		t.Fatalf("ddl = %q", got)
	}
}
