package restore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestVerifyNDJSONFiles(t *testing.T) {
	dir := t.TempDir()
	meta := dump.Metadata{
		Schema: "public",
		Tables: []db.Table{{Name: "users", Columns: []db.Column{{Name: "id"}}}},
	}
	if _, err := verifyNDJSONFiles(meta, dir); err == nil {
		t.Fatal("expected missing file error")
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyNDJSONFiles(meta, dir); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyNDJSONFilesRejectsUnsafeDeclaredPaths(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.ndjson")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "safe.ndjson"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.ndjson")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		paths []string
	}{
		{"empty", []string{""}},
		{"absolute", []string{outside}},
		{"backslash", []string{`data\\users.ndjson`}},
		{"non-clean", []string{"data/../safe.ndjson"}},
		{"dot", []string{"."}},
		{"dot-dot", []string{".."}},
		{"traversal", []string{"../outside.ndjson"}},
		{"duplicate", []string{"safe.ndjson", "safe.ndjson"}},
		{"directory", []string{"directory"}},
		{"symlink-escape", []string{"escape.ndjson"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := make([]db.Table, len(tt.paths))
			for i, path := range tt.paths {
				path := path
				tables[i] = db.Table{Name: "table" + string(rune('a'+i)), DataFile: &path}
			}
			if _, err := verifyNDJSONFiles(dump.Metadata{Schema: "public", Tables: tables}, dir); err == nil {
				t.Fatal("expected unsafe artifact error")
			}
		})
	}
}

func TestRestoreRejectsMissingOrUnsafeArtifactBeforeSQL(t *testing.T) {
	tests := []string{"missing.ndjson", "../escape.ndjson"}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			dir := t.TempDir()
			meta := dump.Metadata{Schema: "public", Tables: []db.Table{{Name: "users", DataFile: &path}}}
			data, err := json.Marshal(meta)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			if err := Restore(context.Background(), sqlDB, dir, WithReplace()); err == nil {
				t.Fatal("expected artifact validation error")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("artifact validation mutated SQL state: %v", err)
			}
		})
	}
}
