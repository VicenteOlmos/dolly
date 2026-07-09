package dump

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestWriteMetadata(t *testing.T) {
	dir := t.TempDir()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "users",
			Columns: []db.Column{
				{Name: "id", DataType: "integer"},
			},
			ForeignKeys: []db.ForeignKey{
				{ConstraintName: "fk_users_group", ColumnName: "group_id", ReferencedTableSchema: "public", ReferencedTableName: "groups", ReferencedColumnName: "id"},
			},
		},
	}

	path, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(path) != "metadata.json.tmp" {
		t.Fatalf("unexpected path basename: %s", filepath.Base(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}

	if meta.Schema != "public" {
		t.Fatalf("schema = %q, want public", meta.Schema)
	}
	if len(meta.Tables) != 1 || meta.Tables[0].Name != "users" {
		t.Fatalf("tables = %+v, want 1 table named users", meta.Tables)
	}
	if len(meta.Tables[0].Columns) != 1 || meta.Tables[0].Columns[0].Name != "id" {
		t.Fatalf("columns = %+v, want 1 column named id", meta.Tables[0].Columns)
	}
	if len(meta.Tables[0].ForeignKeys) != 1 || meta.Tables[0].ForeignKeys[0].ConstraintName != "fk_users_group" {
		t.Fatalf("foreign_keys = %+v, want 1 fk named fk_users_group", meta.Tables[0].ForeignKeys)
	}

	if _, err := time.Parse(time.RFC3339, meta.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}
}

func TestWriteMetadataEmpty(t *testing.T) {
	dir := t.TempDir()

	path, err := writeMetadata(dir, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}

	if len(meta.Tables) != 0 {
		t.Fatalf("expected 0 tables, got %d", len(meta.Tables))
	}
}

func TestWriteMetadataDeterministic(t *testing.T) {
	dir := t.TempDir()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "z_table",
			Columns: []db.Column{
				{Name: "b_col", DataType: "text"},
				{Name: "a_col", DataType: "integer"},
			},
			ForeignKeys: []db.ForeignKey{
				{ConstraintName: "fk_b", ColumnName: "b_col", ReferencedTableSchema: "public", ReferencedTableName: "other", ReferencedColumnName: "id"},
				{ConstraintName: "fk_a", ColumnName: "a_col", ReferencedTableSchema: "public", ReferencedTableName: "other", ReferencedColumnName: "id"},
			},
		},
		{
			Schema: "public",
			Name:   "a_table",
			Columns: []db.Column{
				{Name: "id", DataType: "integer"},
			},
		},
	}

	path1, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data1, err := os.ReadFile(path1)
	if err != nil {
		t.Fatal(err)
	}

	path2, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatal(err)
	}

	var meta1, meta2 Metadata
	if err := json.Unmarshal(data1, &meta1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data2, &meta2); err != nil {
		t.Fatal(err)
	}

	meta1.GeneratedAt = ""
	meta2.GeneratedAt = ""

	out1, err := json.Marshal(meta1)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := json.Marshal(meta2)
	if err != nil {
		t.Fatal(err)
	}

	if string(out1) != string(out2) {
		t.Fatal("metadata output is not deterministic")
	}
}

func TestReadMetadata(t *testing.T) {
	dir := t.TempDir()
	tables := []db.Table{
		{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", DataType: "integer"}}},
	}
	path, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Schema != "public" || len(meta.Tables) != 1 || meta.Tables[0].Name != "users" {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestWriteMetadataMultiSchemaLabel(t *testing.T) {
	dir := t.TempDir()
	tables := []db.Table{
		{Schema: "app", Name: "orders"},
		{Schema: "billing", Name: "invoices"},
	}
	path, err := writeMetadata(dir, tables, nil, []string{"app", "billing"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Schema != "multi" {
		t.Fatalf("schema = %q, want multi", meta.Schema)
	}
}

func TestReadMetadataMissing(t *testing.T) {
	_, err := ReadMetadata(t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}
