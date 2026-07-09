package restore

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestBuildInsertError(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	q, _, err := buildInsert(table, ConflictError)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "ON CONFLICT") {
		t.Fatalf("unexpected ON CONFLICT: %s", q)
	}
	if !strings.Contains(q, `INSERT INTO "public"."users"`) {
		t.Fatalf("query = %s", q)
	}
}

func TestBuildInsertSkip(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	q, _, err := buildInsert(table, ConflictSkip)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `ON CONFLICT ("id") DO NOTHING`) {
		t.Fatalf("query = %s", q)
	}
}

func TestBuildInsertUpsert(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "project_members",
		Columns: []db.Column{
			{Name: "project_id", DataType: "integer", PrimaryKey: true},
			{Name: "tbl_a_id", DataType: "integer", PrimaryKey: true},
			{Name: "role", DataType: "text"},
		},
	}

	q, _, err := buildInsert(table, ConflictUpsert)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `ON CONFLICT ("project_id", "tbl_a_id")`) {
		t.Fatalf("query = %s", q)
	}
	if !strings.Contains(q, `"role" = EXCLUDED."role"`) {
		t.Fatalf("query = %s", q)
	}
}

func TestParseConflictPolicy(t *testing.T) {
	p, err := ParseConflictPolicy("skip")
	if err != nil || p != ConflictSkip {
		t.Fatalf("got %v %v", p, err)
	}
	_, err = ParseConflictPolicy("bogus")
	if err == nil {
		t.Fatal("expected error")
	}
}
