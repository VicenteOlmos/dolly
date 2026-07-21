package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// mockCommandRunner records calls for verification.
type mockCommandRunner struct {
	runCalls    []runCall
	pipeCalls   []pipeCall
	runErr      error
	pipeErr     error
	runFn       func()
	pipeFn      func()
	writeOutput bool
}

type runCall struct {
	name string
	args []string
	env  map[string]string
}

type pipeCall struct {
	srcName string
	srcArgs []string
	dstName string
	dstArgs []string
}

func (m *mockCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	return m.RunWithEnv(ctx, nil, name, args...)
}

func (m *mockCommandRunner) RunWithEnv(ctx context.Context, env map[string]string, name string, args ...string) error {
	_ = ctx
	m.runCalls = append(m.runCalls, runCall{name: name, args: args, env: env})
	if m.writeOutput {
		fmt.Print("run stdout")
		fmt.Fprint(testStderr{}, "run stderr")
	}
	if m.runFn != nil {
		m.runFn()
	}
	return m.runErr
}

func (m *mockCommandRunner) Pipe(ctx context.Context, srcName string, srcArgs []string, dstName string, dstArgs []string) error {
	return m.PipeWithEnv(ctx, nil, srcName, srcArgs, dstName, dstArgs)
}

func (m *mockCommandRunner) PipeWithEnv(ctx context.Context, env map[string]string, srcName string, srcArgs []string, dstName string, dstArgs []string) error {
	_ = ctx
	m.pipeCalls = append(m.pipeCalls, pipeCall{srcName: srcName, srcArgs: srcArgs, dstName: dstName, dstArgs: dstArgs})
	if m.pipeFn != nil {
		m.pipeFn()
	}
	if m.writeOutput {
		fmt.Print("pipe stdout")
		fmt.Fprint(testStderr{}, "pipe stderr")
	}
	return m.pipeErr
}

type testStderr struct{}

func (testStderr) Write(p []byte) (int, error) {
	return os.Stderr.Write(p)
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		strategy    string
		wantName    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "template returns TemplateStrategy",
			strategy: "template",
			wantName: "template",
		},
		{
			name:     "schema-replay returns SchemaReplayStrategy",
			strategy: "schema-replay",
			wantName: "schema-replay",
		},
		{
			name:     "empty string defaults to schema-replay",
			strategy: "",
			wantName: "schema-replay",
		},
		{
			name:     "logical-stream returns CopyStreamStrategy",
			strategy: "logical-stream",
			wantName: "logical-stream",
		},
		{
			name:     "copy-stream alias returns CopyStreamStrategy",
			strategy: "copy-stream",
			wantName: "logical-stream",
		},
		{
			name:     "streaming-copy alias returns CopyStreamStrategy",
			strategy: "streaming-copy",
			wantName: "logical-stream",
		},
		{
			name:     "physical-backup returns ReplicationStrategy",
			strategy: "physical-backup",
			wantName: "physical-backup",
		},
		{
			name:     "replication alias returns ReplicationStrategy",
			strategy: "replication",
			wantName: "physical-backup",
		},
		{
			name:        "unknown strategy errors",
			strategy:    "unknown",
			wantErr:     true,
			errContains: "unknown clone strategy",
		},
		{
			name:        "unknown strategy lists canonical names",
			strategy:    "bogus",
			wantErr:     true,
			errContains: "logical-stream, physical-backup",
		},
		{
			name:        "production-scale returns unknown strategy",
			strategy:    "production-scale",
			wantErr:     true,
			errContains: "unknown clone strategy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strat, err := Resolve(tt.strategy, Options{})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strat.Name() != tt.wantName {
				t.Fatalf("got strategy name %q, want %q", strat.Name(), tt.wantName)
			}
		})
	}
}

func TestResolveWiresCommandRunner(t *testing.T) {
	mockRunner := &mockCommandRunner{}
	opts := Options{CommandRunner: mockRunner}

	strat, err := Resolve("schema-replay", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s, ok := strat.(*SchemaReplayStrategy); !ok || s.Runner != mockRunner {
		t.Fatal("schema-replay did not receive CommandRunner")
	}

	strat, err = Resolve("copy-stream", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s, ok := strat.(*CopyStreamStrategy); !ok || s.Runner != mockRunner {
		t.Fatal("copy-stream did not receive CommandRunner")
	}

	strat, err = Resolve("replication", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s, ok := strat.(*ReplicationStrategy); !ok || s.Runner != mockRunner {
		t.Fatal("replication did not receive CommandRunner")
	}
}

func TestSchemaReplayStrategyExecute(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}
	defer func() { lookPath = origLookPath }()

	origOpenDB := sqlOpenDB
	var openedDSNs []string
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		openedDSNs = append(openedDSNs, dsn)
		return nil, fmt.Errorf("mock db")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	origDump := dumpFunc
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		return nil
	}
	defer func() { dumpFunc = origDump }()

	origRestore := restoreFunc
	restoreFunc = func(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...restore.Option) error {
		return nil
	}
	defer func() { restoreFunc = origRestore }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	opts := Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
	}

	// sqlOpenDB returns an error, so the strategy will fail at open source.
	// We only care about validating the pipe call and dump/restore invocation order.
	err := strat.Execute(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error from mock db open")
	}

	if len(mockRunner.pipeCalls) != 1 {
		t.Fatalf("expected 1 pipe call, got %d", len(mockRunner.pipeCalls))
	}
	pc := mockRunner.pipeCalls[0]
	if pc.srcName != "pg_dump" {
		t.Fatalf("expected pg_dump, got %s", pc.srcName)
	}
	wantSrcArgs := []string{"--schema-only", "--no-owner", "--no-acl", "postgres://u@h-a:5432/db_src"}
	if !sliceEqual(pc.srcArgs, wantSrcArgs) {
		t.Fatalf("pg_dump args = %v, want %v", pc.srcArgs, wantSrcArgs)
	}
	if pc.dstName != "psql" {
		t.Fatalf("expected psql, got %s", pc.dstName)
	}
	wantDstArgs := []string{"-v", "ON_ERROR_STOP=1", "postgres://u@h-a:5432/db_clone"}
	if !sliceEqual(pc.dstArgs, wantDstArgs) {
		t.Fatalf("psql args = %v, want %v", pc.dstArgs, wantDstArgs)
	}
}

func TestSchemaReplayStrategyExecutePassesSanitizationDumpOpts(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		return mockDB, nil
	}
	defer func() { sqlOpenDB = origOpenDB }()

	var capturedDumpOpts []dump.Option
	origDump := dumpFunc
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		capturedDumpOpts = append([]dump.Option(nil), opts...)
		return nil
	}
	defer func() { dumpFunc = origDump }()

	origRestore := restoreFunc
	restoreFunc = func(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...restore.Option) error {
		return nil
	}
	defer func() { restoreFunc = origRestore }()

	origRestoreSeq := restoreSequencesFunc
	restoreSequencesFunc = func(ctx context.Context, srcDB, tgtDB *sql.DB) error { return nil }
	defer func() { restoreSequencesFunc = origRestoreSeq }()

	origListSchemas := listSchemaNamesFunc
	listSchemaNamesFunc = func(ctx context.Context, q *sql.DB) ([]string, error) {
		return []string{"public", "app"}, nil
	}
	defer func() { listSchemaNamesFunc = origListSchemas }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	opts := Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		Strategy:   "schema-replay",
		DumpOpts:   dump.SanitizationOptions(true),
	}

	if err := strat.Execute(context.Background(), opts); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rt := dump.InspectRowTransform(capturedDumpOpts...)
	if rt == nil {
		t.Fatal("schema-replay dump should receive sanitization row transform")
	}
	if got := dump.InspectSchemas(capturedDumpOpts...); !sliceEqual(got, []string{"public", "app"}) {
		t.Fatalf("schemas = %v, want public/app", got)
	}
}

func TestSchemaReplayStrategyKeepsExplicitDumpSchemas(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) { return mockDB, nil }
	defer func() { sqlOpenDB = origOpenDB }()

	listCalled := false
	origListSchemas := listSchemaNamesFunc
	listSchemaNamesFunc = func(ctx context.Context, q *sql.DB) ([]string, error) {
		listCalled = true
		return []string{"public", "app"}, nil
	}
	defer func() { listSchemaNamesFunc = origListSchemas }()

	var capturedDumpOpts []dump.Option
	origDump := dumpFunc
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		capturedDumpOpts = append([]dump.Option(nil), opts...)
		return nil
	}
	defer func() { dumpFunc = origDump }()

	origRestore := restoreFunc
	restoreFunc = func(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...restore.Option) error { return nil }
	defer func() { restoreFunc = origRestore }()

	origRestoreSeq := restoreSequencesFunc
	restoreSequencesFunc = func(ctx context.Context, srcDB, tgtDB *sql.DB) error { return nil }
	defer func() { restoreSequencesFunc = origRestoreSeq }()

	if err := (&SchemaReplayStrategy{Runner: &mockCommandRunner{}}).Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		DumpOpts:   []dump.Option{dump.WithSchemas([]string{"public"})},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if listCalled {
		t.Fatal("list schemas should not be called when schemas are explicit")
	}
	if got := dump.InspectSchemas(capturedDumpOpts...); !sliceEqual(got, []string{"public"}) {
		t.Fatalf("schemas = %v, want public", got)
	}
}

func TestSchemaReplayStrategyPipeFailure(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	mockRunner := &mockCommandRunner{pipeErr: errors.New("pipe broken")}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	err := strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "target",
		SkipCreate: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "replay schema") {
		t.Fatalf("error should mention replay schema: %v", err)
	}
	if !contains(err.Error(), "pipe broken") {
		t.Fatalf("error should mention pipe broken: %v", err)
	}
}

func TestSchemaReplayStrategyProgressSilencesCommandOutput(t *testing.T) {
	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		return nil, fmt.Errorf("mock db")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	var progress []string
	mockRunner := &mockCommandRunner{writeOutput: true}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	stdout, stderr := captureProcessOutput(t, func() {
		err := strat.Execute(context.Background(), Options{
			SourceDSN:  "postgres://u:p@h-a:5432/db_src",
			CloneName:  "db_clone",
			SkipCreate: true,
			ProgressFn: func(message string) { progress = append(progress, message) },
		})
		if err == nil {
			t.Fatal("expected error from mock db open")
		}
	})

	if stdout != "" || stderr != "" {
		t.Fatalf("captured stdout=%q stderr=%q, want both empty", stdout, stderr)
	}
	wantProgress := []string{"replaying schema"}
	if !sliceEqual(progress, wantProgress) {
		t.Fatalf("progress = %v, want %v", progress, wantProgress)
	}
	if len(mockRunner.pipeCalls) != 1 {
		t.Fatalf("expected 1 pipe call, got %d", len(mockRunner.pipeCalls))
	}
}

func TestSchemaReplayStrategyWithoutProgressKeepsCommandOutput(t *testing.T) {
	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		return nil, fmt.Errorf("mock db")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	mockRunner := &mockCommandRunner{writeOutput: true}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	stdout, stderr := captureProcessOutput(t, func() {
		_ = strat.Execute(context.Background(), Options{
			SourceDSN:  "postgres://u:p@h-a:5432/db_src",
			CloneName:  "db_clone",
			SkipCreate: true,
		})
	})

	if !strings.Contains(stdout, "pipe stdout") || !strings.Contains(stderr, "pipe stderr") {
		t.Fatalf("captured stdout=%q stderr=%q, want command output", stdout, stderr)
	}
}

func TestTemplateStrategyExecute(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()

	origOpen := sqlOpenDB
	var openedDSN string
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		openedDSN = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = origOpen }()

	mock.ExpectQuery(`SELECT count\(\*\) FROM pg_stat_activity`).
		WithArgs("db_src").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectExec(`CREATE DATABASE "db_clone" WITH TEMPLATE "db_src"`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	strat := &TemplateStrategy{}
	err = strat.Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h-a:5432/db_src",
		CloneName: "db_clone",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
	if !strings.Contains(openedDSN, "/postgres") {
		t.Fatalf("expected admin DSN to use postgres database, got %q", openedDSN)
	}
}

func TestTemplateStrategyExecuteQuotedNames(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()

	origOpen := sqlOpenDB
	var openedDSN string
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		openedDSN = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = origOpen }()

	sourceDB := `db"a`
	cloneName := `db"b`

	mock.ExpectQuery(`SELECT count\(\*\) FROM pg_stat_activity`).
		WithArgs(sourceDB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectExec(`CREATE DATABASE "db""b" WITH TEMPLATE "db""a"`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	strat := &TemplateStrategy{}
	err = strat.Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h-a:5432/" + sourceDB,
		CloneName: cloneName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
	if !strings.Contains(openedDSN, "/postgres") {
		t.Fatalf("expected admin DSN to use postgres database, got %q", openedDSN)
	}
}

func TestTemplateStrategyCrossInstance(t *testing.T) {
	strat := &TemplateStrategy{}
	err := strat.Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h-a:5432/db_src",
		CloneName: "db_clone",
		TargetDSN: "postgres://u:p@h-b:5432/db_b",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "same PostgreSQL instance") {
		t.Fatalf("error should mention same instance requirement: %v", err)
	}
	if !contains(err.Error(), "schema-replay") {
		t.Fatalf("error should suggest schema-replay: %v", err)
	}
}

func TestTemplateStrategyActiveConnections(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()

	origOpen := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		_ = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = origOpen }()

	mock.ExpectQuery(`SELECT count\(\*\) FROM pg_stat_activity`).
		WithArgs("db_src").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	strat := &TemplateStrategy{}
	err = strat.Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h-a:5432/db_src",
		CloneName: "db_clone",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "active connection(s)") {
		t.Fatalf("error should mention active connections: %v", err)
	}
	if !contains(err.Error(), "pg_terminate_backend") {
		t.Fatalf("error should hint pg_terminate_backend: %v", err)
	}
}

func TestTemplateStrategyCreateFailure(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()

	origOpen := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		_ = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = origOpen }()

	mock.ExpectQuery(`SELECT count\(\*\) FROM pg_stat_activity`).
		WithArgs("db_src").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectExec(`CREATE DATABASE "db_clone" WITH TEMPLATE "db_src"`).
		WillReturnError(errors.New("permission denied to create database"))

	strat := &TemplateStrategy{}
	err = strat.Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h-a:5432/db_src",
		CloneName: "db_clone",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "create database from template") {
		t.Fatalf("error should mention create database from template: %v", err)
	}
}

func TestReplicationStrategyExecute(t *testing.T) {
	targetDir := t.TempDir()
	// Use a fresh empty subdir as pg_basebackup target.
	dataDir := filepath.Join(targetDir, "data")

	mockRunner := &mockCommandRunner{}
	var progress []string
	sourceDSN := "postgres://repl:secret@db-host:5433/mydb"

	err := (&ReplicationStrategy{Runner: mockRunner}).Execute(context.Background(), Options{
		SourceDSN: sourceDSN,
		TargetDir: dataDir,
		ProgressFn: func(msg string) {
			progress = append(progress, msg)
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(mockRunner.runCalls) != 1 {
		t.Fatalf("expected 1 run call, got %d", len(mockRunner.runCalls))
	}
	call := mockRunner.runCalls[0]
	if call.name != "pg_basebackup" {
		t.Fatalf("command = %q, want pg_basebackup", call.name)
	}
	wantArgs := []string{"-h", "db-host", "-p", "5433", "-U", "repl", "-D", dataDir, "-Fp", "-Xs", "-P", "-v"}
	if !sliceEqual(call.args, wantArgs) {
		t.Fatalf("args = %v, want %v", call.args, wantArgs)
	}
	if call.env["PGPASSWORD"] != "secret" {
		t.Fatalf("PGPASSWORD = %q, want secret", call.env["PGPASSWORD"])
	}

	autoConf, err := os.ReadFile(filepath.Join(dataDir, "postgresql.auto.conf"))
	if err != nil {
		t.Fatalf("read postgresql.auto.conf: %v", err)
	}
	autoConfStr := string(autoConf)
	for _, sub := range []string{"host=db-host", "port=5433", "user=repl", "application_name=dolly_clone"} {
		if !contains(autoConfStr, sub) {
			t.Fatalf("postgresql.auto.conf missing %q: %s", sub, autoConfStr)
		}
	}
	if contains(autoConfStr, "password") || contains(autoConfStr, "secret") {
		t.Fatalf("postgresql.auto.conf must not contain password: %s", autoConfStr)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "standby.signal")); err != nil {
		t.Fatalf("standby.signal: %v", err)
	}

	if len(progress) < 2 {
		t.Fatalf("expected progress messages, got %v", progress)
	}
	last := progress[len(progress)-1]
	for _, sub := range []string{dataDir, "pg_ctl", "pg_isready", "entire cluster"} {
		if !contains(last, sub) {
			t.Fatalf("final progress missing %q: %s", sub, last)
		}
	}
}

func TestReplicationStrategyProgressEvent(t *testing.T) {
	targetDir := t.TempDir()
	dataDir := filepath.Join(targetDir, "data")

	mockRunner := &mockCommandRunner{}
	var events []ProgressEvent
	sourceDSN := "postgres://repl:secret@db-host:5433/mydb"

	err := (&ReplicationStrategy{Runner: mockRunner}).Execute(context.Background(), Options{
		SourceDSN: sourceDSN,
		TargetDir: dataDir,
		ProgressEvent: func(ev ProgressEvent) {
			events = append(events, ev)
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// 2 typed events: running_pg_basebackup + clone complete
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Phase != "running_pg_basebackup" {
		t.Fatalf("Phase = %q, want running_pg_basebackup", events[0].Phase)
	}
	if events[0].Current != 1 || events[0].Total != 1 {
		t.Fatalf("Current=%d Total=%d, want 1/1", events[0].Current, events[0].Total)
	}
	if events[0].Elapsed <= 0 {
		t.Fatalf("Elapsed = %v, want > 0", events[0].Elapsed)
	}
	// Second event is the completion message (same phase, same step index)
	if events[1].Phase != "running_pg_basebackup" {
		t.Fatalf("event[1] Phase = %q, want running_pg_basebackup", events[1].Phase)
	}
}

func TestReplicationStrategyProgressEventPrecedesDeprecated(t *testing.T) {
	targetDir := t.TempDir()
	dataDir := filepath.Join(targetDir, "data")

	mockRunner := &mockCommandRunner{}
	var typedEvents []ProgressEvent
	var stringMessages []string
	sourceDSN := "postgres://repl:secret@db-host:5433/mydb"

	err := (&ReplicationStrategy{Runner: mockRunner}).Execute(context.Background(), Options{
		SourceDSN: sourceDSN,
		TargetDir: dataDir,
		ProgressEvent: func(ev ProgressEvent) {
			typedEvents = append(typedEvents, ev)
		},
		ProgressFn: func(msg string) {
			stringMessages = append(stringMessages, msg)
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// When both are set, both callbacks fire for backward compatibility.
	// 2 events: running_pg_basebackup + clone complete
	if len(typedEvents) != 2 {
		t.Fatalf("typed events = %d, want 2", len(typedEvents))
	}
	// Deprecated string callback also fires (2 events with step text).
	if len(stringMessages) != 2 {
		t.Fatalf("string messages = %d, want 2 (both fire when both set)", len(stringMessages))
	}
}

func TestReplicationStrategyRetainsPartialTargetOnFailure(t *testing.T) {
	targetDir := t.TempDir()
	dataDir := filepath.Join(targetDir, "data")

	mockRunner := &mockCommandRunner{runErr: errors.New("disk full")}
	err := (&ReplicationStrategy{Runner: mockRunner}).Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h:5432/db",
		TargetDir: dataDir,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mockRunner.runErr) || !contains(err.Error(), dataDir) || !contains(err.Error(), "remove it explicitly") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("expected retained target dir, stat err = %v", err)
	}
}

func TestReplicationStrategyRetainsPartialTargetOnValidationFailure(t *testing.T) {
	targetDir := t.TempDir()
	dataDir := filepath.Join(targetDir, "data")
	runner := &mockCommandRunner{runFn: func() {
		if err := os.RemoveAll(dataDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dataDir, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	err := (&ReplicationStrategy{Runner: runner}).Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h:5432/db",
		TargetDir: dataDir,
	})
	if err == nil || !contains(err.Error(), "postgresql.auto.conf") || !contains(err.Error(), dataDir) || !contains(err.Error(), "remove it explicitly") {
		t.Fatal("expected error")
	}
	if info, statErr := os.Stat(dataDir); statErr != nil || info.IsDir() {
		t.Fatalf("expected retained partial target file, info=%v err=%v", info, statErr)
	}
}

func TestReplicationStrategyRejectsCallerOwnedDirectoryBeforeRun(t *testing.T) {
	target := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "caller-owned")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &mockCommandRunner{}
	err := (&ReplicationStrategy{Runner: runner}).Execute(context.Background(), Options{SourceDSN: "postgres://u:p@h:5432/db", TargetDir: target})
	if err == nil || !contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want existing target failure", err)
	}
	if len(runner.runCalls) != 0 {
		t.Fatalf("pg_basebackup ran for caller-owned target: %v", runner.runCalls)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("caller-owned content changed: %q, %v", data, err)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mockCopyConn records CopyTo/CopyFrom calls for verification.
type mockCopyConn struct {
	copyToCalls   []copyCall
	copyFromCalls []copyCall
	copyToErr     error
	copyFromErr   error
	closed        bool
}

type copyCall struct {
	sql string
}

func (m *mockCopyConn) CopyTo(ctx context.Context, w io.Writer, sql string) error {
	m.copyToCalls = append(m.copyToCalls, copyCall{sql: sql})
	if m.copyToErr != nil {
		return m.copyToErr
	}
	_, _ = w.Write([]byte("mock data\n"))
	return nil
}

func (m *mockCopyConn) CopyFrom(ctx context.Context, r io.Reader, sql string) error {
	m.copyFromCalls = append(m.copyFromCalls, copyCall{sql: sql})
	if m.copyFromErr != nil {
		return m.copyFromErr
	}
	_, _ = io.Copy(io.Discard, r)
	return nil
}

func (m *mockCopyConn) Close(context.Context) error {
	m.closed = true
	return nil
}

func TestCopyStreamStrategyExecute(t *testing.T) {
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		_ = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = origOpenDB }()

	origOpenCopy := openCopyConn
	srcConn := &mockCopyConn{}
	tgtConn := &mockCopyConn{}
	openCopyConn = func(ctx context.Context, dsn string) (copyConn, error) {
		if strings.Contains(dsn, "db_src") {
			return srcConn, nil
		}
		return tgtConn, nil
	}
	defer func() { openCopyConn = origOpenCopy }()

	origListSchemas := listSchemaNamesFunc
	listedSchemas := false
	listSchemaNamesFunc = func(ctx context.Context, q *sql.DB) ([]string, error) {
		listedSchemas = true
		return []string{"public", "app"}, nil
	}
	defer func() { listSchemaNamesFunc = origListSchemas }()

	origLoadSchemas := loadSchemasFunc
	var loadedSchemas []string
	loadSchemasFunc = func(ctx context.Context, q *sql.DB, schemas []string) ([]db.Table, error) {
		loadedSchemas = append([]string(nil), schemas...)
		return []db.Table{
			{Schema: "public", Name: "users"},
			{Schema: "public", Name: "orders", ForeignKeys: []db.ForeignKey{{ReferencedTableName: "users", ReferencedTableSchema: "public"}}},
			{Schema: "app", Name: "users"},
			{Schema: "app", Name: "events"},
		}, nil
	}
	defer func() { loadSchemasFunc = origLoadSchemas }()

	origRestoreSeq := restoreSequencesFunc
	restoreSequencesFunc = func(ctx context.Context, srcDB, tgtDB *sql.DB) error {
		return nil
	}
	defer func() { restoreSequencesFunc = origRestoreSeq }()

	mockRunner := &mockCommandRunner{}

	var progress []string
	strat := &CopyStreamStrategy{Runner: mockRunner}
	err = strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		ProgressFn: func(message string) { progress = append(progress, message) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !listedSchemas || !sliceEqual(loadedSchemas, []string{"public", "app"}) {
		t.Fatalf("unfiltered stream schemas = %v, listed=%v", loadedSchemas, listedSchemas)
	}

	// Verify schema replay pipe was called
	if len(mockRunner.pipeCalls) != 1 {
		t.Fatalf("expected 1 pipe call, got %d", len(mockRunner.pipeCalls))
	}
	pc := mockRunner.pipeCalls[0]
	if pc.srcName != "pg_dump" {
		t.Fatalf("expected pg_dump, got %s", pc.srcName)
	}
	wantSrcArgs := []string{"--schema-only", "--no-owner", "--no-acl", "postgres://u@h-a:5432/db_src"}
	if !sliceEqual(pc.srcArgs, wantSrcArgs) {
		t.Fatalf("pg_dump args = %v, want %v", pc.srcArgs, wantSrcArgs)
	}
	wantDstArgs := []string{"-v", "ON_ERROR_STOP=1", "postgres://u@h-a:5432/db_clone"}
	if !sliceEqual(pc.dstArgs, wantDstArgs) {
		t.Fatalf("psql args = %v, want %v", pc.dstArgs, wantDstArgs)
	}

	// Verify connections were opened and closed
	if !srcConn.closed {
		t.Fatal("expected source copy connection to be closed")
	}
	if !tgtConn.closed {
		t.Fatal("expected target copy connection to be closed")
	}

	// Verify tables were copied in FK-safe order with schema-qualified identifiers
	if len(srcConn.copyToCalls) != 4 {
		t.Fatalf("expected 4 CopyTo calls, got %d", len(srcConn.copyToCalls))
	}
	wantSQL := []string{
		`COPY "app"."events" TO STDOUT`,
		`COPY "app"."users" TO STDOUT`,
		`COPY "public"."users" TO STDOUT`,
		`COPY "public"."orders" TO STDOUT`,
	}
	for i, want := range wantSQL {
		if srcConn.copyToCalls[i].sql != want {
			t.Fatalf("copyToCalls[%d] = %q, want %q", i, srcConn.copyToCalls[i].sql, want)
		}
	}

	if len(tgtConn.copyFromCalls) != 4 {
		t.Fatalf("expected 4 CopyFrom calls, got %d", len(tgtConn.copyFromCalls))
	}
	for i, want := range wantSQL {
		fromSQL := strings.ReplaceAll(want, "TO STDOUT", "FROM STDIN")
		if tgtConn.copyFromCalls[i].sql != fromSQL {
			t.Fatalf("copyFromCalls[%d] = %q, want %q", i, tgtConn.copyFromCalls[i].sql, fromSQL)
		}
	}

	wantProgress := []string{
		"replaying schema",
		`copying table "app"."events"`,
		`copying table "app"."users"`,
		`copying table "public"."users"`,
		`copying table "public"."orders"`,
		"restoring sequences",
	}
	if !sliceEqual(progress, wantProgress) {
		t.Fatalf("progress = %v, want %v", progress, wantProgress)
	}
}

func TestCopyStreamStrategySchemaReplayFailure(t *testing.T) {
	mockRunner := &mockCommandRunner{pipeErr: errors.New("pipe broken")}
	strat := &CopyStreamStrategy{Runner: mockRunner}

	err := strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		DumpOpts:   []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// A dump schema filter bypasses source-wide schema enumeration.
	if !contains(err.Error(), "load schema") {
		t.Fatalf("error should mention load schema: %v", err)
	}
	if !mentionsHostResolutionFailure(err.Error()) {
		t.Fatalf("error should mention host resolution failure: %v", err)
	}
}

func TestCopyStreamStrategyForwardsSchemaOptions(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts Options
		want []string
	}{
		{"selected dump schemas", Options{DumpOpts: []dump.Option{dump.WithSchemas([]string{"app"})}}, []string{"app"}},
		{"restore fallback", Options{RestoreOpts: []restore.Option{restore.WithSchemas([]string{"audit"})}}, []string{"audit"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dbConn, _, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer dbConn.Close()
			origOpenDB, origList, origLoad := sqlOpenDB, listSchemaNamesFunc, loadSchemasFunc
			sqlOpenDB = func(string) (*sql.DB, error) { return dbConn, nil }
			listSchemaNamesFunc = func(context.Context, *sql.DB) ([]string, error) {
				t.Fatal("listed all schemas despite filter")
				return nil, nil
			}
			var got []string
			loadSchemasFunc = func(_ context.Context, _ *sql.DB, schemas []string) ([]db.Table, error) {
				got = append([]string(nil), schemas...)
				return nil, errors.New("stop")
			}
			defer func() { sqlOpenDB, listSchemaNamesFunc, loadSchemasFunc = origOpenDB, origList, origLoad }()

			tt.opts.SourceDSN, tt.opts.CloneName, tt.opts.SkipCreate = "postgres://u:p@h:5432/source", "clone", true
			err = (&CopyStreamStrategy{}).Execute(context.Background(), tt.opts)
			if err == nil || !sliceEqual(got, tt.want) {
				t.Fatalf("error=%v schemas=%v, want %v", err, got, tt.want)
			}
		})
	}
}

func mentionsHostResolutionFailure(s string) bool {
	lower := strings.ToLower(s)
	return contains(lower, "hostname resolving error") ||
		contains(lower, "no such host") ||
		contains(lower, "temporary failure in name resolution")
}

func TestCopyTable(t *testing.T) {
	src := &mockCopyConn{}
	tgt := &mockCopyConn{}

	err := copyTable(context.Background(), src, tgt, "public", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(src.copyToCalls) != 1 {
		t.Fatalf("expected 1 CopyTo call, got %d", len(src.copyToCalls))
	}
	if src.copyToCalls[0].sql != `COPY "public"."users" TO STDOUT` {
		t.Fatalf("unexpected CopyTo SQL: %q", src.copyToCalls[0].sql)
	}

	if len(tgt.copyFromCalls) != 1 {
		t.Fatalf("expected 1 CopyFrom call, got %d", len(tgt.copyFromCalls))
	}
	if tgt.copyFromCalls[0].sql != `COPY "public"."users" FROM STDIN` {
		t.Fatalf("unexpected CopyFrom SQL: %q", tgt.copyFromCalls[0].sql)
	}
}

func TestCopyTableSchemaQualified(t *testing.T) {
	src := &mockCopyConn{}
	tgt := &mockCopyConn{}

	err := copyTable(context.Background(), src, tgt, "app", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `COPY "app"."users" TO STDOUT`
	if src.copyToCalls[0].sql != want {
		t.Fatalf("got %q, want %q", src.copyToCalls[0].sql, want)
	}
}

func TestCopyTableQuotedName(t *testing.T) {
	src := &mockCopyConn{}
	tgt := &mockCopyConn{}

	err := copyTable(context.Background(), src, tgt, "public", `user"s`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `COPY "public"."user""s" TO STDOUT`
	if src.copyToCalls[0].sql != want {
		t.Fatalf("got %q, want %q", src.copyToCalls[0].sql, want)
	}
}

func TestCopyTableSourceFailure(t *testing.T) {
	src := &mockCopyConn{copyToErr: errors.New("copy to failed")}
	tgt := &mockCopyConn{}

	err := copyTable(context.Background(), src, tgt, "public", "users")
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "copy from source") {
		t.Fatalf("error should mention copy from source: %v", err)
	}
}

func TestCopyTableTargetFailure(t *testing.T) {
	src := &mockCopyConn{}
	tgt := &mockCopyConn{copyFromErr: errors.New("copy from failed")}

	err := copyTable(context.Background(), src, tgt, "public", "users")
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "copy to target") {
		t.Fatalf("error should mention copy to target: %v", err)
	}
}

func TestCopyTableCloseWithErrorOnSourceFailure(t *testing.T) {
	// Verify that when source CopyTo fails, the pipe writer is closed with an error
	// so target CopyFrom does not silently accept partial rows.
	wantErr := errors.New("source stream aborted")
	src := &mockCopyConn{copyToErr: wantErr}
	tgt := &mockCopyConn{}

	err := copyTable(context.Background(), src, tgt, "public", "users")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error to wrap source error: got %v", err)
	}
	if len(tgt.copyFromCalls) != 1 {
		t.Fatalf("expected target CopyFrom to be invoked so it can observe the error, got %d calls", len(tgt.copyFromCalls))
	}
}

// blockingSourceConn writes continuously until the pipe is closed.
type blockingSourceConn struct{}

func (m *blockingSourceConn) CopyTo(ctx context.Context, w io.Writer, sql string) error {
	buf := make([]byte, 1024)
	for {
		_, err := w.Write(buf)
		if err != nil {
			return err
		}
	}
}

func (m *blockingSourceConn) CopyFrom(ctx context.Context, r io.Reader, sql string) error {
	return errors.New("source should not be target")
}

func (m *blockingSourceConn) Close(context.Context) error { return nil }

// immediateFailTargetConn returns an error from CopyFrom immediately.
type immediateFailTargetConn struct {
	err error
}

func (m *immediateFailTargetConn) CopyTo(ctx context.Context, w io.Writer, sql string) error {
	return errors.New("target should not be source")
}

func (m *immediateFailTargetConn) CopyFrom(ctx context.Context, r io.Reader, sql string) error {
	return m.err
}

func (m *immediateFailTargetConn) Close(context.Context) error { return nil }

func TestCopyTableTargetErrorPriority(t *testing.T) {
	// When target CopyFrom fails, the pipe reader is closed.
	// The source CopyTo may then fail due to the closed pipe.
	// The target error must take priority over the secondary source error.
	wantTgtErr := errors.New("target constraint violation")

	src := &blockingSourceConn{}
	tgt := &immediateFailTargetConn{err: wantTgtErr}

	err := copyTable(context.Background(), src, tgt, "public", "users")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantTgtErr) {
		t.Fatalf("expected target error to take priority, got: %v", err)
	}
	if !contains(err.Error(), "copy") {
		t.Fatalf("error should mention copy: %v", err)
	}
}

func TestCopyStreamCleanupUsesBoundedIndependentContext(t *testing.T) {
	origDrop := dropDatabaseFunc
	defer func() { dropDatabaseFunc = origDrop }()
	primary := errors.New("source unavailable")
	var live, bounded bool
	dropDatabaseFunc = func(ctx context.Context, _, _ string) error {
		live = ctx.Err() == nil
		_, bounded = ctx.Deadline()
		return errors.New("cleanup failed")
	}
	err := (&CopyStreamStrategy{}).wrapWithCleanup("open source", "postgres://admin@host/postgres", "clone", primary)
	if !errors.Is(err, primary) {
		t.Fatalf("error = %v, want primary error", err)
	}
	if !live || !bounded {
		t.Fatalf("cleanup context live=%v bounded=%v", live, bounded)
	}
}

func TestRestoreSequences(t *testing.T) {
	srcDB, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer srcDB.Close()

	tgtDB, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer tgtDB.Close()

	rows := sqlmock.NewRows([]string{"schemaname", "sequencename", "last_value", "start_value"}).
		AddRow("public", "users_id_seq", 42, 1).
		AddRow("app", "events_seq", 100, 1)

	srcMock.ExpectQuery(`SELECT schemaname, sequencename, last_value, start_value`).
		WillReturnRows(rows)

	tgtMock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 42, true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	tgtMock.ExpectExec(`SELECT setval\('"app"\."events_seq"'::regclass, 100, true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = restoreSequences(context.Background(), srcDB, tgtDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("source unmet expectations: %v", err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("target unmet expectations: %v", err)
	}
}

func TestRestoreSequencesListError(t *testing.T) {
	srcDB, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer srcDB.Close()

	tgtDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer tgtDB.Close()

	srcMock.ExpectQuery(`SELECT schemaname, sequencename, last_value, start_value`).
		WillReturnError(errors.New("connection lost"))

	err = restoreSequences(context.Background(), srcDB, tgtDB)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "list sequences") {
		t.Fatalf("error should mention list sequences: %v", err)
	}
}

func TestRestoreSequencesSetvalError(t *testing.T) {
	srcDB, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer srcDB.Close()

	tgtDB, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer tgtDB.Close()

	rows := sqlmock.NewRows([]string{"schemaname", "sequencename", "last_value", "start_value"}).
		AddRow("public", "users_id_seq", 42, 1)

	srcMock.ExpectQuery(`SELECT schemaname, sequencename, last_value, start_value`).
		WillReturnRows(rows)

	tgtMock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 42, true\)`).
		WillReturnError(errors.New("permission denied"))

	err = restoreSequences(context.Background(), srcDB, tgtDB)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "setval") {
		t.Fatalf("error should mention setval: %v", err)
	}
}

func TestRestoreSequencesQuotedNames(t *testing.T) {
	srcDB, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer srcDB.Close()

	tgtDB, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer tgtDB.Close()

	// Names containing double quotes and a single quote to verify escaping
	rows := sqlmock.NewRows([]string{"schemaname", "sequencename", "last_value", "start_value"}).
		AddRow(`my"schema`, `user's_seq`, 7, 1)

	srcMock.ExpectQuery(`SELECT schemaname, sequencename, last_value, start_value`).
		WillReturnRows(rows)

	// The SQL must use a safely quoted string literal with ::regclass, not bare identifiers.
	tgtMock.ExpectExec(`SELECT setval\('"my""schema"\."user''s_seq"'::regclass, 7, true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = restoreSequences(context.Background(), srcDB, tgtDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("source unmet expectations: %v", err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("target unmet expectations: %v", err)
	}
}

func TestRestoreSequencesNeverCalled(t *testing.T) {
	srcDB, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer srcDB.Close()

	tgtDB, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer tgtDB.Close()

	rows := sqlmock.NewRows([]string{"schemaname", "sequencename", "last_value", "start_value"}).
		AddRow("public", "users_id_seq", nil, 1)

	srcMock.ExpectQuery(`SELECT schemaname, sequencename, last_value, start_value`).
		WillReturnRows(rows)

	tgtMock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 1, false\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = restoreSequences(context.Background(), srcDB, tgtDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("source unmet expectations: %v", err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("target unmet expectations: %v", err)
	}
}

func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`hello`, `'hello'`},
		{`it''s`, `'it''''s'`},
		{`no'quotes`, `'no''quotes'`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := quoteLiteral(tt.input)
			if got != tt.want {
				t.Fatalf("quoteLiteral(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteQualifiedTable(t *testing.T) {
	got := quoteQualifiedTable("app", `user"s`)
	want := `"app"."user""s"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSchemaReplayStrategyProgressEventOrdering(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		return nil, fmt.Errorf("mock db")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	origDump := dumpFunc
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		return nil
	}
	defer func() { dumpFunc = origDump }()

	origRestore := restoreFunc
	restoreFunc = func(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...restore.Option) error {
		return nil
	}
	defer func() { restoreFunc = origRestore }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	var events []ProgressEvent
	err := strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		ProgressEvent: func(ev ProgressEvent) {
			events = append(events, ev)
		},
	})
	if err == nil {
		t.Fatal("expected error from mock db open")
	}

	// sqlOpenDB fails at "open source" step, so only 1 event is emitted (replaying_schema)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (sqlOpenDB fails before dump/restore)", len(events))
	}

	if events[0].Phase != "replaying_schema" {
		t.Fatalf("event[0] Phase = %q, want replaying_schema", events[0].Phase)
	}
	if events[0].Current != 1 {
		t.Fatalf("event[0] Current = %d, want 1", events[0].Current)
	}
	if events[0].Total != 5 {
		t.Fatalf("event[0] Total = %d, want 5", events[0].Total)
	}
	if events[0].Elapsed <= 0 {
		t.Fatalf("event[0] Elapsed = %v, want > 0", events[0].Elapsed)
	}
}

func TestSchemaReplayStrategyProgressEventWithCreate(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		return nil, fmt.Errorf("mock db")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	origDump := dumpFunc
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		return nil
	}
	defer func() { dumpFunc = origDump }()

	origRestore := restoreFunc
	restoreFunc = func(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...restore.Option) error {
		return nil
	}
	defer func() { restoreFunc = origRestore }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	var events []ProgressEvent
	err := strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: false,
		ProgressEvent: func(ev ProgressEvent) {
			events = append(events, ev)
		},
	})
	// CreateDatabase → sqlOpenDB → error
	if err == nil {
		t.Fatal("expected error from mock db open")
	}

	// Only 1 event: creating_target (sqlOpenDB fails in CreateDatabase)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (sqlOpenDB fails in CreateDatabase)", len(events))
	}

	if events[0].Phase != "creating_target" {
		t.Fatalf("event[0] Phase = %q, want creating_target", events[0].Phase)
	}
	if events[0].Current != 1 {
		t.Fatalf("event[0] Current = %d, want 1", events[0].Current)
	}
}

func TestSchemaReplayStrategyDeprecatedProgressFnStillWorks(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		return nil, fmt.Errorf("mock db")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	origDump := dumpFunc
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		return nil
	}
	defer func() { dumpFunc = origDump }()

	origRestore := restoreFunc
	restoreFunc = func(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...restore.Option) error {
		return nil
	}
	defer func() { restoreFunc = origRestore }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	var progress []string
	err := strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		ProgressFn: func(msg string) { progress = append(progress, msg) },
	})
	if err == nil {
		t.Fatal("expected error from mock db open")
	}

	// reportProgressEvent calls opts.ProgressFn when no ProgressEvent is set.
	// sqlOpenDB fails after replaying_schema event, so only 1 string message.
	if len(progress) != 1 {
		t.Fatalf("progress = %d, want 1", len(progress))
	}
	if progress[0] != "replaying schema" {
		t.Fatalf("progress[0] = %q, want 'replaying schema'", progress[0])
	}
}

func TestSchemaReplayStrategyDropOnPostCreateFailure(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	// Record dropDatabase calls
	var dropCalls []string
	origDrop := dropDatabaseFunc
	dropDatabaseFunc = func(ctx context.Context, adminDSN, dbName string) error {
		dropCalls = append(dropCalls, dbName)
		return nil
	}
	defer func() { dropDatabaseFunc = origDrop }()

	// Override sqlOpenDB: succeed for admin DSN (CreateDatabase), fail for source.
	origOpenDB := sqlOpenDB
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()
	mock.ExpectExec(`CREATE DATABASE "db_clone"`).WillReturnResult(sqlmock.NewResult(1, 1))

	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		if strings.Contains(dsn, "/postgres") {
			return mockDB, nil
		}
		return nil, fmt.Errorf("source connection failed")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	err = strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: false,
	})
	if err == nil {
		t.Fatal("expected error from source connection failure")
	}
	if !contains(err.Error(), "source connection failed") {
		t.Fatalf("error should wrap source connection failure: %v", err)
	}
	if len(dropCalls) != 1 {
		t.Fatalf("expected 1 dropDatabase call, got %d", len(dropCalls))
	}
	if dropCalls[0] != "db_clone" {
		t.Fatalf("dropDatabase dbName = %q, want db_clone", dropCalls[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestSchemaReplayStrategyCleanupSurvivesCancellation(t *testing.T) {
	origDrop := dropDatabaseFunc
	defer func() { dropDatabaseFunc = origDrop }()
	primary := errors.New("replay canceled")
	var cleanupLive, cleanupBounded bool
	dropDatabaseFunc = func(ctx context.Context, _, _ string) error {
		cleanupLive = ctx.Err() == nil
		_, cleanupBounded = ctx.Deadline()
		return errors.New("cleanup failed")
	}

	origOpenDB := sqlOpenDB
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()
	mock.ExpectExec(`CREATE DATABASE "db_clone"`).WillReturnResult(sqlmock.NewResult(1, 1))
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		if strings.Contains(dsn, "/postgres") {
			return mockDB, nil
		}
		return nil, errors.New("source unavailable")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	ctx, cancel := context.WithCancel(context.Background())
	runner := &mockCommandRunner{pipeErr: primary, pipeFn: cancel}
	err = (&SchemaReplayStrategy{Runner: runner}).Execute(ctx, Options{SourceDSN: "postgres://u:p@h:5432/source", CloneName: "db_clone"})
	if !errors.Is(err, primary) {
		t.Fatalf("error = %v, want primary error", err)
	}
	if !cleanupLive || !cleanupBounded {
		t.Fatalf("cleanup context live=%v bounded=%v", cleanupLive, cleanupBounded)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaReplayStrategyCleanupDeadlinePreservesPrimaryError(t *testing.T) {
	origDrop := dropDatabaseFunc
	origTimeout := schemaReplayCleanupTimeout
	defer func() {
		dropDatabaseFunc = origDrop
		schemaReplayCleanupTimeout = origTimeout
	}()

	primary := errors.New("replay canceled")
	cleanupStarted := make(chan struct{}, 1)
	var cleanupErr error
	schemaReplayCleanupTimeout = time.Millisecond
	dropDatabaseFunc = func(ctx context.Context, _, _ string) error {
		cleanupStarted <- struct{}{}
		<-ctx.Done()
		cleanupErr = ctx.Err()
		return cleanupErr
	}

	origOpenDB := sqlOpenDB
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mockDB.Close()
	mock.ExpectExec(`CREATE DATABASE "db_clone"`).WillReturnResult(sqlmock.NewResult(1, 1))
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		if strings.Contains(dsn, "/postgres") {
			return mockDB, nil
		}
		return nil, errors.New("source unavailable")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	origStderr := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = origStderr }()

	err = (&SchemaReplayStrategy{Runner: &mockCommandRunner{pipeErr: primary}}).Execute(context.Background(), Options{
		SourceDSN: "postgres://u:p@h:5432/source",
		CloneName: "db_clone",
	})
	if !errors.Is(err, primary) {
		t.Fatalf("error = %v, want primary error", err)
	}
	if !errors.Is(cleanupErr, context.DeadlineExceeded) {
		t.Fatalf("cleanup error = %v, want DeadlineExceeded", cleanupErr)
	}
	select {
	case <-cleanupStarted:
	default:
		t.Fatal("cleanup did not start")
	}
	if _, err := stderr.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	warning, err := io.ReadAll(stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(warning), "warning: cleanup drop database") {
		t.Fatalf("cleanup warning missing: %q", warning)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaReplayStrategyNoDropWhenSkipCreate(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	// Record dropDatabase calls
	var dropCalls []string
	origDrop := dropDatabaseFunc
	dropDatabaseFunc = func(ctx context.Context, adminDSN, dbName string) error {
		dropCalls = append(dropCalls, dbName)
		return nil
	}
	defer func() { dropDatabaseFunc = origDrop }()

	// Override sqlOpenDB to fail for source DSN.
	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		return nil, fmt.Errorf("source connection failed")
	}
	defer func() { sqlOpenDB = origOpenDB }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	err := strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		DumpOpts:   []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err == nil {
		t.Fatal("expected error from source connection failure")
	}
	if !contains(err.Error(), "source connection failed") {
		t.Fatalf("error should wrap source connection failure: %v", err)
	}
	if len(dropCalls) != 0 {
		t.Fatalf("expected 0 dropDatabase calls with SkipCreate=true, got %d", len(dropCalls))
	}
}

func TestSchemaReplayStrategyInvokesRestoreSequences(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		db, _, err := sqlmock.New()
		if err != nil {
			return nil, err
		}
		return db, nil
	}
	defer func() { sqlOpenDB = origOpenDB }()

	origDump := dumpFunc
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		return nil
	}
	defer func() { dumpFunc = origDump }()

	origRestore := restoreFunc
	restoreFunc = func(ctx context.Context, dbConn *sql.DB, inputDir string, opts ...restore.Option) error {
		return nil
	}
	defer func() { restoreFunc = origRestore }()

	var restoreSeqCalled bool
	origRestoreSeq := restoreSequencesFunc
	restoreSequencesFunc = func(ctx context.Context, srcDB, tgtDB *sql.DB) error {
		restoreSeqCalled = true
		return nil
	}
	defer func() { restoreSequencesFunc = origRestoreSeq }()

	mockRunner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: mockRunner}

	err := strat.Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		DumpOpts:   []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !restoreSeqCalled {
		t.Fatal("restoreSequencesFunc was not called in schema-replay strategy")
	}
}
