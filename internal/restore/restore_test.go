package restore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func writeFixtureDump(t *testing.T, dir string) {
	t.Helper()
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables: []db.Table{
			{
				Schema:  "public",
				Name:    "users",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"id": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplySchemaSQLUsesOnErrorStop(t *testing.T) {
	origLookPath := psqlLookPath
	origRunPSQLSchema := runPSQLSchema
	defer func() {
		psqlLookPath = origLookPath
		runPSQLSchema = origRunPSQLSchema
	}()

	psqlLookPath = func(file string) (string, error) { return "/bin/" + file, nil }

	var gotArgs []string
	var gotEnv []string
	runPSQLSchema = func(ctx context.Context, args []string, env []string, stderr *strings.Builder) error {
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.sql"), []byte("CREATE TABLE public.users (id integer);"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := applySchemaSQL(context.Background(), "postgres://u:secret@localhost/db", dir); err != nil {
		t.Fatal(err)
	}

	if len(gotArgs) != 5 {
		t.Fatalf("args = %v", gotArgs)
	}
	if !reflect.DeepEqual(gotArgs[:2], []string{"-v", "ON_ERROR_STOP=1"}) {
		t.Fatalf("args = %v, want ON_ERROR_STOP first", gotArgs)
	}
	if gotArgs[2] != "-f" {
		t.Fatalf("args = %v, want -f", gotArgs)
	}
	if strings.Contains(gotArgs[4], "secret") {
		t.Fatalf("clean DSN leaked password: %v", gotArgs)
	}
	if !containsString(gotEnv, "PGPASSWORD=secret") {
		t.Fatalf("env missing PGPASSWORD: %v", gotEnv)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestRestoreFullFlow(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)
	dataFile := "data/7075626c6963.7573657273.ndjson"
	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	meta.Tables[0].DataFile = &dataFile
	if err := os.Mkdir(filepath.Join(dir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "users.ndjson"), filepath.Join(dir, dataFile)); err != nil {
		t.Fatal(err)
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), metaData, 0o600); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	// Schema load happens outside transaction now.
	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	mock.ExpectBegin()

	mock.ExpectExec(`INSERT INTO "public"."users"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).
			AddRow("public", "users", "id"))
	mock.ExpectExec(`SELECT CASE WHEN m\.max_value IS NULL THEN NULL ELSE setval\(pg_get_serial_sequence\('"public"\."users"', 'id'\), m\.max_value, true\) END`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := Restore(context.Background(), sqlDB, dir); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsEmptyDumpBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": []
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := Restore(context.Background(), sqlDB, dir); !IsEmptyDumpError(err) {
		t.Fatalf("error = %v, want EmptyDumpError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("empty dump should reject before DB access: %v", err)
	}
}

func TestRestoreRejectsNoDataFilesBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [
    {"name": "users", "columns": [{"name": "id"}]},
    {"name": "orders", "columns": [{"name": "id"}]}
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := Restore(context.Background(), sqlDB, dir); !IsNoDataFilesError(err) {
		t.Fatalf("error = %v, want NoDataFilesError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no-data-files dump should reject before DB access: %v", err)
	}
}

func TestRestoreRejectsReplaceWithoutTransactionBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := Restore(context.Background(), sqlDB, dir, WithReplace(), WithoutTransaction()); err == nil || !strings.Contains(err.Error(), "requires transactions") {
		t.Fatalf("error = %v, want replace transaction gate", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("replace gate mutated SQL state: %v", err)
	}
}

func TestRestoreMissingSchemaDefaultRejectsBeforeSchemaApply(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}))
	if err := Restore(context.Background(), sqlDB, dir); err == nil || !strings.Contains(err.Error(), "table \"users\"") {
		t.Fatalf("error = %v, want missing schema rejection", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreMissingSchemaWithoutTransactionAppliesSchema(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)
	origApply := restoreApplySchemaSQL
	defer func() { restoreApplySchemaSQL = origApply }()
	applied := false
	restoreApplySchemaSQL = func(context.Context, string, string) error {
		applied = true
		return nil
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}))
	_ = Restore(context.Background(), sqlDB, dir, WithTrustedSchemaSQL(), WithoutTransaction())
	if !applied {
		t.Fatal("expected explicit non-transactional schema application")
	}
}

func TestRestoreWithSchemasUsesINFilter(t *testing.T) {
	dir := t.TempDir()
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "multi",
		Tables: []db.Table{
			{
				Schema:  "app",
				Name:    "orders",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"id": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orders.ndjson"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("app", "orders", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("app").
		WillReturnRows(tablesRows)
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("app", "orders", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("app").WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("app").WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "app"."orders"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	if err := Restore(context.Background(), sqlDB, dir, WithSchemas([]string{"app"})); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreWithProgressCallbacks(t *testing.T) {
	dir := t.TempDir()

	// Write a 2-table dump fixture.
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables: []db.Table{
			{
				Schema:  "public",
				Name:    "alpha",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
			{
				Schema:  "public",
				Name:    "beta",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		line, err := json.Marshal(map[string]any{"id": float64(1)})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ndjson"), append(line, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "alpha", int64(0)).
		AddRow("public", "beta", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	// LoadPostgresSchemas queries columns and FKs per table; order is not guaranteed.
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "alpha", "id", "integer", "NO", 1, true).
		AddRow("public", "beta", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WillReturnRows(fksRows)

	mock.ExpectBegin()

	for range 2 {
		mock.ExpectExec(`INSERT INTO`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))
	}

	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	var events []ProgressEvent
	if err := Restore(context.Background(), sqlDB, dir, WithProgress(func(ev ProgressEvent) {
		events = append(events, ev)
	})); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// 2 tables × 2 events (start + end) = 4
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}

	for i, ev := range events {
		if ev.Total != 2 {
			t.Fatalf("event[%d] Total = %d, want 2", i, ev.Total)
		}
		if ev.Elapsed <= 0 {
			t.Fatalf("event[%d] Elapsed = %v, want > 0", i, ev.Elapsed)
		}
	}

	// Verify monotonic Current: 1,1,2,2
	wantCurrent := []int{1, 1, 2, 2}
	for i, ev := range events {
		if ev.Current != wantCurrent[i] {
			t.Fatalf("event[%d] Current = %d, want %d", i, ev.Current, wantCurrent[i])
		}
	}

	// Verify phases
	wantPhases := []string{"table_start", "table_end", "table_start", "table_end"}
	for i, ev := range events {
		if ev.Phase != wantPhases[i] {
			t.Fatalf("event[%d] Phase = %q, want %q", i, ev.Phase, wantPhases[i])
		}
	}
}

func TestRestoreSilentByDefault(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"."users"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	var called bool
	if err := Restore(context.Background(), sqlDB, dir, WithProgress(func(ev ProgressEvent) {
		called = true
	})); err != nil {
		t.Fatal(err)
	}
	// WithProgress IS set, so callback should be called.
	if !called {
		t.Fatal("expected progress callback to be called")
	}
}

func TestRestoreWithoutProgressSilent(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"."users"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	// No WithProgress — restore should complete silently.
	if err := Restore(context.Background(), sqlDB, dir); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreDuplicateKeyRollsBack(t *testing.T) {
	dir := t.TempDir()
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables: []db.Table{
			{
				Schema:  "public",
				Name:    "users",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	ndjson := []byte(`{"id":1}` + "\n" + `{"id":1}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), ndjson, 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"."users"`).WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "public"."users"`).WithArgs(int64(1)).
		WillReturnError(errors.New("duplicate key value violates unique constraint"))

	mock.ExpectRollback()

	err = Restore(context.Background(), sqlDB, dir)
	if err == nil {
		t.Fatal("expected duplicate insert error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreProgressFailurePreservesSemantics verifies that when WithProgress(fn)
// is configured and a restore operation fails, the progress callback is invoked
// (confirming progress events are emitted) AND the original error is returned
// (confirming failure semantics are preserved).
func TestRestoreProgressFailurePreservesSemantics(t *testing.T) {
	dir := t.TempDir()
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables: []db.Table{
			{
				Schema:  "public",
				Name:    "users",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	ndjson := []byte(`{"id":1}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), ndjson, 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	// Simulate a failure on INSERT: progress callback fires for table_start,
	// then the insert fails and the transaction rolls back.

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"."users"`).WithArgs(int64(1)).
		WillReturnError(errors.New("connection refused"))

	mock.ExpectRollback()

	var events []ProgressEvent
	err = Restore(context.Background(), sqlDB, dir, WithProgress(func(ev ProgressEvent) {
		events = append(events, ev)
	}))

	// The error must propagate regardless of progress being configured.
	if err == nil {
		t.Fatal("expected restore error when WithProgress is configured")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// At least the table_start event should have been emitted before the failure.
	if len(events) == 0 {
		t.Fatal("expected at least one progress event before failure")
	}

	// The first event should be table_start for "users".
	if events[0].Phase != "table_start" || events[0].Table != "users" {
		t.Fatalf("first event = %+v, want table_start for users", events[0])
	}
}

func categoryAndItemTables() (db.Table, db.Table) {
	categories := db.Table{
		Schema: "public",
		Name:   "categories",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1},
		},
	}
	items := db.Table{
		Schema: "public",
		Name:   "items",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1},
		},
		ForeignKeys: []db.ForeignKey{
			{
				ConstraintName:        "items_category_id_fkey",
				ColumnName:            "category_id",
				ReferencedTableSchema: "public",
				ReferencedTableName:   "categories",
				ReferencedColumnName:  "id",
			},
		},
	}
	return categories, items
}

// TestRestoreSortsByForeignKey verifies that Restore reorders tables by FK
// dependency (parents before children) even when metadata lists them in the
// wrong order. If B has FK to A, A must be inserted first regardless of
// metadata.json order.
func TestRestoreSortsByForeignKey(t *testing.T) {
	dir := t.TempDir()

	// Child table "items" has FK referencing parent table "categories".
	// Metadata lists child first (wrong order); SortTables must fix it.
	categories, items := categoryAndItemTables()

	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		// Wrong order: child before parent.
		Tables: []db.Table{items, categories},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"categories", "items"} {
		if err := os.WriteFile(filepath.Join(dir, name+".ndjson"), []byte("{\"id\":1}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	// Inner implementation queries tables in sorted order (categories, items),
	// so mock the schema introspection for both.
	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "categories", int64(0)).
		AddRow("public", "items", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	// LoadPostgresSchemas queries columns + FKs per table, order non-deterministic.
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "categories", "id", "integer", "NO", 1, true).
		AddRow("public", "items", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WillReturnRows(fksRows)

	// INSERTs must be categories first (parent), then items (child).
	// If sorting didn't happen, items would be first and mock fails.

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"."categories"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "public"."items"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	if err := Restore(context.Background(), sqlDB, dir); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreReplaceTruncatesChildrenBeforeParents(t *testing.T) {
	dir := t.TempDir()
	categories, items := categoryAndItemTables()

	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables:      []db.Table{items, categories},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"categories", "items"} {
		if err := os.WriteFile(filepath.Join(dir, name+".ndjson"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "categories", int64(0)).
		AddRow("public", "items", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "categories", "id", "integer", "NO", 1, true).
		AddRow("public", "items", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`TRUNCATE TABLE "public"\."categories", "public"\."items"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	if err := Restore(context.Background(), sqlDB, dir, WithReplace()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreInvokesSequenceRestore(t *testing.T) {
	dir := t.TempDir()
	lastVal := int64(99)
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables: []db.Table{
			{
				Schema:  "public",
				Name:    "users",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
		},
		Sequences: []dump.SequenceState{
			{Schema: "public", Name: "users_id_seq", LastValue: &lastVal, StartValue: 1, IsCalled: true},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"id": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"."users"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))

	// Sequences run inside the main transaction before commit.
	mock.ExpectQuery(`SELECT tbl_ns.nspname, tbl.relname, a.attname`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column"}).AddRow("public", "users", "id"))
	// Monotonic guard: current value (1) < dump value (99) → proceed.
	mock.ExpectQuery(`SELECT last_value, is_called`).WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(1, true))
	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 99, true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	if err := Restore(context.Background(), sqlDB, dir); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequenceFailureRollsBackMainTransaction(t *testing.T) {
	dir := t.TempDir()
	lastVal := int64(99)
	meta := dump.Metadata{
		GeneratedAt: "2026-06-01T00:00:00Z",
		Schema:      "public",
		Tables: []db.Table{
			{
				Schema:  "public",
				Name:    "users",
				Columns: []db.Column{{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1}},
			},
		},
		Sequences: []dump.SequenceState{
			{Schema: "public", Name: "users_id_seq", LastValue: &lastVal, StartValue: 1, IsCalled: true},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"id": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"\."users"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT tbl_ns.nspname, tbl.relname, a.attname`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column"}).AddRow("public", "users", "id"))
	// Monotonic guard: current value (1) < dump value (99) → proceed.
	mock.ExpectQuery(`SELECT last_value, is_called`).WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(1, true))
	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 99, true\)`).
		WillReturnError(errors.New("setval denied"))
	mock.ExpectRollback()

	err = Restore(context.Background(), sqlDB, dir)
	if err == nil {
		t.Fatal("expected sequence error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSyncSequenceFailureRollsBackMainTransaction(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(0))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)
	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)
	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"\."users"`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).
			AddRow("public", "users", "id"))
	mock.ExpectExec(`SELECT CASE WHEN m\.max_value IS NULL THEN NULL ELSE setval\(pg_get_serial_sequence`).WillReturnError(errors.New("setval denied"))
	mock.ExpectRollback()

	err = Restore(context.Background(), sqlDB, dir)
	if err == nil {
		t.Fatal("expected sequence sync error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
