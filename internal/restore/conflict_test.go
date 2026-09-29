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

func TestBuildInsertAlwaysIdentity(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "invoices",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true, Identity: "ALWAYS"},
			{Name: "note", DataType: "text"},
		},
	}

	q, _, err := buildInsert(table, ConflictError)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `OVERRIDING SYSTEM VALUE`) || strings.Contains(q, "ON CONFLICT") {
		t.Fatalf("error policy query = %s", q)
	}

	q, _, err = buildInsert(table, ConflictSkip)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `OVERRIDING SYSTEM VALUE`) || !strings.Contains(q, `ON CONFLICT ("id") DO NOTHING`) {
		t.Fatalf("skip policy query = %s", q)
	}

	q, _, err = buildInsert(table, ConflictUpsert)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `OVERRIDING SYSTEM VALUE`) || !strings.Contains(q, `ON CONFLICT ("id") DO UPDATE SET`) {
		t.Fatalf("upsert policy query = %s", q)
	}

	byDefault := table
	byDefault.Columns[0].Identity = "BY DEFAULT"
	q, _, err = buildInsert(byDefault, ConflictError)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "OVERRIDING SYSTEM VALUE") {
		t.Fatalf("BY DEFAULT should not override: %s", q)
	}

	empty := db.Table{Schema: "public", Name: "defaults"}
	q, _, err = buildInsert(empty, ConflictError)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "OVERRIDING SYSTEM VALUE") {
		t.Fatalf("DEFAULT VALUES must not override: %s", q)
	}

	nonPKIdentity := db.Table{
		Schema: "public",
		Name:   "line_items",
		Columns: []db.Column{
			{Name: "code", DataType: "text", PrimaryKey: true},
			{Name: "seq", DataType: "bigint", Identity: "ALWAYS"},
			{Name: "qty", DataType: "integer"},
			{Name: "legacy_id", DataType: "bigint", Identity: "BY DEFAULT"},
		},
	}
	q, _, err = buildInsert(nonPKIdentity, ConflictUpsert)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `ON CONFLICT ("code") DO UPDATE SET`) {
		t.Fatalf("upsert policy query = %s", q)
	}
	if strings.Contains(q, `"seq" = EXCLUDED."seq"`) {
		t.Fatalf("ALWAYS identity must not appear in upsert SET: %s", q)
	}
	if !strings.Contains(q, `"qty" = EXCLUDED."qty"`) || !strings.Contains(q, `"legacy_id" = EXCLUDED."legacy_id"`) {
		t.Fatalf("normal and BY DEFAULT columns must update: %s", q)
	}
}

func TestBuildInsertUpsertSkipsGeneratedColumn(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "orders",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "total", DataType: "integer", Generated: true},
			{Name: "note", DataType: "text"},
		},
	}
	q, _, err := buildInsert(table, ConflictUpsert)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, `"total" = EXCLUDED."total"`) {
		t.Fatalf("generated column must not appear in upsert SET: %s", q)
	}
	if !strings.Contains(q, `"note" = EXCLUDED."note"`) {
		t.Fatalf("writable column must update: %s", q)
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
