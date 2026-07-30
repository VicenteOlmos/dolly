package dump

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestParseQualifiedTable(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    QualifiedTable
		wantErr string
	}{
		{name: "unquoted", raw: "public.users", want: QualifiedTable{Schema: "public", Name: "users"}},
		{name: "quoted both", raw: `"my.schema"."my.table"`, want: QualifiedTable{Schema: "my.schema", Name: "my.table"}},
		{name: "quoted escape", raw: `"pub""lic"."us""ers"`, want: QualifiedTable{Schema: `pub"lic`, Name: `us"ers`}},
		{name: "blank", raw: "  ", wantErr: "empty"},
		{name: "unqualified", raw: "users", wantErr: "unqualified"},
		{name: "extra dot unquoted", raw: "a.b.c", wantErr: "extra dots"},
		{name: "csv", raw: "public.users,public.orders", wantErr: "CSV"},
		{name: "glob star", raw: "public.user*", wantErr: "glob"},
		{name: "glob question", raw: "public.?users", wantErr: "glob"},
		{name: "system schema", raw: "pg_catalog.pg_class", wantErr: "system schema"},
		{name: "information schema", raw: "information_schema.tables", wantErr: "system schema"},
		{name: "pg_temp", raw: "pg_temp_1.foo", wantErr: "system schema"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseQualifiedTable(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestQualifiedTableKeyCollisionSafe(t *testing.T) {
	a := QualifiedTable{Schema: "a.b", Name: "c"}
	b := QualifiedTable{Schema: "a", Name: "b.c"}
	if a.key() == b.key() {
		t.Fatalf("keys should differ: %q vs %q", a.key(), b.key())
	}
}

func TestLoadSelectorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tables.txt")
	content := "# comment\n\npublic.users\npublic.orders\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, ignored, err := LoadSelectorEntries(nil, []string{path}, "file", "--include-table-file")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if len(ignored) != 2 {
		t.Fatalf("ignored = %d, want 2", len(ignored))
	}
	if entries[0].Table != (QualifiedTable{Schema: "public", Name: "orders"}) {
		t.Fatalf("first entry = %+v", entries[0].Table)
	}
	if entries[1].Table != (QualifiedTable{Schema: "public", Name: "users"}) {
		t.Fatalf("second entry = %+v", entries[1].Table)
	}
}

func TestPlanTableSelectionPrecedence(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "users"},
		{Schema: "public", Name: "orders"},
		{Schema: "public", Name: "audit_log"},
	}
	policy := &SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "users"}}},
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "users"}}},
	}

	filtered, prov, err := PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 0 {
		t.Fatalf("filtered = %+v, want empty", filtered)
	}
	if err := guardSelectedTables(filtered, []string{"public"}); !IsNoTablesError(err) {
		t.Fatalf("guard = %v, want NoTablesError", err)
	}
	if len(prov.Warnings) != 0 {
		t.Fatalf("warnings = %v", prov.Warnings)
	}

	policy = &SelectionPolicy{
		Includes: []SelectorEntry{
			{Table: QualifiedTable{Schema: "public", Name: "users"}},
			{Table: QualifiedTable{Schema: "public", Name: "orders"}},
		},
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "orders"}}},
	}
	filtered, prov, err = PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "users" {
		t.Fatalf("filtered = %+v", filtered)
	}
	if prov.Selected[0] != "public.users" {
		t.Fatalf("selected = %v", prov.Selected)
	}
}

func TestPlanTableSelectionExcludeAllReturnsEmptyForGuard(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "users"},
		{Schema: "public", Name: "orders"},
	}
	policy := &SelectionPolicy{
		Excludes: []SelectorEntry{
			{Table: QualifiedTable{Schema: "public", Name: "users"}},
			{Table: QualifiedTable{Schema: "public", Name: "orders"}},
		},
	}
	filtered, _, err := PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 0 {
		t.Fatalf("filtered = %+v, want empty", filtered)
	}
	if err := guardSelectedTables(filtered, []string{"public"}); !IsNoTablesError(err) {
		t.Fatalf("guard = %v, want NoTablesError", err)
	}
}

func TestPlanTableSelectionIncludeMissFails(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "users"}}
	policy := &SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "missing"}}},
	}
	_, _, err := PlanTableSelection(tables, policy, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
	if !IsTableSelectionError(err) {
		t.Fatalf("error = %v, want ErrTableSelection", err)
	}
}

func TestPlanTableSelectionExcludeMissWarns(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "users"}}
	policy := &SelectionPolicy{
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "missing"}}},
	}
	filtered, prov, err := PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered = %+v", filtered)
	}
	if len(prov.Warnings) != 1 || !strings.Contains(prov.Warnings[0], "missing") {
		t.Fatalf("warnings = %v", prov.Warnings)
	}
}

func TestPlanTableSelectionExcludeOnly(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "users"},
		{Schema: "public", Name: "audit_log"},
	}
	policy := &SelectionPolicy{
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "audit_log"}}},
	}
	filtered, prov, err := PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "users" {
		t.Fatalf("filtered = %+v", filtered)
	}
	if prov.Selected[0] != "public.users" {
		t.Fatalf("selected = %v", prov.Selected)
	}
}

func TestPlanTableSelectionDeterministicProvenanceNoSecrets(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "users"}}
	policy := &SelectionPolicy{
		Includes: []SelectorEntry{{
			Table: QualifiedTable{Schema: "public", Name: "users"},
			Source: SelectorSource{
				Kind: "flag",
				Name: "--include-table",
			},
		}},
	}
	_, prov, err := PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(prov)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, secret := range []string{"postgres://", "password", "dsn"} {
		if strings.Contains(strings.ToLower(s), secret) {
			t.Fatalf("provenance leaked %q: %s", secret, s)
		}
	}
	if prov.RequestedIncludes[0].Source != "--include-table" {
		t.Fatalf("source = %q", prov.RequestedIncludes[0].Source)
	}
}

func TestPlanTableSelectionDeterministicOutput(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "users"},
		{Schema: "public", Name: "orders"},
		{Schema: "public", Name: "audit_log"},
	}
	policy := &SelectionPolicy{Includes: []SelectorEntry{
		{Table: QualifiedTable{Schema: "public", Name: "users"}},
		{Table: QualifiedTable{Schema: "public", Name: "orders"}},
	}}
	filtered1, prov1, err := PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	filtered2, prov2, err := PlanTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(filtered1, filtered2) {
		t.Fatalf("filtered differs: %+v vs %+v", filtered1, filtered2)
	}
	if !reflect.DeepEqual(prov1.Selected, prov2.Selected) {
		t.Fatalf("selected differs: %v vs %v", prov1.Selected, prov2.Selected)
	}
}
