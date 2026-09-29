package db

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAnnotatePartitionsAndGeneratedColumns(t *testing.T) {
	prev := SkipRelationAnnotations
	SkipRelationAnnotations = false
	t.Cleanup(func() { SkipRelationAnnotations = prev })

	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	mock.ExpectQuery(`SELECT t\.table_schema`).
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
			AddRow("public", "events", int64(0)).
			AddRow("public", "events_2024", int64(2)))
	mock.ExpectQuery(`SELECT c\.table_schema`).
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
			AddRow("public", "events", "id", "integer", "NO", 1, true).
			AddRow("public", "events", "total", "integer", "YES", 2, false).
			AddRow("public", "events_2024", "id", "integer", "NO", 1, true).
			AddRow("public", "events_2024", "total", "integer", "YES", 2, false))
	mock.ExpectQuery(`SELECT tc\.table_schema`).
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}))
	emptyUniqueIndexMock(mock)
	mock.ExpectQuery(`pg_get_partkeydef`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "relkind", "relispartition", "relpersistence", "partkey", "bound", "parent_schema", "parent_name"}).
			AddRow("public", "events", "p", false, "p", "RANGE (id)", "", "", "").
			AddRow("public", "events_2024", "r", true, "p", "", "FOR VALUES FROM (1) TO (2)", "public", "events"))
	mock.ExpectQuery(`is_generated = 'ALWAYS'`).
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).
			AddRow("public", "events_2024", "total"))

	got, err := LoadPostgresSchemas(context.Background(), conn, []string{"public"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("tables = %d", len(got))
	}
	parent, child := got[0], got[1]
	if parent.RelKind != "p" || parent.PartitionBy != "RANGE (id)" || parent.PartitionOf != "" {
		t.Fatalf("parent = %+v", parent)
	}
	if child.PartitionOf != "public.events" || child.PartitionBound != "FOR VALUES FROM (1) TO (2)" {
		t.Fatalf("child partition = %+v", child)
	}
	if child.Columns[1].Name != "total" || !child.Columns[1].Generated {
		t.Fatalf("generated column = %+v", child.Columns)
	}
	if parent.Columns[1].Generated {
		t.Fatal("parent total should stay writable")
	}
	leaves := WithoutPartitionParents(got)
	if len(leaves) != 1 || leaves[0].Name != "events_2024" {
		t.Fatalf("leaves = %+v", leaves)
	}
	data := DataColumns(child.Columns)
	if len(data) != 1 || data[0].Name != "id" || HasGenerated(data) {
		t.Fatalf("data columns = %+v", data)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWithoutPartitionParentsKeepsOrdinaryTables(t *testing.T) {
	tables := []Table{{Schema: "public", Name: "users", RelKind: "r"}}
	got := WithoutPartitionParents(tables)
	if len(got) != 1 || got[0].Name != "users" {
		t.Fatalf("got %+v", got)
	}
}
