package dumphistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const registerCompletedMetadataJSON = `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [
    {"schema": "public", "name": "users", "row_count": 100, "columns": []},
    {"schema": "public", "name": "orders", "row_count": 50, "columns": []}
  ],
  "provenance": {
    "seq": 1,
    "base_dir": "/tmp/base",
    "source_database": "mydb",
    "schemas": ["public"],
    "table_count": 2,
    "total_row_estimate": 150
  }
}`

func TestRegisterCompletedDumpNilStore(t *testing.T) {
	if err := RegisterCompletedDump(nil, "/base", 1, "/base/1", "db", nil); err != nil {
		t.Fatalf("nil store: %v", err)
	}
}

func TestRegisterCompletedDumpMissingMetadata(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	err = RegisterCompletedDump(store, dir, 1, dir, "db", nil)
	if err == nil {
		t.Fatal("expected error for missing metadata.json")
	}
	if !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("error = %q, want metadata mention", err.Error())
	}
}

func TestRegisterCompletedDumpSuccess(t *testing.T) {
	baseDir := t.TempDir()
	outDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "metadata.json"), []byte(registerCompletedMetadataJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	schemas := []string{"public"}
	if err := RegisterCompletedDump(store, baseDir, 1, outDir, "mydb", schemas); err != nil {
		t.Fatalf("RegisterCompletedDump: %v", err)
	}

	recs, err := store.ListBase(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	rec := recs[0]
	if rec.Seq != 1 {
		t.Fatalf("seq = %d, want 1", rec.Seq)
	}
	if rec.Path != outDir {
		t.Fatalf("path = %q, want %q", rec.Path, outDir)
	}
	if rec.SourceDatabase != "mydb" {
		t.Fatalf("source_database = %q, want mydb", rec.SourceDatabase)
	}
	if rec.SchemaLabel != "public" {
		t.Fatalf("schema_label = %q, want public", rec.SchemaLabel)
	}
	if rec.TableCount != 2 {
		t.Fatalf("table_count = %d, want 2", rec.TableCount)
	}
	if rec.RowEstimate != 150 {
		t.Fatalf("row_estimate = %d, want 150", rec.RowEstimate)
	}
	if len(rec.Schemas) != 1 || rec.Schemas[0] != "public" {
		t.Fatalf("schemas = %v, want [public]", rec.Schemas)
	}
}
