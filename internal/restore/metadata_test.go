package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestVerifyNDJSONFiles(t *testing.T) {
	dir := t.TempDir()
	meta := dump.Metadata{
		Schema: "public",
		Tables: []db.Table{{Name: "users", Columns: []db.Column{{Name: "id"}}}},
	}
	if err := verifyNDJSONFiles(meta, dir); err == nil {
		t.Fatal("expected missing file error")
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyNDJSONFiles(meta, dir); err != nil {
		t.Fatal(err)
	}
}
