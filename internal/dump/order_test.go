package dump

import (
	"reflect"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestSortTables(t *testing.T) {
	tests := []struct {
		name  string
		input []db.Table
		want  []string
	}{
		{
			name: "linear chain",
			input: []db.Table{
				{Schema: "public", Name: "c", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "b"}}},
				{Schema: "public", Name: "a", ForeignKeys: nil},
				{Schema: "public", Name: "b", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "a"}}},
			},
			want: []string{"public.a", "public.b", "public.c"},
		},
		{
			name: "diamond dependency",
			input: []db.Table{
				{Schema: "public", Name: "d", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "b"}, {ReferencedTableSchema: "public", ReferencedTableName: "c"}}},
				{Schema: "public", Name: "b", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "a"}}},
				{Schema: "public", Name: "c", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "a"}}},
				{Schema: "public", Name: "a", ForeignKeys: nil},
			},
			want: []string{"public.a", "public.b", "public.c", "public.d"},
		},
		{
			name: "no fks stable name order",
			input: []db.Table{
				{Schema: "public", Name: "z"},
				{Schema: "public", Name: "a"},
				{Schema: "public", Name: "m"},
			},
			want: []string{"public.a", "public.m", "public.z"},
		},
		{
			name: "cyclic fks",
			input: []db.Table{
				{Schema: "public", Name: "a", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "b"}}},
				{Schema: "public", Name: "b", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "a"}}},
			},
			want: []string{"public.a", "public.b"},
		},
		{
			name: "single table",
			input: []db.Table{
				{Schema: "public", Name: "only"},
			},
			want: []string{"public.only"},
		},
		{
			name: "cross-schema fk ordering",
			input: []db.Table{
				{Schema: "app", Name: "orders", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "users"}}},
				{Schema: "public", Name: "users", ForeignKeys: nil},
			},
			want: []string{"public.users", "app.orders"},
		},
		{
			name: "same name different schema no collision",
			input: []db.Table{
				{Schema: "app", Name: "users"},
				{Schema: "public", Name: "users", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "app", ReferencedTableName: "users"}}},
			},
			want: []string{"app.users", "public.users"},
		},
		{
			name: "cross-schema fk missing parent skipped",
			input: []db.Table{
				{Schema: "app", Name: "orders", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "other", ReferencedTableName: "users"}}},
			},
			want: []string{"app.orders"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SortTables(tt.input)
			var gotNames []string
			for _, table := range got {
				gotNames = append(gotNames, qualifiedName(table.Schema, table.Name))
			}
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Fatalf("got %v, want %v", gotNames, tt.want)
			}
		})
	}
}
