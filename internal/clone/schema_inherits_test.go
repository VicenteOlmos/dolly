package clone

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFilterInheritedTableColumnsKeepsLocalDefault(t *testing.T) {
	t.Parallel()
	key := "app.child"
	cols := map[string][]schemaColumn{
		key: {
			{name: "id", sqlType: "integer", defaultExpr: sql.NullString{String: "2", Valid: true}},
		},
	}
	classic := map[string][]inheritParent{
		key: {{schema: "app", name: "parent"}},
	}
	// attislocal columns are omitted from loadInheritedColumnNames, so merged id stays.
	inherited := map[string]map[string]bool{key: {}}
	filterInheritedTableColumns(cols, classic, inherited)
	got := cols[key]
	if len(got) != 1 {
		t.Fatalf("columns = %+v, want local id with default", got)
	}
	if got[0].name != "id" || !got[0].defaultExpr.Valid || got[0].defaultExpr.String != "2" {
		t.Fatalf("local id default lost: %+v", got[0])
	}
}

func TestFilterInheritedTableColumnsUsesInheritedMapOnly(t *testing.T) {
	t.Parallel()
	key := "app.child"
	cols := map[string][]schemaColumn{
		key: {
			{name: "id", sqlType: "integer"},
		},
	}
	classic := map[string][]inheritParent{key: {{schema: "app", name: "parent"}}}
	inherited := map[string]map[string]bool{key: {"id": true}}
	filterInheritedTableColumns(cols, classic, inherited)
	if len(cols[key]) != 0 {
		t.Fatalf("inherited-only id should be filtered: %+v", cols[key])
	}
}

func TestLoadClassicInheritsPreservesInhseqnoOrder(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`FROM pg_inherits`).WillReturnRows(
		sqlmock.NewRows([]string{"child_schema", "child_name", "parent_schema", "parent_name"}).
			AddRow("app", "merged", "app", "first_parent").
			AddRow("app", "merged", "app", "second_parent"),
	)
	got, err := loadClassicInherits(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	parents := got["app.merged"]
	if len(parents) != 2 {
		t.Fatalf("parents = %+v", parents)
	}
	if parents[0].name != "first_parent" || parents[1].name != "second_parent" {
		t.Fatalf("inhseqno order not preserved: %+v", parents)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
