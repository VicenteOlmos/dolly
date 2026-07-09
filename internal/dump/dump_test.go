package dump

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
	if _, err := os.Stat(filepath.Join(dir, "users.ndjson")); os.IsNotExist(err) {
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
		AddRow("app", "orders", int64(1)).
		AddRow("billing", "invoices", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1, \$2\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("app", "billing").
		WillReturnRows(tablesRows)

	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("app", "orders", "id", "integer", "NO", 1, true).
		AddRow("billing", "invoices", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("app", "billing").WillReturnRows(allCols)
	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("app", "billing").WillReturnRows(allFks)
	for range 2 {
		streamRows := sqlmock.NewRows([]string{"id"})
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

	streamRows := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(streamRows)

	mock.ExpectCommit()

	err = Dump(context.Background(), sqlDB, dir, WithoutSequences())
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "empty_tbl.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty ndjson file, got %q", string(data))
	}

	metaPath := filepath.Join(dir, "metadata.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaData), "empty_tbl") {
		t.Fatal("metadata.json should contain empty_tbl")
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
	if _, err := os.Stat(filepath.Join(dir, "users.ndjson")); err != nil {
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
		data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
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
