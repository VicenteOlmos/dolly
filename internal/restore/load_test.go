package restore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestLoadTableContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := t.TempDir()
	path := filepath.Join(dir, "users.ndjson")
	if err := os.WriteFile(path, []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema:  "public",
		Name:    "users",
		Columns: []db.Column{{Name: "id", DataType: "integer", PrimaryKey: true}},
	}

	err = loadTable(ctx, sqlDB, table, path, ConflictError)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTableOnlyGeneratedColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "computed.ndjson")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i := 0; i < 2; i++ {
		mock.ExpectExec(`INSERT INTO "public"\."computed" DEFAULT VALUES`).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	table := db.Table{Schema: "public", Name: "computed", Columns: []db.Column{{Name: "value", Generated: true}}}
	if err := loadTable(context.Background(), conn, table, path, ConflictError); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTableValidateTableNameRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.ndjson")
	if err := os.WriteFile(path, []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema:  "public",
		Name:    "../etc",
		Columns: []db.Column{{Name: "id", DataType: "integer", PrimaryKey: true}},
	}

	err = loadTable(context.Background(), sqlDB, table, path, ConflictError)
	if err == nil {
		t.Fatal("expected error for traversal table name")
	}
	if !strings.Contains(err.Error(), "validate table") {
		t.Fatalf("error = %v, want validate table error", err)
	}
}

func TestLoadTableValidateTableNameRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.ndjson")
	if err := os.WriteFile(path, []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema:  "public",
		Name:    "",
		Columns: []db.Column{{Name: "id", DataType: "integer", PrimaryKey: true}},
	}

	err = loadTable(context.Background(), sqlDB, table, path, ConflictError)
	if err == nil {
		t.Fatal("expected error for empty table name")
	}
	if !strings.Contains(err.Error(), "validate table") {
		t.Fatalf("error = %v, want validate table error", err)
	}
}

func TestWritableTableRetainsAlwaysIdentity(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "invoices",
		Columns: []db.Column{
			{Name: "id", Identity: "ALWAYS", PrimaryKey: true},
			{Name: "total", Generated: true},
			{Name: "note"},
		},
	}
	got, err := writableTable(table)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Columns) != 2 || got.Columns[0].Name != "id" || got.Columns[0].Identity != "ALWAYS" {
		t.Fatalf("writable columns = %+v", got.Columns)
	}
	// COPY uses the explicit column list above; INSERT fallback uses OVERRIDING SYSTEM VALUE.
	q, _, err := buildInsert(got, ConflictError)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "OVERRIDING SYSTEM VALUE") {
		t.Fatalf("insert fallback query = %s", q)
	}
}
