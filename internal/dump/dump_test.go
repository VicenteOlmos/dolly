package dump

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestDumpFullFlow(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(2))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true).
		AddRow("public", "users", "name", "text", "YES", 2, false)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	streamRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").
		AddRow(2, "v2")
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(streamRows)

	mock.ExpectCommit()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences())
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); os.IsNotExist(err) {
		t.Fatal("metadata.json not found")
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "7075626c6963.7573657273.ndjson")); os.IsNotExist(err) {
		t.Fatal("users.ndjson not found")
	}
	if _, err := os.Stat(filepath.Join(dir, "metadata.json.tmp")); err == nil {
		t.Fatal("metadata.json.tmp should not exist")
	}
}

func TestDumpWithSchemasMulti(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("app", "users", int64(1)).
		AddRow("billing", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1, \$2\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("app", "billing").
		WillReturnRows(tablesRows)

	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("app", "users", "id", "integer", "NO", 1, true).
		AddRow("billing", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("app", "billing").WillReturnRows(allCols)
	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("app", "billing").WillReturnRows(allFks)

	emptyUniqueIndexMock(mock)
	for _, id := range []int{1, 2} {
		streamRows := sqlmock.NewRows([]string{"id"}).AddRow(id)
		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(streamRows)
	}

	mock.ExpectCommit()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSchemas([]string{"app", "billing"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Schema != "multi" {
		t.Fatalf("metadata schema = %q, want multi", meta.Schema)
	}
	paths := map[string]bool{}
	for _, table := range meta.Tables {
		if table.DataFile == nil {
			t.Fatalf("table %s missing data_file", table.Schema)
		}
		paths[*table.DataFile] = true
		if _, err := os.Stat(filepath.Join(dir, *table.DataFile)); err != nil {
			t.Fatalf("table %s data file: %v", table.Schema, err)
		}
	}
	if len(paths) != 2 {
		t.Fatalf("same-name cross-schema paths collided: %v", paths)
	}
}

func TestDumpEmptyTable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "empty_tbl", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "empty_tbl", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	streamRows := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(streamRows)

	mock.ExpectCommit()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences())
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "data", "7075626c6963.656d7074795f74626c.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty ndjson file, got %q", string(data))
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Tables) != 1 || meta.Tables[0].DataFile == nil || *meta.Tables[0].DataFile != "data/7075626c6963.656d7074795f74626c.ndjson" {
		t.Fatalf("empty table data_file = %#v", meta.Tables)
	}
}

func TestAssignDataFilesIsCollisionFreeAndDeterministic(t *testing.T) {
	tables := []db.Table{{Schema: "app", Name: "users"}, {Schema: "audit", Name: "users"}}
	assignDataFiles(tables)
	first := []string{*tables[0].DataFile, *tables[1].DataFile}
	assignDataFiles(tables)
	second := []string{*tables[0].DataFile, *tables[1].DataFile}
	if first[0] == first[1] {
		t.Fatalf("same table name collided: %v", first)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("data_file paths changed: %v -> %v", first, second)
	}
}

func TestDumpCompleteDataFilesAreDeterministic(t *testing.T) {
	for run := 0; run < 2; run++ {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(t.TempDir(), "dump")
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).AddRow("public", "users", 1))
		mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).AddRow("public", "users", "id", "integer", "NO", 1, true))
		mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}))
		emptyUniqueIndexMock(mock)
		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectCommit()
		if err := Dump(context.Background(), sqlDB, dir, WithoutSequences()); err != nil {
			t.Fatal(err)
		}
		meta, err := ReadMetadata(dir)
		if err != nil || len(meta.Tables) != 1 || meta.Tables[0].DataFile == nil || *meta.Tables[0].DataFile != "data/7075626c6963.7573657273.ndjson" {
			t.Fatalf("run %d metadata=%+v err=%v", run, meta.Tables, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		sqlDB.Close()
	}
}

func TestDumpSnapshotConsistency(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	streamRows := sqlmock.NewRows([]string{"id"}).
		AddRow(1)
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(streamRows)

	mock.ExpectCommit()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences())
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDumpWithoutTransaction(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	streamRows := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(streamRows)

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithoutTransaction())
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDumpErrorContext(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnError(context.Canceled)

	mock.ExpectRollback()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "users") {
		t.Fatalf("error missing table context: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err == nil {
		t.Fatal("metadata.json should not exist on error")
	}
}

func TestDumpWithProgressCallbacks(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	streamRows := sqlmock.NewRows([]string{"id"}).
		AddRow(1)
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(streamRows)

	mock.ExpectCommit()

	var events []ProgressEvent
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProgress(func(ev ProgressEvent) {
		events = append(events, ev)
	}))
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (start+end)", len(events))
	}
	if events[0].Phase != "table_start" || events[0].Table != "users" {
		t.Fatalf("first event = %+v, want table_start users", events[0])
	}
	if events[0].Current != 1 || events[0].Total != 1 {
		t.Fatalf("first event Current=%d Total=%d, want 1/1", events[0].Current, events[0].Total)
	}
	if events[0].Elapsed <= 0 {
		t.Fatalf("first event Elapsed = %v, want > 0", events[0].Elapsed)
	}
	if events[1].Phase != "table_end" || events[1].Table != "users" {
		t.Fatalf("second event = %+v, want table_end users", events[1])
	}
	if events[1].Current != 1 || events[1].Total != 1 {
		t.Fatalf("second event Current=%d Total=%d, want 1/1", events[1].Current, events[1].Total)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDumpWithProgressCallbacksMultiTable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "alpha", int64(0)).
		AddRow("public", "beta", int64(0)).
		AddRow("public", "gamma", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "alpha", "id", "integer", "NO", 1, true).
		AddRow("public", "beta", "id", "integer", "NO", 1, true).
		AddRow("public", "gamma", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(allCols)
	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(allFks)

	emptyUniqueIndexMock(mock)

	for range 3 {
		streamRows := sqlmock.NewRows([]string{"id"})
		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(streamRows)
	}

	mock.ExpectCommit()

	var events []ProgressEvent
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProgress(func(ev ProgressEvent) {
		events = append(events, ev)
	}))
	if err != nil {
		t.Fatal(err)
	}

	// 3 tables × 2 events (start + end) = 6
	if len(events) != 6 {
		t.Fatalf("events = %d, want 6", len(events))
	}

	// Verify monotonic Current and Total
	for i, ev := range events {
		wantTotal := 3
		if ev.Total != wantTotal {
			t.Fatalf("event[%d] Total = %d, want %d", i, ev.Total, wantTotal)
		}
		// Current alternates: 1,1,2,2,3,3 for start+end pairs
		wantCurrent := (i / 2) + 1
		if ev.Current != wantCurrent {
			t.Fatalf("event[%d] Current = %d, want %d", i, ev.Current, wantCurrent)
		}
		if ev.Elapsed <= 0 {
			t.Fatalf("event[%d] Elapsed = %v, want > 0", i, ev.Elapsed)
		}
	}

	// Verify phase sequence
	wantPhases := []string{"table_start", "table_end", "table_start", "table_end", "table_start", "table_end"}
	for i, ev := range events {
		if ev.Phase != wantPhases[i] {
			t.Fatalf("event[%d] Phase = %q, want %q", i, ev.Phase, wantPhases[i])
		}
	}

	// Verify table names
	wantTables := []string{"alpha", "alpha", "beta", "beta", "gamma", "gamma"}
	for i, ev := range events {
		if ev.Table != wantTables[i] {
			t.Fatalf("event[%d] Table = %q, want %q", i, ev.Table, wantTables[i])
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDumpWithProgressObserverPanic(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	streamRows := sqlmock.NewRows([]string{"id"}).
		AddRow(1)
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(streamRows)

	mock.ExpectCommit()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProgress(func(ev ProgressEvent) {
		if ev.Phase == "table_start" {
			panic("observer failure")
		}
	}))
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatalf("metadata.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "7075626c6963.7573657273.ndjson")); err != nil {
		t.Fatalf("users.ndjson: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDumpSanitizedVsUnsanitized(t *testing.T) {
	setupDumpUsersMock := func(mock sqlmock.Sqlmock) {
		tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
			AddRow("public", "users", int64(1))
		mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
			WithArgs("public").
			WillReturnRows(tablesRows)

		colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
			AddRow("public", "users", "id", "integer", "NO", 1, true).
			AddRow("public", "users", "name", "text", "YES", 2, false).
			AddRow("public", "users", "email", "text", "YES", 3, false)
		mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

		fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
		mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

		emptyUniqueIndexMock(mock)

		streamRows := sqlmock.NewRows([]string{"id", "name", "email"}).
			AddRow(1, "Alice", "alice@corp.test")
		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(streamRows)
	}

	runDump := func(t *testing.T, opts ...Option) string {
		t.Helper()
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer sqlDB.Close()

		dir := t.TempDir()
		mock.ExpectBegin()
		setupDumpUsersMock(mock)
		mock.ExpectCommit()

		allOpts := append([]Option{WithoutSequences()}, opts...)
		if err := Dump(context.Background(), sqlDB, dir, allOpts...); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "data", "7075626c6963.7573657273.ndjson"))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}

	plain := runDump(t)
	sanitized := runDump(t, WithRowTransform(SanitizeByPattern))

	if !strings.Contains(plain, "alice@corp.test") {
		t.Fatalf("plain dump should contain original email: %s", plain)
	}
	if strings.Contains(sanitized, "alice@corp.test") {
		t.Fatalf("sanitized dump should not contain original email: %s", sanitized)
	}
	if !strings.Contains(sanitized, "redacted@example.com") {
		t.Fatalf("sanitized dump should contain placeholder: %s", sanitized)
	}
	if !strings.Contains(plain, `"name":"Alice"`) || !strings.Contains(sanitized, `"name":"Alice"`) {
		t.Fatalf("name column should be unchanged in both dumps")
	}
}

func TestDumpContextCancellation(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = Dump(ctx, sqlDB, dir, WithoutSequences(), WithoutTransaction())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "load schema") {
		t.Fatalf("error missing context: %v", err)
	}
}

func TestDumpSlowWithSubsetRejected(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(),
		WithSlowConnection(),
		WithSubset(SubsetConfig{
			Seeds: []RowPredicate{{Table: "users", Column: "id", Op: "=", Value: "1"}},
		}),
	)
	if err == nil {
		t.Fatal("expected error for slow+subset")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("error missing incompatible context: %v", err)
	}
}

func TestDumpCapturesSequences(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	seqsRows := sqlmock.NewRows([]string{"schemaname", "sequencename", "last_value", "start_value"}).
		AddRow("public", "users_id_seq", 42, 1)
	mock.ExpectQuery(`SELECT schemaname, sequencename`).
		WillReturnRows(seqsRows)

	streamRows := sqlmock.NewRows([]string{"id"}).
		AddRow(1)
	mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(streamRows)

	mock.ExpectCommit()

	// Do NOT pass WithoutSequences() — captureSequences should run.
	if err := Dump(context.Background(), sqlDB, dir); err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Sequences) != 1 {
		t.Fatalf("Sequences = %d, want 1", len(meta.Sequences))
	}
	if meta.Sequences[0].Name != "users_id_seq" {
		t.Fatalf("Sequences[0].Name = %q, want users_id_seq", meta.Sequences[0].Name)
	}
}

func TestDumpWithTableSelectionFiltersBeforeMetadata(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(2)).
		AddRow("public", "orders", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	streamRows := sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2)
	mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(streamRows)

	mock.ExpectCommit()

	policy := SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "users"}}},
	}
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProvenance(Provenance{}), WithTableSelection(policy, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Tables) != 1 || meta.Tables[0].Name != "users" {
		t.Fatalf("tables = %+v, want only users", meta.Tables)
	}
	if meta.Provenance == nil || meta.Provenance.TableSelection == nil {
		t.Fatal("expected table_selection provenance")
	}
	if len(meta.Provenance.TableSelection.Selected) != 1 || meta.Provenance.TableSelection.Selected[0] != "public.users" {
		t.Fatalf("selected = %v", meta.Provenance.TableSelection.Selected)
	}
}

func TestDumpWithTableSelectionIncludeMissFailsBeforeOutput(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	policy := SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "missing"}}},
	}
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithTableSelection(policy, nil))
	if err == nil {
		t.Fatal("expected selection error")
	}
	if !IsTableSelectionError(err) {
		t.Fatalf("error = %v, want ErrTableSelection", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist on selection failure")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json.tmp")); statErr == nil {
		t.Fatal("metadata.json.tmp should not exist on selection failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDumpNoTablesInSelectedSchemaFailsBeforeOutput(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"})
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("empty_schema").
		WillReturnRows(tablesRows)

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSchemas([]string{"empty_schema"}))
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json.tmp")); statErr == nil {
		t.Fatal("metadata.json.tmp should not exist")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDumpExcludeAllSelectionFailsBeforeOutput(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1)).
		AddRow("public", "orders", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true).
		AddRow("public", "orders", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	policy := SelectionPolicy{
		Excludes: []SelectorEntry{
			{Table: QualifiedTable{Schema: "public", Name: "users"}},
			{Table: QualifiedTable{Schema: "public", Name: "orders"}},
		},
	}
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithTableSelection(policy, nil))
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json.tmp")); statErr == nil {
		t.Fatal("metadata.json.tmp should not exist")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDumpSubsetPercentEmptySchemaFailsBeforeOutput(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"})
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("empty_schema").
		WillReturnRows(tablesRows)

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSchemas([]string{"empty_schema"}),
		WithSubset(SubsetConfig{Percent: 50, Limits: DefaultSubsetLimits()}))
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json.tmp")); statErr == nil {
		t.Fatal("metadata.json.tmp should not exist")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDumpWithoutTableSelectionUnchanged(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1)).
		AddRow("public", "orders", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true).
		AddRow("public", "orders", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	for range 2 {
		streamRows := sqlmock.NewRows([]string{"id"}).AddRow(1)
		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(streamRows)
	}

	mock.ExpectCommit()

	if err := Dump(context.Background(), sqlDB, dir, WithoutSequences()); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Tables) != 2 {
		t.Fatalf("tables = %d, want 2 without selection", len(meta.Tables))
	}
	if meta.Provenance != nil && meta.Provenance.TableSelection != nil {
		t.Fatal("no-flag dump should not record table_selection provenance")
	}
}

func TestDumpChunkDispatchUsesSlowOnlyForNamedTables(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(2)).
		AddRow("public", "orders", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true).
		AddRow("public", "orders", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProvenance(Provenance{}),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "users"}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provenance == nil || meta.Provenance.ChunkTables == nil {
		t.Fatal("expected chunk_tables provenance")
	}
	if len(meta.Provenance.ChunkTables.Chunked) != 1 || meta.Provenance.ChunkTables.Chunked[0] != "public.users" {
		t.Fatalf("chunked = %v", meta.Provenance.ChunkTables.Chunked)
	}
}

func TestDumpChunkMissFailsBeforeMetadata(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "missing"}}))
	if err == nil || !IsChunkPolicyError(err) {
		t.Fatalf("error = %v, want chunk policy", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist on chunk planning failure")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json.tmp")); statErr == nil {
		t.Fatal("metadata.json.tmp should not exist on chunk planning failure")
	}
}

func TestDumpWorkersWithChunkRejected(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	err = Dump(context.Background(), sqlDB, t.TempDir(), WithoutSequences(), WithoutTransaction(),
		WithWorkers(2),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "users"}}))
	if err == nil || !strings.Contains(err.Error(), "parallel dump workers") {
		t.Fatalf("error = %v", err)
	}
}

func TestDumpChunkWithSubsetRejected(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	err = Dump(context.Background(), sqlDB, t.TempDir(), WithoutSequences(), WithoutTransaction(),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "users"}}),
		WithSubset(SubsetConfig{
			Seeds: []RowPredicate{{Table: "users", Column: "id", Op: "=", Value: "1"}},
		}))
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("error = %v", err)
	}
}

func TestDumpChunkTableResumeNoDuplicates(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	const chunkSize = 2

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(5))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true).
		AddRow("public", "users", "name", "text", "NO", 2, false)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	rows1 := sqlmock.NewRows([]string{"id", "name"})
	for i := 1; i <= chunkSize; i++ {
		rows1.AddRow(i, fmt.Sprintf("v%d", i))
	}
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(rows1)

	failingRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(chunkSize+1, fmt.Sprintf("v%d", chunkSize+1)).
		RowError(0, fmt.Errorf("simulate mid-chunk failure"))
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT").
		WithArgs(int64(chunkSize)).
		WillReturnRows(failingRows)

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(),
		WithSlowChunkSize(chunkSize),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "users"}}))
	if err == nil {
		t.Fatal("expected interrupted chunk dump error")
	}

	resumeRows := sqlmock.NewRows([]string{"id", "name"})
	for i := chunkSize + 1; i <= chunkSize+1; i++ {
		resumeRows.AddRow(i, fmt.Sprintf("v%d", i))
	}
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
			AddRow("public", "users", int64(5)))
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true).
		AddRow("public", "users", "name", "text", "NO", 2, false))
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}))
	emptyUniqueIndexMock(mock)
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT").
		WithArgs(int64(chunkSize)).
		WillReturnRows(resumeRows)

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(),
		WithSlowChunkSize(chunkSize),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "users"}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	finalPath := tableDataPath(dir, db.Table{Schema: "public", Name: "users"})
	// assignDataFiles runs during Dump; resolve path from metadata.
	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(meta.Tables))
	}
	finalPath = tableDataPath(dir, meta.Tables[0])
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(gotLines) != chunkSize+1 {
		t.Fatalf("got %d lines, want %d", len(gotLines), chunkSize+1)
	}
}

func TestDumpCaptureSequencesQueryErrorFailsClosed(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	// pg_sequences query fails (e.g. unreadable system view)
	mock.ExpectQuery(`SELECT schemaname, sequencename`).
		WillReturnError(fmt.Errorf("simulated pg_sequences failure"))

	mock.ExpectRollback()

	// Do NOT pass WithoutSequences() — captureSequences must run and fail.
	err = Dump(context.Background(), sqlDB, dir)
	if err == nil {
		t.Fatal("expected capture sequences error, got nil")
	}
	if !strings.Contains(err.Error(), "capture sequences") {
		t.Fatalf("error missing capture sequences context: %v", err)
	}
	// No metadata should be written.
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist on sequence capture failure")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json.tmp")); statErr == nil {
		t.Fatal("metadata.json.tmp should not exist on sequence capture failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDumpCaptureSequencesScopeErrorFailsClosed(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	mock.ExpectBegin()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	// pg_sequences returns a sequence from a schema outside cfg.schemas (public)
	seqsRows := sqlmock.NewRows([]string{"schemaname", "sequencename", "last_value", "start_value"}).
		AddRow("secret", "token_seq", 42, 1)
	mock.ExpectQuery(`SELECT schemaname, sequencename`).
		WithArgs("public").
		WillReturnRows(seqsRows)

	mock.ExpectRollback()

	err = Dump(context.Background(), sqlDB, dir, WithSchemas([]string{"public"}))
	if err == nil {
		t.Fatal("expected scope error, got nil")
	}
	if !strings.Contains(err.Error(), "capture sequences") {
		t.Fatalf("error missing capture sequences context: %v", err)
	}
	var scopeErr *SequenceScopeOutofRangeError
	if !errors.As(err, &scopeErr) {
		t.Fatalf("error = %v, want SequenceScopeOutofRangeError", err)
	}
	if scopeErr.Schema != "secret" || scopeErr.SeqName != "token_seq" {
		t.Fatalf("scope error = %+v", scopeErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist on scope error")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json.tmp")); statErr == nil {
		t.Fatal("metadata.json.tmp should not exist on scope error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func assertStrategyRecords(t *testing.T, got []TableStrategyRecord, wantTables []string, wantStrategies []KeyStrategy, wantResumable []bool) {
	t.Helper()
	if len(got) != len(wantTables) {
		t.Fatalf("strategies = %+v, want %d records", got, len(wantTables))
	}
	for i := range wantTables {
		if got[i].Table != wantTables[i] {
			t.Fatalf("record[%d].table = %q, want %q", i, got[i].Table, wantTables[i])
		}
		if got[i].Strategy != wantStrategies[i] {
			t.Fatalf("record[%d].strategy = %q, want %q", i, got[i].Strategy, wantStrategies[i])
		}
		if got[i].Resumable != wantResumable[i] {
			t.Fatalf("record[%d].resumable = %t, want %t", i, got[i].Resumable, wantResumable[i])
		}
		if wantResumable[i] {
			if len(got[i].KeyColumns) == 0 || got[i].Fingerprint == "" {
				t.Fatalf("record[%d] missing key identity: %+v", i, got[i])
			}
		} else if len(got[i].KeyColumns) != 0 || got[i].Fingerprint != "" {
			t.Fatalf("record[%d] should omit key identity: %+v", i, got[i])
		}
	}
}

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stderr = w
	outC := make(chan string)
	go func() {
		var buf strings.Builder
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()
	fn()
	w.Close()
	os.Stderr = old
	return <-outC
}

func eventsCodeUniqueIndexRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"nspname", "relname", "index_name", "index_oid", "indisprimary",
		"indisvalid", "indisready", "amname", "has_predicate",
		"is_expression", "indnkeyatts", "attname", "is_nullable", "pos",
		"attnum", "opclass_oid", "collation_oid", "optval",
	}).AddRow("public", "events", "events_code_key", 20, false, true, true, "btree", false, false, 1, "code", false, 1, int64(1), int64(1978), int64(0), int64(0))
}

func expectEventsCodeUniqueIndex(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`pg_index[\s\S]*indisunique`).WillReturnRows(eventsCodeUniqueIndexRows())
}

func expectMixedSlowSchemaMock(mock sqlmock.Sqlmock) {
	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "events", int64(1)).
		AddRow("public", "logs", int64(1)).
		AddRow("public", "notes", int64(1)).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "events", "code", "text", "NO", 1, false).
		AddRow("public", "logs", "note", "text", "YES", 1, false).
		AddRow("public", "notes", "body", "text", "YES", 1, false).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	expectEventsCodeUniqueIndex(mock)
}

func assertFallbackWarningsInOrder(t *testing.T, stderr string, qualified ...string) {
	t.Helper()
	var want []string
	for _, q := range qualified {
		want = append(want, "warning: table \""+q+"\" has no safe key; using non-resumable normal streaming")
	}
	pos := 0
	for _, line := range want {
		idx := strings.Index(stderr[pos:], line)
		if idx < 0 {
			t.Fatalf("stderr missing warning %q in order:\n%s", line, stderr)
		}
		if strings.Count(stderr, line) != 1 {
			t.Fatalf("stderr should contain %q exactly once:\n%s", line, stderr)
		}
		pos += idx + len(line)
	}
}

func TestDumpSlowMixedDispatchAndWarnings(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	expectMixedSlowSchemaMock(mock)

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("a"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"note"}).AddRow("n"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow("b"))
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	stderr := captureStderr(func() {
		err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSlowConnection(), WithProvenance(Provenance{}))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	assertFallbackWarningsInOrder(t, stderr, "public.logs", "public.notes")
	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provenance == nil {
		t.Fatal("expected provenance")
	}
	assertStrategyRecords(t, meta.Provenance.Strategies,
		[]string{"public.events", "public.logs", "public.notes", "public.users"},
		[]KeyStrategy{KeyStrategyUniqueIndex, KeyStrategyNormalStream, KeyStrategyNormalStream, KeyStrategyPrimaryKey},
		[]bool{true, false, false, true},
	)
	for _, name := range []string{"logs", "notes", "events"} {
		if _, err := os.Stat(slowCheckpointPath(dir, db.Table{Schema: "public", Name: name})); err == nil {
			t.Fatalf("%s should not leave checkpoint", name)
		}
	}
}

func TestDumpChunkUniqueAndPKDispatch(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "events", int64(1)).
		AddRow("public", "orders", int64(1)).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "events", "code", "text", "NO", 1, false).
		AddRow("public", "orders", "id", "integer", "NO", 1, true).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	expectEventsCodeUniqueIndex(mock)

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("a"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))

	stderr := captureStderr(func() {
		err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProvenance(Provenance{}),
			WithChunkTables([]QualifiedTable{{Schema: "public", Name: "events"}}))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr, "no safe key") {
		t.Fatalf("chunk-only unrequested tables should not warn: %q", stderr)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provenance == nil || meta.Provenance.ChunkTables == nil {
		t.Fatal("expected chunk_tables provenance")
	}
	if len(meta.Provenance.ChunkTables.Requested) != 1 {
		t.Fatalf("requested = %v", meta.Provenance.ChunkTables.Requested)
	}
	if len(meta.Provenance.ChunkTables.Chunked) != 1 || meta.Provenance.ChunkTables.Chunked[0] != "public.events" {
		t.Fatalf("chunked = %v", meta.Provenance.ChunkTables.Chunked)
	}
	assertStrategyRecords(t, meta.Provenance.Strategies,
		[]string{"public.events"},
		[]KeyStrategy{KeyStrategyUniqueIndex},
		[]bool{true},
	)
}

func TestDumpSlowPlusChunkUsesGlobalPlans(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	expectMixedSlowSchemaMock(mock)

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("a"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"note"}).AddRow("n"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow("b"))
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	stderr := captureStderr(func() {
		err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSlowConnection(), WithProvenance(Provenance{}),
			WithChunkTables([]QualifiedTable{{Schema: "public", Name: "users"}}))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	assertFallbackWarningsInOrder(t, stderr, "public.logs", "public.notes")

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provenance == nil || meta.Provenance.ChunkTables == nil || len(meta.Provenance.ChunkTables.Requested) != 1 ||
		meta.Provenance.ChunkTables.Requested[0].Normalized != "public.users" {
		t.Fatalf("chunk provenance = %+v", meta.Provenance)
	}
	assertStrategyRecords(t, meta.Provenance.Strategies,
		[]string{"public.events", "public.logs", "public.notes", "public.users"},
		[]KeyStrategy{KeyStrategyUniqueIndex, KeyStrategyNormalStream, KeyStrategyNormalStream, KeyStrategyPrimaryKey},
		[]bool{true, false, false, true},
	)
	if len(meta.Provenance.ChunkTables.Chunked) != 1 || meta.Provenance.ChunkTables.Chunked[0] != "public.users" {
		t.Fatalf("chunked = %v", meta.Provenance.ChunkTables.Chunked)
	}
}

func TestDumpChunkOnlyRequestedFallbackWarning(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "logs", int64(1)).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "logs", "note", "text", "YES", 1, false).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	emptyUniqueIndexMock(mock)

	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"note"}).AddRow("n"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	stderr := captureStderr(func() {
		err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProvenance(Provenance{}),
			WithChunkTables([]QualifiedTable{{Schema: "public", Name: "logs"}}))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	assertFallbackWarningsInOrder(t, stderr, "public.logs")
	if _, err := os.Stat(slowCheckpointPath(dir, db.Table{Schema: "public", Name: "logs"})); err == nil {
		t.Fatal("fallback chunk table should not create checkpoint")
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provenance == nil {
		t.Fatal("expected provenance")
	}
	assertStrategyRecords(t, meta.Provenance.Strategies,
		[]string{"public.logs"},
		[]KeyStrategy{KeyStrategyNormalStream},
		[]bool{false},
	)
}

func TestDumpLegacyArtifactGuardBehavior(t *testing.T) {
	t.Run("resumable_rejects_before_stream", func(t *testing.T) {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer sqlDB.Close()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "events.ckpt.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}

		tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
			AddRow("public", "events", int64(1)).
			AddRow("tenant", "events", int64(1))
		mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1, \$2\)`).
			WithArgs("public", "tenant").
			WillReturnRows(tablesRows)
		colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
			AddRow("public", "events", "id", "integer", "NO", 1, true).
			AddRow("tenant", "events", "id", "integer", "NO", 1, true)
		mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public", "tenant").WillReturnRows(colsRows)
		fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
		mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public", "tenant").WillReturnRows(fksRows)
		emptyUniqueIndexMock(mock)

		err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSlowConnection(),
			WithSchemas([]string{"public", "tenant"}))
		if err == nil || !strings.Contains(err.Error(), "ambiguous legacy slow artifact") {
			t.Fatalf("err = %v, want ambiguous legacy rejection before streaming", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("fallback_chunk_skips_guard", func(t *testing.T) {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer sqlDB.Close()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "events.ckpt.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}

		tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
			AddRow("public", "events", int64(1)).
			AddRow("public", "logs", int64(1)).
			AddRow("tenant", "events", int64(1))
		mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1, \$2\)`).
			WithArgs("public", "tenant").
			WillReturnRows(tablesRows)
		colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
			AddRow("public", "events", "id", "integer", "NO", 1, true).
			AddRow("public", "logs", "note", "text", "YES", 1, false).
			AddRow("tenant", "events", "id", "integer", "NO", 1, true)
		mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public", "tenant").WillReturnRows(colsRows)
		fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
		mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public", "tenant").WillReturnRows(fksRows)
		emptyUniqueIndexMock(mock)

		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(sqlmock.NewRows([]string{"note"}).AddRow("n"))
		mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))

		stderr := captureStderr(func() {
			err = Dump(context.Background(), sqlDB, dir, WithoutSequences(),
				WithSchemas([]string{"public", "tenant"}),
				WithChunkTables([]QualifiedTable{{Schema: "public", Name: "logs"}}))
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		assertFallbackWarningsInOrder(t, stderr, "public.logs")
	})
}

var errTestPlanValidator = errors.New("test plan validator rejection")

func TestValidateDumpOptionsPlanValidatorIncompatibleModes(t *testing.T) {
	validator := func(PreparedDumpPlan) error { return nil }
	tests := []struct {
		name string
		cfg  config
		want string
	}{
		{
			name: "parallel",
			cfg:  config{workers: 2, planValidator: validator},
			want: "dump plan validation",
		},
		{
			name: "subset",
			cfg:  config{subset: &SubsetConfig{}, planValidator: validator},
			want: "dump plan validation",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDumpOptions(&tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q incompatibility", err, tc.want)
			}
		})
	}
}

func assertNoDumpOutputArtifacts(t *testing.T, dir string, resumable ...db.Table) {
	t.Helper()
	for _, name := range []string{"metadata.json", "metadata.json.tmp"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("%s should not exist on validator failure", name)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "data")); err == nil && len(entries) > 0 {
		t.Fatalf("data directory should be empty or absent, got %d entries", len(entries))
	}
	for _, table := range resumable {
		tables := []db.Table{table}
		assignDataFiles(tables)
		for _, path := range []string{
			slowCheckpointPath(dir, table),
			slowCheckpointPath(dir, table) + ".tmp",
			tableDataPath(dir, tables[0]),
			tableDataPath(dir, tables[0]) + ".tmp",
		} {
			if _, err := os.Stat(path); err == nil {
				t.Fatalf("%s should not exist on validator failure", path)
			}
		}
	}
}

func TestDumpPlanValidatorSeesCanonicalStrategies(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	expectMixedSlowSchemaMock(mock)

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("a"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"note"}).AddRow("n"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow("b"))
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	var seen PreparedDumpPlan
	calls := 0
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSlowConnection(), WithProvenance(Provenance{}),
		WithPlanValidator(func(plan PreparedDumpPlan) error {
			calls++
			seen = plan
			return nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("validator calls = %d, want 1", calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	assertStrategyRecords(t, seen.Strategies,
		[]string{"public.events", "public.logs", "public.notes", "public.users"},
		[]KeyStrategy{KeyStrategyUniqueIndex, KeyStrategyNormalStream, KeyStrategyNormalStream, KeyStrategyPrimaryKey},
		[]bool{true, false, false, true},
	)

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen.Strategies, meta.Provenance.Strategies) {
		t.Fatalf("validator strategies = %+v, provenance = %+v", seen.Strategies, meta.Provenance.Strategies)
	}
}

func TestDumpPlanValidatorAfterSelection(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	expectMixedSlowSchemaMock(mock)

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("a"))
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	policy := SelectionPolicy{
		Includes: []SelectorEntry{
			{Table: QualifiedTable{Schema: "public", Name: "events"}},
			{Table: QualifiedTable{Schema: "public", Name: "users"}},
		},
	}

	var seen PreparedDumpPlan
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSlowConnection(), WithProvenance(Provenance{}),
		WithTableSelection(policy, nil),
		WithPlanValidator(func(plan PreparedDumpPlan) error {
			seen = plan
			return nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	assertStrategyRecords(t, seen.Strategies,
		[]string{"public.events", "public.users"},
		[]KeyStrategy{KeyStrategyUniqueIndex, KeyStrategyPrimaryKey},
		[]bool{true, true},
	)
}

func TestDumpPlanValidatorErrorBeforeOutput(t *testing.T) {
	t.Run("serial_tx", func(t *testing.T) {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer sqlDB.Close()

		dir := t.TempDir()
		mock.ExpectBegin()

		tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
			AddRow("public", "users", int64(1))
		mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
			WithArgs("public").
			WillReturnRows(tablesRows)

		colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
			AddRow("public", "users", "id", "integer", "NO", 1, true)
		mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

		fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
		mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

		emptyUniqueIndexMock(mock)
		mock.ExpectRollback()

		err = Dump(context.Background(), sqlDB, dir, WithoutSequences(),
			WithPlanValidator(func(PreparedDumpPlan) error { return errTestPlanValidator }))
		if err == nil {
			t.Fatal("expected validator error")
		}
		if !errors.Is(err, errTestPlanValidator) {
			t.Fatalf("error = %v, want %v", err, errTestPlanValidator)
		}
		assertNoDumpOutputArtifacts(t, dir, db.Table{Schema: "public", Name: "users"})
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("slow_no_tx", func(t *testing.T) {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer sqlDB.Close()

		dir := t.TempDir()
		expectMixedSlowSchemaMock(mock)

		err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSlowConnection(),
			WithPlanValidator(func(PreparedDumpPlan) error { return errTestPlanValidator }))
		if err == nil {
			t.Fatal("expected validator error")
		}
		if !errors.Is(err, errTestPlanValidator) {
			t.Fatalf("error = %v, want %v", err, errTestPlanValidator)
		}
		assertNoDumpOutputArtifacts(t, dir,
			db.Table{Schema: "public", Name: "events"},
			db.Table{Schema: "public", Name: "users"},
		)
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDumpPlanValidatorDefensiveCopy(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	expectMixedSlowSchemaMock(mock)

	eventsTable := db.Table{
		Schema:        "public",
		Name:          "events",
		Columns:       []db.Column{{Name: "code", DataType: "text", OrdinalPosition: 1}},
		UniqueIndexes: streamUniqueTable().UniqueIndexes,
	}
	mock.ExpectQuery(slowQueryPattern(eventsTable, DefaultSlowChunkSize, 0)).
		WithoutArgs().
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("a"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"note"}).AddRow("n"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow("b"))
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithSlowConnection(), WithProvenance(Provenance{}),
		WithPlanValidator(func(plan PreparedDumpPlan) error {
			plan.Strategies[0].KeyColumns[0] = "mutated"
			plan.Strategies = append(plan.Strategies, TableStrategyRecord{Table: "public.extra"})
			return nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provenance.Strategies[0].KeyColumns[0] != "code" {
		t.Fatalf("provenance key column mutated: %+v", meta.Provenance.Strategies[0])
	}
	if len(meta.Provenance.Strategies) != 4 {
		t.Fatalf("provenance strategies = %d, want 4", len(meta.Provenance.Strategies))
	}
}

func TestDumpPlanValidatorChunkScopedStrategies(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "events", int64(1)).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "events", "code", "text", "NO", 1, false).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	expectEventsCodeUniqueIndex(mock)

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("a"))
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	var seen PreparedDumpPlan
	calls := 0
	err = Dump(context.Background(), sqlDB, dir, WithoutSequences(), WithProvenance(Provenance{}),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "events"}}),
		WithPlanValidator(func(plan PreparedDumpPlan) error {
			calls++
			seen = plan
			return nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("validator calls = %d, want 1", calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	assertStrategyRecords(t, seen.Strategies,
		[]string{"public.events"},
		[]KeyStrategy{KeyStrategyUniqueIndex},
		[]bool{true},
	)
}
