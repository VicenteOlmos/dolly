package restore

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func dumpTables() []db.Table {
	return []db.Table{
		{Schema: "public", Name: "users"},
		{Schema: "public", Name: "orders"},
		{Schema: "app", Name: "users"},
	}
}

func TestResolveRestoreExcludeSelectorQualified(t *testing.T) {
	qt, err := ResolveRestoreExcludeSelector("public.orders", dumpTables())
	if err != nil {
		t.Fatal(err)
	}
	if qt.Schema != "public" || qt.Name != "orders" {
		t.Fatalf("got %+v", qt)
	}
}

func TestResolveRestoreExcludeSelectorBareNameUnique(t *testing.T) {
	qt, err := ResolveRestoreExcludeSelector("orders", dumpTables())
	if err != nil {
		t.Fatal(err)
	}
	if qt.Name != "orders" || qt.Schema != "public" {
		t.Fatalf("got %+v", qt)
	}
}

func TestResolveRestoreExcludeSelectorBareNameAmbiguous(t *testing.T) {
	_, err := ResolveRestoreExcludeSelector("users", dumpTables())
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRestoreExcludeSelectorMissing(t *testing.T) {
	_, err := ResolveRestoreExcludeSelector("public.missing", dumpTables())
	if err == nil || !strings.Contains(err.Error(), "not found in dump") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyRestoreTableExclusionsFilters(t *testing.T) {
	filtered, excluded, _, err := ApplyRestoreTableExclusions(dumpTables(), []string{"public.orders", "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if excluded != 1 || len(filtered) != 2 {
		t.Fatalf("filtered=%d excluded=%d", len(filtered), excluded)
	}
}

func TestApplyRestoreTableExclusionsEmptySet(t *testing.T) {
	_, _, _, err := ApplyRestoreTableExclusions(dumpTables(), []string{
		"public.users", "public.orders", "app.users",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRestoreEmptyTableSetError(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "only"}}
	filtered, excluded, _, err := ApplyRestoreTableExclusions(tables, []string{"only"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 0 || excluded != 1 {
		t.Fatalf("filtered=%d excluded=%d", len(filtered), excluded)
	}
	msg := (&EmptyTableSetError{InputDir: "/tmp/x"}).Error()
	if !strings.Contains(msg, "table set is empty") {
		t.Fatalf("message = %q", msg)
	}
	if !IsEmptyTableSetError(&EmptyTableSetError{}) {
		t.Fatal("IsEmptyTableSetError")
	}
}
