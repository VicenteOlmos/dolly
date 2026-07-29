package dump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestQuoteSnapshotLiteral(t *testing.T) {
	valid := []struct {
		in   string
		want string
	}{
		{"123-456-789", "'123-456-789'"},
		{"00000004-00000273-1", "'00000004-00000273-1'"},
		{"DEAD-BEEF-42", "'DEAD-BEEF-42'"},
	}
	for _, tc := range valid {
		lit, err := quoteSnapshotLiteral(tc.in)
		if err != nil {
			t.Fatalf("id %q: %v", tc.in, err)
		}
		if lit != tc.want {
			t.Fatalf("id %q literal = %q, want %q", tc.in, lit, tc.want)
		}
	}
	for _, id := range []string{"", "abc", "123-456", "123-456-789-extra", "123;drop", "' OR 1=1", "GHI-456-1"} {
		if _, err := quoteSnapshotLiteral(id); err == nil {
			t.Fatalf("id %q should be rejected", id)
		}
	}
}

func TestValidateDumpWorkersPolicy(t *testing.T) {
	cases := []struct {
		name string
		cfg  config
		want string
	}{
		{"subset", config{workers: 2, subset: &SubsetConfig{}}, "subset"},
		{"no_tx", config{workers: 2, withoutTransaction: true}, "snapshot"},
		{"too_many", config{workers: 17}, "16"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDumpOptions(&tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(2)
	if err := validateParallelDumpOptions(&config{}, sqlDB, 2); err == nil || !strings.Contains(err.Error(), "max_open_conns") {
		t.Fatalf("error = %v", err)
	}
}

func mustStaging(t *testing.T) (dir, staging string) {
	t.Helper()
	dir = t.TempDir()
	var err error
	staging, err = os.MkdirTemp(dir, parallelStagingPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return dir, staging
}

func TestCleanupParallelArtifactsRemovesStagingAndMetadata(t *testing.T) {
	dir, staging := mustStaging(t)
	table := db.Table{Schema: "public", Name: "users", DataFile: strPtr("data/users.ndjson")}
	if err := os.WriteFile(parallelStagingPath(staging, table), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	metaTmp := filepath.Join(dir, "metadata.json.tmp")
	for _, path := range []string{metaTmp, filepath.Join(dir, "metadata.json")} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	final := tableDataPath(dir, table)
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanupParallelArtifacts(dir, staging, metaTmp, []db.Table{table})
	for _, path := range []string{staging, metaTmp, filepath.Join(dir, "metadata.json"), final} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("expected %q removed", path)
		}
	}
}

func TestPublishParallelArtifactsPublishesInPlanOrder(t *testing.T) {
	dir, staging := mustStaging(t)
	tables := []db.Table{{Schema: "public", Name: "a"}, {Schema: "public", Name: "b"}}
	assignDataFiles(tables)
	for _, table := range tables {
		if err := os.WriteFile(parallelStagingPath(staging, table), []byte(table.Name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	metaTmp := filepath.Join(dir, "metadata.json.tmp")
	if err := os.WriteFile(metaTmp, []byte(`{"schema":"public"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishParallelArtifacts(&ParallelPlan{outputDir: dir, stagingDir: staging, tables: tables, metaTmpPath: metaTmp}); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		data, err := os.ReadFile(tableDataPath(dir, table))
		if err != nil || string(data) != table.Name+"\n" {
			t.Fatalf("table %s: %v data=%q", table.Name, err, data)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging dir should be removed after publish")
	}
}

func TestParallelPlanCloseCleansUnpublished(t *testing.T) {
	dir, staging := mustStaging(t)
	metaTmp := filepath.Join(dir, "metadata.json.tmp")
	if err := os.WriteFile(metaTmp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&ParallelPlan{outputDir: dir, stagingDir: staging, metaTmpPath: metaTmp}).Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{staging, metaTmp} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %q removed", path)
		}
	}
}

func withTestWorkerSessions(t *testing.T, q querier) {
	t.Helper()
	old := parallelWorkerSessionOpener
	parallelWorkerSessionOpener = func(context.Context, *sql.DB, string) (querier, func() error, error) {
		return q, func() error { return nil }, nil
	}
	t.Cleanup(func() { parallelWorkerSessionOpener = old })
}

func withParallelStream(t *testing.T, fn func(context.Context, querier, db.Table, string, RowTransform) error) {
	t.Helper()
	old := parallelStreamTable
	parallelStreamTable = fn
	t.Cleanup(func() { parallelStreamTable = old })
}

func runTestParallelDump(t *testing.T, tables []db.Table, workers int, cfg config) error {
	t.Helper()
	dir, staging := mustStaging(t)
	withTestWorkerSessions(t, stubQuerier{})
	return runParallelDump(context.Background(), &ParallelPlan{
		cfg: cfg, outputDir: dir, tables: tables, stagingDir: staging, startedAt: time.Now(),
		coordinator: &snapshotCoordinator{snapshotLit: "'1-2-3'"},
	}, workers)
}

func TestIntrospectParallelPlanNoTablesBeforeMetadata(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"})
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)`).
		WithArgs("empty_schema").
		WillReturnRows(tablesRows)

	cfg := &config{schemas: []string{"empty_schema"}, skipSequences: true}
	_, _, err = introspectParallelPlan(context.Background(), sqlDB, cfg)
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIntrospectParallelPlanExcludeAllBeforeMetadata(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

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

	cfg := &config{
		schemas:       []string{"public"},
		skipSequences: true,
		selection: &SelectionPolicy{
			Excludes: []SelectorEntry{
				{Table: QualifiedTable{Schema: "public", Name: "users"}},
				{Table: QualifiedTable{Schema: "public", Name: "orders"}},
			},
		},
	}
	_, _, err = introspectParallelPlan(context.Background(), sqlDB, cfg)
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestParallelProgressCallbackSerialized(t *testing.T) {
	tables := make([]db.Table, 4)
	for i := range tables {
		tables[i] = db.Table{Schema: "public", Name: fmtTable(i)}
	}
	assignDataFiles(tables)
	var invocations int
	seen := make(map[string]struct{}, len(tables)*2)
	withParallelStream(t, func(context.Context, querier, db.Table, string, RowTransform) error { return nil })
	err := runTestParallelDump(t, tables, 2, config{
		onProgress: func(ev ProgressEvent) {
			invocations++
			if ev.Worker < 1 || ev.Worker > 2 || ev.Table == "" || !strings.Contains(ev.Table, ".") {
				t.Errorf("bad event: worker=%d table=%q", ev.Worker, ev.Table)
			}
			if ev.Phase != "table_start" && ev.Phase != "table_end" {
				t.Errorf("phase %q", ev.Phase)
			}
			if ev.Total != len(tables) {
				t.Errorf("total %d want %d", ev.Total, len(tables))
			}
			seen[ev.Phase+":"+ev.Table] = struct{}{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := len(tables) * 2
	if invocations != want || len(seen) != want {
		t.Fatalf("invocations=%d unique=%d want %d", invocations, len(seen), want)
	}
}

func TestParallelSchedulerRespectsWorkerCap(t *testing.T) {
	tables := make([]db.Table, 8)
	for i := range tables {
		tables[i] = db.Table{Schema: "public", Name: fmtTable(i)}
	}
	assignDataFiles(tables)
	var active, peak atomic.Int32
	withParallelStream(t, func(context.Context, querier, db.Table, string, RowTransform) error {
		cur := active.Add(1)
		for {
			oldPeak := peak.Load()
			if cur <= oldPeak || peak.CompareAndSwap(oldPeak, cur) {
				break
			}
		}
		defer active.Add(-1)
		return nil
	})
	if err := runTestParallelDump(t, tables, 3, config{}); err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 3 {
		t.Fatalf("peak concurrency = %d, want <= 3", peak.Load())
	}
}

func TestParallelDumpFailureCleansArtifacts(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "ok"}, {Schema: "public", Name: "fail"}}
	assignDataFiles(tables)
	dir, staging := mustStaging(t)
	metaTmp := filepath.Join(dir, "metadata.json.tmp")
	if err := os.WriteFile(metaTmp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	withParallelStream(t, func(_ context.Context, _ querier, table db.Table, path string, _ RowTransform) error {
		if table.Name == "fail" {
			return errParallelTestFailure
		}
		return os.WriteFile(path, []byte("x"), 0o600)
	})
	withTestWorkerSessions(t, stubQuerier{})
	plan := &ParallelPlan{cfg: config{}, outputDir: dir, tables: tables, stagingDir: staging, metaTmpPath: metaTmp, startedAt: time.Now(), coordinator: &snapshotCoordinator{snapshotLit: "'1-2-3'"}}
	if err := runParallelDump(context.Background(), plan, 2); !errors.Is(err, errParallelTestFailure) {
		t.Fatalf("error = %v", err)
	}
	cleanupParallelArtifacts(plan.outputDir, plan.stagingDir, plan.metaTmpPath, plan.tables)
	for _, path := range []string{metaTmp, filepath.Join(dir, "metadata.json")} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("expected %q removed", path)
		}
	}
}

type stubQuerier struct{}

func (stubQuerier) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}

var errParallelTestFailure = errors.New("parallel test failure")
var errParallelTestWorkerClose = errors.New("parallel test worker close failure")
var errParallelTestCoordinatorClose = errors.New("parallel test coordinator close failure")

func fmtTable(i int) string { return "t" + strconv.Itoa(i) }

func strPtr(s string) *string { return &s }

func withManualMonitorTicks(t *testing.T) chan struct{} {
	t.Helper()
	ch := make(chan struct{}, 8)
	old := parallelSnapshotMonitorTicks
	parallelSnapshotMonitorTicks = func(ctx context.Context) <-chan struct{} {
		out := make(chan struct{})
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ch:
					select {
					case out <- struct{}{}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return out
	}
	t.Cleanup(func() { parallelSnapshotMonitorTicks = old })
	return ch
}

func TestSnapshotMonitorLivenessCheckSurfacesBadConn(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT pg_export_snapshot").WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).AddRow("1-2-3"))
	coordinator, err := newSnapshotCoordinator(context.Background(), sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT 1").WillReturnError(driver.ErrBadConn)
	err = parallelSnapshotLivenessCheck(context.Background(), coordinator)
	if err == nil || !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("error = %v, want ErrBadConn", err)
	}
}

func TestSnapshotLifecycleFailureCancelsActiveWorkers(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "blocked"}}
	assignDataFiles(tables)
	dir, staging := mustStaging(t)
	metaTmp := filepath.Join(dir, "metadata.json.tmp")
	if err := os.WriteFile(metaTmp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	tickCh := withManualMonitorTicks(t)
	startedCh := make(chan struct{})
	withParallelStream(t, func(ctx context.Context, _ querier, _ db.Table, _ string, _ RowTransform) error {
		close(startedCh)
		<-ctx.Done()
		return ctx.Err()
	})
	withTestWorkerSessions(t, stubQuerier{})
	oldCheck := parallelSnapshotLivenessCheck
	parallelSnapshotLivenessCheck = func(ctx context.Context, c *snapshotCoordinator) error {
		return errParallelTestCoordinatorClose
	}
	t.Cleanup(func() { parallelSnapshotLivenessCheck = oldCheck })

	plan := &ParallelPlan{
		cfg:         config{},
		outputDir:   dir,
		tables:      tables,
		stagingDir:  staging,
		metaTmpPath: metaTmp,
		startedAt:   time.Now(),
		coordinator: &snapshotCoordinator{snapshotLit: "'1-2-3'"},
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- runParallelDump(context.Background(), plan, 1)
	}()
	<-startedCh
	tickCh <- struct{}{}
	err := <-errCh
	if !errors.Is(err, errParallelTestCoordinatorClose) {
		t.Fatalf("error = %v, want coordinator lifecycle failure", err)
	}
	cleanupParallelArtifacts(plan.outputDir, plan.stagingDir, plan.metaTmpPath, plan.tables)
	for _, path := range []string{metaTmp, filepath.Join(dir, "metadata.json")} {
		if _, statErr := os.Stat(path); statErr == nil {
			t.Fatalf("expected %q removed", path)
		}
	}
}

func TestParallelDumpJoinsWorkerCloseAndCoordinatorCloseErrors(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "fail"}}
	assignDataFiles(tables)
	dir, staging := mustStaging(t)
	withParallelStream(t, func(context.Context, querier, db.Table, string, RowTransform) error {
		return errParallelTestFailure
	})
	oldOpener := parallelWorkerSessionOpener
	parallelWorkerSessionOpener = func(context.Context, *sql.DB, string) (querier, func() error, error) {
		return stubQuerier{}, func() error { return errParallelTestWorkerClose }, nil
	}
	t.Cleanup(func() { parallelWorkerSessionOpener = oldOpener })

	plan := &ParallelPlan{
		cfg:         config{},
		outputDir:   dir,
		tables:      tables,
		stagingDir:  staging,
		startedAt:   time.Now(),
		coordinator: &snapshotCoordinator{snapshotLit: "'1-2-3'"},
	}
	err := runParallelDump(context.Background(), plan, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, errParallelTestFailure) {
		t.Fatalf("primary error missing: %v", err)
	}
	if !errors.Is(err, errParallelTestWorkerClose) {
		t.Fatalf("worker close error missing: %v", err)
	}
}
