package restore

import (
	"errors"
	"reflect"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestBuildRestoreLevels_parentBeforeChild(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "child", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "parent"}}},
		{Schema: "public", Name: "parent"},
	}
	levels, err := BuildRestoreLevels(tables)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"public.parent"}, {"public.child"}}
	if got := levelNames(levels); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildRestoreLevels_independentConcurrencyLevels(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "orders", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "users"}}},
		{Schema: "public", Name: "posts", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "users"}}},
		{Schema: "public", Name: "users"},
	}
	levels, err := BuildRestoreLevels(tables)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"public.users"}, {"public.orders", "public.posts"}}
	if got := levelNames(levels); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildRestoreLevels_multiSchemaSameName(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "users", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "app", ReferencedTableName: "users"}}},
		{Schema: "app", Name: "users"},
	}
	levels, err := BuildRestoreLevels(tables)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"app.users"}, {"public.users"}}
	if got := levelNames(levels); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildRestoreLevels_missingExternalParent(t *testing.T) {
	tables := []db.Table{
		{Schema: "app", Name: "orders", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "other", ReferencedTableName: "users"}}},
	}
	levels, err := BuildRestoreLevels(tables)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"app.orders"}}
	if got := levelNames(levels); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildRestoreLevels_duplicateFK(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "child", ForeignKeys: []db.ForeignKey{
			{ReferencedTableSchema: "public", ReferencedTableName: "parent", ColumnName: "a"},
			{ReferencedTableSchema: "public", ReferencedTableName: "parent", ColumnName: "b"},
		}},
		{Schema: "public", Name: "parent"},
	}
	levels, err := BuildRestoreLevels(tables)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"public.parent"}, {"public.child"}}
	if got := levelNames(levels); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildRestoreLevels_cycleDeterministic(t *testing.T) {
	makeCycle := func() []db.Table {
		return []db.Table{
			{Schema: "public", Name: "b", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "a"}}},
			{Schema: "public", Name: "a", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "b"}}},
		}
	}
	_, err1 := BuildRestoreLevels(makeCycle())
	_, err2 := BuildRestoreLevels([]db.Table{
		makeCycle()[1],
		makeCycle()[0],
	})
	for _, err := range []error{err1, err2} {
		var cycle *RestoreCycleError
		if !errors.As(err, &cycle) {
			t.Fatalf("expected RestoreCycleError, got %v", err)
		}
		if !errors.Is(err, ErrRestoreCycle) {
			t.Fatalf("expected ErrRestoreCycle, got %v", err)
		}
		want := []string{"public.a", "public.b"}
		if !reflect.DeepEqual(cycle.Tables, want) {
			t.Fatalf("cycle tables = %v, want %v", cycle.Tables, want)
		}
	}
	_, err := BuildRestoreLevels([]db.Table{
		{Schema: "public", Name: "self_ref", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "self_ref"}}},
	})
	var selfCycle *RestoreCycleError
	if !errors.As(err, &selfCycle) {
		t.Fatalf("expected RestoreCycleError, got %v", err)
	}
	if !reflect.DeepEqual(selfCycle.Tables, []string{"public.self_ref"}) {
		t.Fatalf("self-cycle tables = %v", selfCycle.Tables)
	}
}

func TestBuildRestoreLevels_qualifiedLabelDotCollision(t *testing.T) {
	tables := []db.Table{
		{Schema: "a.b", Name: "c"},
		{Schema: "a", Name: "b.c"},
	}
	levels, err := BuildRestoreLevels(tables)
	if err != nil {
		t.Fatal(err)
	}
	got := levelNames(levels)
	want := [][]string{{`"a"."b.c"`, `"a.b"."c"`}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got[0][0] == got[0][1] {
		t.Fatal("qualified labels must not collide")
	}
}

func TestBuildRestoreLevels_shuffledInputStable(t *testing.T) {
	base := []db.Table{
		{Schema: "public", Name: "c", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "b"}}},
		{Schema: "public", Name: "a"},
		{Schema: "public", Name: "b", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "a"}}},
	}
	shuffled := []db.Table{base[0], base[2], base[1]}
	levelsA, err := BuildRestoreLevels(base)
	if err != nil {
		t.Fatal(err)
	}
	levelsB, err := BuildRestoreLevels(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(levelNames(levelsA), levelNames(levelsB)) {
		t.Fatalf("shuffled levels differ: %v vs %v", levelNames(levelsA), levelNames(levelsB))
	}
}

func levelNames(levels []RestoreLevel) [][]string {
	out := make([][]string, len(levels))
	for i, level := range levels {
		out[i] = append([]string(nil), level.Tables...)
	}
	return out
}
