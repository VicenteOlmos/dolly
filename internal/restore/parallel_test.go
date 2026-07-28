package restore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func parallelTestCfg(manifestPath string, workers int, opts ...Option) config {
	cfg := config{
		policy:             ConflictError,
		withoutTransaction: true,
		dsn:                "postgres://localhost/db",
		workers:            workers,
		partialStatePath:   manifestPath,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

func TestValidateParallelRestoreOptions(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	tests := []struct {
		name    string
		cfg     config
		wantErr string
	}{
		{name: "serial noop", cfg: parallelTestCfg(manifest, 1)},
		{name: "valid", cfg: parallelTestCfg(manifest, 4)},
		{name: "too many workers", cfg: parallelTestCfg(manifest, 17), wantErr: "between 1 and 16"},
		{name: "needs no tx", cfg: func() config { c := parallelTestCfg(manifest, 2); c.withoutTransaction = false; return c }(), wantErr: "WithoutTransaction"},
		{name: "needs dsn", cfg: func() config { c := parallelTestCfg(manifest, 2); c.dsn = ""; return c }(), wantErr: "requires DSN"},
		{name: "no replace", cfg: func() config { c := parallelTestCfg(manifest, 2); c.replace = true; return c }(), wantErr: "replace"},
		{name: "no schema sql", cfg: func() config { c := parallelTestCfg(manifest, 2); c.schemaSQL = true; return c }(), wantErr: "trusted schema"},
		{name: "copy policy", cfg: func() config { c := parallelTestCfg(manifest, 2); c.policy = ConflictSkip; return c }(), wantErr: "conflict policy error"},
		{name: "needs manifest", cfg: func() config { c := parallelTestCfg(manifest, 2); c.partialStatePath = ""; return c }(), wantErr: "manifest path"},
		{name: "reject traversal", cfg: parallelTestCfg("../state.json", 2), wantErr: "traverses parent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateParallelRestoreOptions(&tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestRestoreParallelRejectsUnsafeOptionsBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	origApply := restoreApplySchemaSQL
	restoreApplySchemaSQL = func(context.Context, string, string) error {
		t.Fatal("schema replay must not run for invalid parallel options")
		return nil
	}
	defer func() { restoreApplySchemaSQL = origApply }()

	err = Restore(context.Background(), sqlDB, dir,
		WithWorkers(2), WithoutTransaction(), WithDSN("postgres://localhost/db"),
		WithPartialStateManifest(filepath.Join(dir, "state.json")), WithReplace())
	if err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("err = %v, want replace rejection", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreParallelRejectsTrustedSchemaBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	origApply := restoreApplySchemaSQL
	restoreApplySchemaSQL = func(context.Context, string, string) error {
		t.Fatal("schema replay must not run for parallel restore")
		return nil
	}
	defer func() { restoreApplySchemaSQL = origApply }()

	err = Restore(context.Background(), sqlDB, dir,
		WithWorkers(2), WithoutTransaction(), WithDSN("postgres://localhost/db"),
		WithPartialStateManifest(filepath.Join(dir, "state.json")), WithTrustedSchemaSQL())
	if err == nil || !strings.Contains(err.Error(), "trusted schema") {
		t.Fatalf("err = %v, want trusted schema rejection", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreCycleDoesNotWriteManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	tables := []db.Table{
		{Schema: "public", Name: "a", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "b"}}},
		{Schema: "public", Name: "b", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "a"}}},
	}
	meta := dump.Metadata{Schema: "public", Tables: tables}
	for _, table := range tables {
		if err := os.WriteFile(filepath.Join(dir, table.Name+".ndjson"), []byte("{\"id\":1}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeMetadata(t, dir, meta)

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	err = Restore(context.Background(), sqlDB, dir,
		WithWorkers(2), WithoutTransaction(), WithDSN("postgres://localhost/db"),
		WithPartialStateManifest(manifest))
	if !errors.Is(err, ErrRestoreCycle) {
		t.Fatalf("err = %v, want cycle", err)
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatal("manifest should not exist after cycle rejection")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRunParallelRestore_levelOrderingAndWorkerCap(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	parent := db.Table{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", PrimaryKey: true}}}
	child := db.Table{Schema: "public", Name: "orders", Columns: []db.Column{{Name: "id", PrimaryKey: true}},
		ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "public", ReferencedTableName: "users"}}}
	meta := dump.Metadata{Schema: "public", Tables: []db.Table{child, parent}}
	dataPaths := []string{filepath.Join(dir, "orders.ndjson"), filepath.Join(dir, "users.ndjson")}
	levels, err := BuildRestoreLevels(meta.Tables)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var order []string
	var active atomic.Int32
	var maxActive atomic.Int32
	orig := parallelLoadTableCopy
	origSeq := parallelRestoreSequences
	origSync := parallelSyncSequences
	parallelLoadTableCopy = func(ctx context.Context, dsn string, table db.Table, path string) error {
		n := active.Add(1)
		for {
			cur := maxActive.Load()
			if n <= cur || maxActive.CompareAndSwap(cur, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		order = append(order, qualifiedLabel(table.Schema, table.Name))
		mu.Unlock()
		active.Add(-1)
		return nil
	}
	parallelRestoreSequences = func(context.Context, execQuerier, dump.Metadata, []string) error { return nil }
	parallelSyncSequences = func(context.Context, execQuerier, []string) error { return nil }
	defer func() {
		parallelLoadTableCopy = orig
		parallelRestoreSequences = origSeq
		parallelSyncSequences = origSync
	}()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	cfg := parallelTestCfg(manifest, 2)
	if err := runParallelRestore(context.Background(), &cfg, sqlDB, meta, dataPaths, levels, []string{"public"}, 2, time.Now()); err != nil {
		t.Fatal(err)
	}
	if maxActive.Load() > 2 {
		t.Fatalf("max active workers = %d, want <= 2", maxActive.Load())
	}
	if !reflect.DeepEqual(order[:1], []string{"public.users"}) {
		t.Fatalf("parent order = %v", order)
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatal("manifest should be removed on success")
	}
}

func TestRunParallelRestore_failFastAndManifestStates(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	users := db.Table{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", PrimaryKey: true}}}
	posts := db.Table{Schema: "public", Name: "posts", Columns: []db.Column{{Name: "id", PrimaryKey: true}}}
	meta := dump.Metadata{Schema: "public", Tables: []db.Table{users, posts}}
	dataPaths := []string{filepath.Join(dir, "users.ndjson"), filepath.Join(dir, "posts.ndjson")}
	levels := []RestoreLevel{
		{Tables: []string{"public.users"}},
		{Tables: []string{"public.posts"}},
	}

	orig := parallelLoadTableCopy
	parallelLoadTableCopy = func(_ context.Context, _ string, table db.Table, _ string) error {
		if table.Name == "users" {
			return errors.New("copy users failed")
		}
		return nil
	}
	defer func() { parallelLoadTableCopy = orig }()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	cfg := parallelTestCfg(manifest, 2)
	err = runParallelRestore(context.Background(), &cfg, sqlDB, meta, dataPaths, levels, nil, 2, time.Now())
	if err == nil || !strings.Contains(err.Error(), "copy users failed") {
		t.Fatalf("err = %v", err)
	}
	got, err := LoadPartialStateManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Failed) != 1 || got.Failed[0].Table != "public.users" {
		t.Fatalf("failed = %+v", got.Failed)
	}
	if len(got.Committed) != 0 {
		t.Fatalf("committed = %v", got.Committed)
	}
	if len(got.Pending) != 1 || got.Pending[0] != "public.posts" {
		t.Fatalf("pending = %v", got.Pending)
	}
}

func TestRunParallelRestore_copyFailureSurfacesManifestWriteError(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	users := db.Table{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", PrimaryKey: true}}}
	meta := dump.Metadata{Schema: "public", Tables: []db.Table{users}}
	dataPaths := []string{filepath.Join(dir, "users.ndjson")}
	levels := []RestoreLevel{{Tables: []string{"public.users"}}}

	origLoad := parallelLoadTableCopy
	origWrite := parallelWriteManifest
	var writeCount atomic.Int32
	parallelLoadTableCopy = func(_ context.Context, _ string, _ db.Table, _ string) error {
		return errors.New("copy users failed")
	}
	parallelWriteManifest = func(path string, m PartialStateManifest) error {
		if writeCount.Add(1) == 1 {
			return origWrite(path, m)
		}
		return errors.New("disk full")
	}
	defer func() {
		parallelLoadTableCopy = origLoad
		parallelWriteManifest = origWrite
	}()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	cfg := parallelTestCfg(manifest, 2)
	err = runParallelRestore(context.Background(), &cfg, sqlDB, meta, dataPaths, levels, nil, 2, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "copy users failed") || !strings.Contains(msg, "disk full") {
		t.Fatalf("err = %v, want both copy and manifest errors", err)
	}
	if _, statErr := os.Stat(manifest); statErr != nil {
		t.Fatal("manifest should be retained")
	}
}

func TestRunParallelRestore_sequenceGatingAndManifestRetention(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	table := db.Table{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", PrimaryKey: true}}}
	meta := dump.Metadata{Schema: "public", Tables: []db.Table{table}}
	dataPaths := []string{filepath.Join(dir, "users.ndjson")}
	levels := []RestoreLevel{{Tables: []string{"public.users"}}}

	origLoad := parallelLoadTableCopy
	origSeq := parallelRestoreSequences
	parallelLoadTableCopy = func(context.Context, string, db.Table, string) error { return nil }
	parallelRestoreSequences = func(context.Context, execQuerier, dump.Metadata, []string) error {
		return errors.New("setval denied")
	}
	defer func() {
		parallelLoadTableCopy = origLoad
		parallelRestoreSequences = origSeq
	}()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	cfg := parallelTestCfg(manifest, 2)
	err = runParallelRestore(context.Background(), &cfg, sqlDB, meta, dataPaths, levels, []string{"public"}, 2, time.Now())
	if err == nil || !strings.Contains(err.Error(), "restore sequences") {
		t.Fatalf("err = %v", err)
	}
	got, err := LoadPartialStateManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Committed, []string{"public.users"}) {
		t.Fatalf("committed = %v", got.Committed)
	}
}

func TestRunParallelRestore_dottedIdentifiers(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	parent := db.Table{Schema: "a.b", Name: "c"}
	child := db.Table{Schema: "a", Name: "b.c", ForeignKeys: []db.ForeignKey{{ReferencedTableSchema: "a.b", ReferencedTableName: "c"}}}
	meta := dump.Metadata{Schema: "a", Tables: []db.Table{child, parent}}
	levels, err := BuildRestoreLevels(meta.Tables)
	if err != nil {
		t.Fatal(err)
	}
	parentLabel := qualifiedLabel("a.b", "c")
	childLabel := qualifiedLabel("a", "b.c")
	if levels[0].Tables[0] != parentLabel || levels[1].Tables[0] != childLabel {
		t.Fatalf("levels = %+v", levels)
	}

	var loaded []string
	orig := parallelLoadTableCopy
	origSeq := parallelRestoreSequences
	origSync := parallelSyncSequences
	parallelLoadTableCopy = func(_ context.Context, _ string, table db.Table, _ string) error {
		loaded = append(loaded, qualifiedLabel(table.Schema, table.Name))
		return nil
	}
	parallelRestoreSequences = func(context.Context, execQuerier, dump.Metadata, []string) error { return nil }
	parallelSyncSequences = func(context.Context, execQuerier, []string) error { return nil }
	defer func() {
		parallelLoadTableCopy = orig
		parallelRestoreSequences = origSeq
		parallelSyncSequences = origSync
	}()

	dataPaths := []string{filepath.Join(dir, "child.ndjson"), filepath.Join(dir, "parent.ndjson")}
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	cfg := parallelTestCfg(manifest, 2)
	if err := runParallelRestore(context.Background(), &cfg, sqlDB, meta, dataPaths, levels, nil, 2, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, []string{parentLabel, childLabel}) {
		t.Fatalf("loaded = %v", loaded)
	}
}

func TestRestoreSerialWorkersUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, dir)
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).AddRow("public", "users", int64(0)))
	mock.ExpectQuery(`SELECT c\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true))
	mock.ExpectQuery(`SELECT tc\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}))
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "public"."users"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}))
	mock.ExpectCommit()

	var events []ProgressEvent
	err = Restore(context.Background(), sqlDB, dir, WithWorkers(1), WithProgress(func(ev ProgressEvent) {
		events = append(events, ev)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Worker != 0 || events[0].Table != "users" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParallelRestoreSourceNeverDisablesConstraints(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(filename)
	for _, file := range []string{"parallel.go", "restore.go", "load.go"} {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		if strings.Contains(body, "session_replication_role") {
			t.Fatalf("%s must not disable constraints via session_replication_role", file)
		}
	}
}

func writeMetadata(t *testing.T, dir string, meta dump.Metadata) {
	t.Helper()
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mockSchemaIntrospection(mock sqlmock.Sqlmock, tables []db.Table) {
	rows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"})
	for _, table := range tables {
		rows.AddRow(table.Schema, table.Name, int64(0))
	}
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(rows)
	cols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"})
	for _, table := range tables {
		cols.AddRow(table.Schema, table.Name, "id", "integer", "NO", 1, true)
	}
	mock.ExpectQuery(`SELECT c\.table_schema`).WillReturnRows(cols)
	mock.ExpectQuery(`SELECT tc\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}))
}
