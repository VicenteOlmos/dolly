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
