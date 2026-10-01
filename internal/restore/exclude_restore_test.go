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

func TestRestoreExcludeAllTablesFailsBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables:      []db.Table{{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id"}}}},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	err = Restore(context.Background(), sqlDB, dir, WithExcludeTables([]string{"public.users"}))
	if !IsEmptyTableSetError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRestoreSequencesSkipExcludedTableOwner(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	meta := dump.Metadata{
		Tables: []db.Table{
			{Schema: "public", Name: "orders", Columns: []db.Column{{Name: "id"}}},
		},
		Sequences: []dump.SequenceState{
			{Schema: "public", Name: "users_id_seq", StartValue: 9},
			{Schema: "public", Name: "orders_id_seq", StartValue: 3},
		},
	}
	allDumpTables := []db.Table{
		{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id"}}},
		{Schema: "public", Name: "orders", Columns: []db.Column{{Name: "id"}}},
	}

	mock.ExpectQuery(`SELECT tbl_ns.nspname, tbl.relname, a.attname`).
		WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column"}).AddRow("public", "orders", "id"))
	expectSequenceCurrentValueLess(mock)
	mock.ExpectExec(`SELECT setval\('"public"\."orders_id_seq"'`).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, map[string]bool{
		tableKey("public", "users"): true,
	}, allDumpTables); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataSkipsExcludedOwnerMissingOnTarget(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	meta := dump.Metadata{
		Tables: []db.Table{
			{Schema: "public", Name: "orders", Columns: []db.Column{{Name: "id"}}},
		},
		Sequences: []dump.SequenceState{
			{Schema: "public", Name: "users_id_seq", StartValue: 42},
			{Schema: "public", Name: "orders_id_seq", StartValue: 3},
		},
	}
	allDumpTables := []db.Table{
		{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id"}}},
		{Schema: "public", Name: "orders", Columns: []db.Column{{Name: "id"}}},
	}

	mock.ExpectQuery(`SELECT tbl_ns.nspname, tbl.relname, a.attname`).
		WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column"}).AddRow("public", "orders", "id"))
	expectSequenceCurrentValueLess(mock)
	mock.ExpectExec(`SELECT setval\('"public"\."orders_id_seq"'`).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, map[string]bool{
		tableKey("public", "users"): true,
	}, allDumpTables); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
