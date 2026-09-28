package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestResolveDataFileForwardSlashRelativePath(t *testing.T) {
	root := t.TempDir()
	dataRel := "data/7075626c6963.7573657273.ndjson"
	if err := os.Mkdir(filepath.Join(root, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(dataRel)), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	table := db.Table{Name: "users", DataFile: &dataRel}
	got, err := resolveDataFile(root, table)
	if err != nil {
		t.Fatalf("resolveDataFile: %v", err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("stat resolved path: %v", err)
	}
	if _, err := verifyNDJSONFiles(dumpMetadataWithTable(table), root); err != nil {
		t.Fatalf("verifyNDJSONFiles: %v", err)
	}
}

func dumpMetadataWithTable(table db.Table) dump.Metadata {
	return dump.Metadata{Schema: "public", Tables: []db.Table{table}}
}