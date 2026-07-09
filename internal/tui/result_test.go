package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectDumpResultSummaryFullMetadata(t *testing.T) {
	dir := t.TempDir()
	meta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [
    {"schema": "public", "name": "users", "row_count": 100, "columns": []},
    {"schema": "public", "name": "orders", "row_count": 50, "columns": []}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"users.ndjson", "orders.ndjson"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	summary := collectDumpResultSummary(dir, nil)

	if summary.Outcome != DumpOutcomeSuccess {
		t.Fatalf("Outcome = %v, want success", summary.Outcome)
	}
	if summary.MetadataMissing {
		t.Fatal("expected metadata present")
	}
	if summary.TableCount != 2 {
		t.Fatalf("TableCount = %d, want 2", summary.TableCount)
	}
	if summary.TotalRowEstimate == nil || *summary.TotalRowEstimate != 150 {
		t.Fatalf("TotalRowEstimate = %v, want 150", summary.TotalRowEstimate)
	}
	if len(summary.Files) != 3 {
		t.Fatalf("Files = %v, want 3 artifacts", summary.Files)
	}
}

func TestCollectDumpResultSummaryMissingMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary := collectDumpResultSummary(dir, nil)

	if !summary.MetadataMissing {
		t.Fatal("expected MetadataMissing")
	}
	if summary.TableCount != 0 {
		t.Fatalf("TableCount = %d, want 0", summary.TableCount)
	}
	if len(summary.Files) != 1 || summary.Files[0] != "users.ndjson" {
		t.Fatalf("Files = %v, want [users.ndjson]", summary.Files)
	}
}

func TestCollectDumpResultSummaryExcludesTmp(t *testing.T) {
	dir := t.TempDir()
	meta := `{"generated_at":"2026-01-01T00:00:00Z","schema":"public","tables":[]}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson.tmp"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary := collectDumpResultSummary(dir, nil)

	if !summary.HasIncomplete {
		t.Fatal("expected HasIncomplete for .tmp file")
	}
	for _, name := range summary.Files {
		if filepath.Ext(name) == ".tmp" || name == "users.ndjson.tmp" {
			t.Fatalf("Files should exclude .tmp, got %v", summary.Files)
		}
	}
}

func TestCollectDumpResultSummaryPartialErrorDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"generated_at":"2026-01-01T00:00:00Z","schema":"public","tables":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "partial.ndjson"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary := collectDumpResultSummary(dir, os.ErrPermission)

	if summary.Outcome != DumpOutcomeError {
		t.Fatalf("Outcome = %v, want error", summary.Outcome)
	}
	if summary.Error == "" {
		t.Fatal("expected error text")
	}
	if len(summary.Files) != 2 {
		t.Fatalf("Files = %v, want partial artifacts", summary.Files)
	}
}

func TestCollectDumpResultSummaryRowEstimateSum(t *testing.T) {
	dir := t.TempDir()
	meta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [
    {"schema": "public", "name": "a", "row_count": 1000, "columns": []},
    {"schema": "public", "name": "b", "columns": []},
    {"schema": "public", "name": "c", "row_count": 240, "columns": []}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	summary := collectDumpResultSummary(dir, nil)

	if summary.TotalRowEstimate == nil || *summary.TotalRowEstimate != 1240 {
		t.Fatalf("TotalRowEstimate = %v, want 1240", summary.TotalRowEstimate)
	}
}
