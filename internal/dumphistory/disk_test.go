package dumphistory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListBaseMergedIncludesUnregisteredDisk(t *testing.T) {
	baseDir := t.TempDir()
	dir1 := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [{"name": "users", "row_count": 10}]
}`
	if err := os.WriteFile(filepath.Join(dir1, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	recs, err := ListBaseMerged(baseDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1 from disk scan", len(recs))
	}
	if recs[0].Seq != 1 || recs[0].SchemaLabel != "public" || recs[0].TableCount != 1 {
		t.Fatalf("record = %+v, want seq 1 public 1 table", recs[0])
	}
}

func TestListBaseMergedPrefersStoreOverDisk(t *testing.T) {
	baseDir := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Register(Record{
		Seq:         1,
		BaseDir:     baseDir,
		Path:        path,
		CreatedAt:   now,
		SchemaLabel: "app",
		TableCount:  5,
	}); err != nil {
		t.Fatal(err)
	}

	recs, err := ListBaseMerged(baseDir, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if recs[0].SchemaLabel != "app" {
		t.Fatalf("schema = %q, want app from store", recs[0].SchemaLabel)
	}
}
