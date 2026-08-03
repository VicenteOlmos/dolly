package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestRewriteDSN(t *testing.T) {
	tests := []struct {
		name     string
		original string
		newDB    string
		want     string
		wantErr  bool
	}{
		{
			name:     "basic rewrite",
			original: "postgres://u:p@h-a:5432/db_a?sslmode=disable",
			newDB:    "db_a_kloned_1",
			want:     "postgres://u:p@h-a:5432/db_a_kloned_1?sslmode=disable",
		},
		{
			name:     "SSL params preserved",
			original: "postgres://u:p@h-a:5432/db_a?sslmode=require&sslrootcert=/tmp/x.crt",
			newDB:    "db_b",
			want:     "postgres://u:p@h-a:5432/db_b?sslmode=require&sslrootcert=/tmp/x.crt",
		},
		{
			name:     "special chars in password",
			original: "postgres://u:p%40ss%23word@h-a:5432/db_a",
			newDB:    "db_b",
			want:     "postgres://u:p%40ss%23word@h-a:5432/db_b",
		},
		{
			name:     "raw special chars in password",
			original: "postgres://u:p%x^=y@h-b:5432/db_src",
			newDB:    "db_clone",
			want:     "postgres://u:p%25x%5E=y@h-b:5432/db_clone",
		},
		{
			name:     "IPv6 host",
			original: "postgres://u:p@[::1]:5432/db_a?sslmode=disable",
			newDB:    "db_b",
			want:     "postgres://u:p@[::1]:5432/db_b?sslmode=disable",
		},
		{
			name:     "empty path",
			original: "postgres://u:p@h-a:5432",
			newDB:    "db_b",
			want:     "postgres://u:p@h-a:5432/db_b",
		},
		{
			name:     "malformed URL",
			original: "://bad-url",
			newDB:    "db",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RewriteDSN(tt.original, tt.newDB)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDBName(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		want    string
		wantErr bool
	}{
		{
			name: "standard DSN",
			dsn:  "postgres://u:p@h-a:5432/db_a?sslmode=disable",
			want: "db_a",
		},
		{
			name: "no query params",
			dsn:  "postgres://h-a:5432/db_a",
			want: "db_a",
		},
		{
			name: "raw special chars in password",
			dsn:  "postgres://u:p%x^=y@h-b:5432/db_src",
			want: "db_src",
		},
		{
			name:    "empty path",
			dsn:     "postgres://u:p@h-a:5432",
			wantErr: true,
		},
		{
			name:    "malformed URL",
			dsn:     "://bad",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDBName(tt.dsn)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCloneName(t *testing.T) {
	tests := []struct {
		name     string
		sourceDB string
		template string
		want     string
	}{
		{
			name:     "default template",
			sourceDB: "db_a",
			template: "",
			want:     "db_a_dolly_{n}",
		},
		{
			name:     "custom template",
			sourceDB: "db_x",
			template: "{db}_backup",
			want:     "db_x_backup",
		},
		{
			name:     "empty source name",
			sourceDB: "",
			template: "{db}_dolly_{n}",
			want:     "_dolly_{n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CloneName(tt.sourceDB, tt.template)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSameInstance(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		target  string
		want    bool
		wantErr bool
	}{
		{
			name:   "same host and port",
			source: "postgres://u:p@h-a:5432/db_src",
			target: "postgres://u:p@h-a:5432/db_tgt",
			want:   true,
		},
		{
			name:   "same host default vs explicit port",
			source: "postgres://u:p@h-a:5432/db_src",
			target: "postgres://u:p@h-a/db_tgt",
			want:   true,
		},
		{
			name:   "same host both default port",
			source: "postgres://u:p@h-a/db_src",
			target: "postgres://u:p@h-a/db_tgt",
			want:   true,
		},
		{
			name:   "different hosts",
			source: "postgres://u:p@h-a:5432/db_src",
			target: "postgres://u:p@h-b:5432/db_tgt",
			want:   false,
		},
		{
			name:   "different ports",
			source: "postgres://u:p@h-a:5432/db_src",
			target: "postgres://u:p@h-a:5433/db_tgt",
			want:   false,
		},
		{
			name:   "IPv6 same",
			source: "postgres://u:p@[::1]:5432/db_src",
			target: "postgres://u:p@[::1]:5432/db_tgt",
			want:   true,
		},
		{
			name:   "IPv6 different",
			source: "postgres://u:p@[::1]:5432/db_src",
			target: "postgres://u:p@[::2]:5432/db_tgt",
			want:   false,
		},
		{
			name:   "unix socket same path",
			source: "postgres:///db_src?host=/var/run/postgresql",
			target: "postgres:///db_tgt?host=/var/run/postgresql",
			want:   true,
		},
		{
			name:   "unix socket different path",
			source: "postgres:///db_src?host=/var/run/postgresql",
			target: "postgres:///db_tgt?host=/tmp/postgresql",
			want:   false,
		},
		{
			name:   "query param host overrides URL host",
			source: "postgres://host-a:5432/db_src?host=host-b",
			target: "postgres://host-b:5432/db_tgt",
			want:   true,
		},
		{
			name:   "query param port overrides missing URL port",
			source: "postgres://h-a/db_src?port=5433",
			target: "postgres://h-a:5433/db_tgt",
			want:   true,
		},
		{
			name:   "query param host and port override",
			source: "postgres://host-a:5432/db_src?host=host-b&port=5433",
			target: "postgres://host-b:5433/db_tgt",
			want:   true,
		},
		{
			name:   "unix socket with port query param same",
			source: "postgres:///db_src?host=/var/run/postgresql&port=5433",
			target: "postgres:///db_tgt?host=/var/run/postgresql&port=5433",
			want:   true,
		},
		{
			name:   "unix socket with port query param different",
			source: "postgres:///db_src?host=/var/run/postgresql&port=5433",
			target: "postgres:///db_tgt?host=/var/run/postgresql&port=5434",
			want:   false,
		},
		{
			name:    "invalid source DSN",
			source:  "://bad",
			target:  "postgres://h-a/db_a",
			wantErr: true,
		},
		{
			name:    "invalid target DSN",
			source:  "postgres://h-a/db_a",
			target:  "://bad",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SameInstance(tt.source, tt.target)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateDatabaseSuccess(t *testing.T) {
	mockDB, mock := newSQLMock(t)
	defer mockDB.Close()

	orig := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		_ = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = orig }()

	mock.ExpectExec(`CREATE DATABASE "db_clone"`).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := CreateDatabase(context.Background(), "postgres://u@h-a:5432/postgres", "db_clone"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateDatabasePrivilegeError(t *testing.T) {
	mockDB, mock := newSQLMock(t)
	defer mockDB.Close()

	orig := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		_ = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = orig }()

	mock.ExpectExec(`CREATE DATABASE "db_clone"`).WillReturnError(errors.New("permission denied to create database"))

	err := CreateDatabase(context.Background(), "postgres://u@h-a:5432/postgres", "db_clone")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, errors.New("permission denied to create database")) {
		// errors.Is won't match a fresh error; check string containment
		if !contains(err.Error(), "permission denied") {
			t.Fatalf("error should mention privilege issue: %v", err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateDatabaseQuotedName(t *testing.T) {
	mockDB, mock := newSQLMock(t)
	defer mockDB.Close()

	orig := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		_ = dsn
		return mockDB, nil
	}
	defer func() { sqlOpenDB = orig }()

	mock.ExpectExec(`CREATE DATABASE "db""clone"`).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := CreateDatabase(context.Background(), "postgres://u@h-a:5432/postgres", `db"clone`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateDatabaseConnectionError(t *testing.T) {
	orig := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		_ = dsn
		return nil, fmt.Errorf("connection refused")
	}
	defer func() { sqlOpenDB = orig }()

	err := CreateDatabase(context.Background(), "postgres://h-z/postgres", "db_clone")
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "open admin connection") {
		t.Fatalf("error should mention open admin connection: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRunPreflightFailureSkipsExecute(t *testing.T) {
	baseDir := t.TempDir()

	origPF := preflightFunc
	preflightFunc = func(ctx context.Context, opts Options, strat Strategy) error {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strat.Name(),
			Role:     "app_user",
			Database: "postgres (CREATEDB required)",
			Hint:     "grant CREATEDB to this role or use skip_create with an existing target database",
		}
	}
	defer func() { preflightFunc = origPF }()

	mockRunner := &mockCommandRunner{}
	err := Run(context.Background(), Options{
		SourceDSN:     "postgres://u:p@h-a:5432/db_src",
		CloneName:     "db_clone",
		DumpDir:       baseDir,
		Strategy:      "schema-replay",
		CommandRunner: mockRunner,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "CREATEDB") {
		t.Fatalf("error should mention CREATEDB: %v", err)
	}
	if len(mockRunner.pipeCalls) != 0 {
		t.Fatalf("expected no pipe calls when preflight fails, got %d", len(mockRunner.pipeCalls))
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatalf("read dump dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no temp dump dir under DumpDir, got %d entries", len(entries))
	}
}

func TestRunSkipsCreate(t *testing.T) {
	mockDB, mock := newSQLMock(t)
	defer mockDB.Close()

	origOpen := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) { return mockDB, nil }
	defer func() { sqlOpenDB = origOpen }()

	expectPreflightSchemaReplay(mock, struct {
		sourceDB, cloneName   string
		skipCreate, crossInst bool
		sourceVer, targetVer  int
	}{sourceDB: "db_src", cloneName: "db_clone", skipCreate: true})

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

	origRestoreSeq := restoreSequencesFunc
	restoreSequencesFunc = func(ctx context.Context, srcDB, tgtDB *sql.DB) error { return nil }
	defer func() { restoreSequencesFunc = origRestoreSeq }()

	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origPgDump := pgDumpVersion
	pgDumpVersion = func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil }
	defer func() { pgDumpVersion = origPgDump }()

	mockRunner := &mockCommandRunner{}

	err := Run(context.Background(), Options{
		SourceDSN:     "postgres://u:p@h-a:5432/db_src",
		CloneName:     "db_clone",
		SkipCreate:    true,
		Strategy:      "schema-replay",
		CommandRunner: mockRunner,
		DumpOpts:      []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockRunner.pipeCalls) != 1 {
		t.Fatalf("expected 1 pipe call, got %d", len(mockRunner.pipeCalls))
	}
}

func TestRunRewritesCustomTargetURLToCloneName(t *testing.T) {
	mockDB, mock := newSQLMock(t)
	defer mockDB.Close()

	origOpen := sqlOpenDB
	var openedDSNs []string
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		openedDSNs = append(openedDSNs, dsn)
		return mockDB, nil
	}
	defer func() { sqlOpenDB = origOpen }()

	expectPreflightSchemaReplay(mock, struct {
		sourceDB, cloneName   string
		skipCreate, crossInst bool
		sourceVer, targetVer  int
	}{sourceDB: "db_src", cloneName: "db_clone", skipCreate: true, crossInst: true})

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

	origRestoreSeq := restoreSequencesFunc
	restoreSequencesFunc = func(ctx context.Context, srcDB, tgtDB *sql.DB) error { return nil }
	defer func() { restoreSequencesFunc = origRestoreSeq }()

	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origPgDump := pgDumpVersion
	pgDumpVersion = func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil }
	defer func() { pgDumpVersion = origPgDump }()

	mockRunner := &mockCommandRunner{}

	err := Run(context.Background(), Options{
		SourceDSN:     "postgres://u:p@h-a:5432/db_src",
		CloneName:     "db_clone",
		TargetDSN:     "postgres://u:p@h-b:5432/db_b?sslmode=require",
		SkipCreate:    true,
		Strategy:      "schema-replay",
		CommandRunner: mockRunner,
		DumpOpts:      []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockRunner.pipeCalls) != 1 {
		t.Fatalf("expected 1 pipe call, got %d", len(mockRunner.pipeCalls))
	}
	pc := mockRunner.pipeCalls[0]
	wantDstArgs := []string{"-v", "ON_ERROR_STOP=1", "postgres://u@h-b:5432/db_clone?sslmode=require"}
	if !sliceEqual(pc.dstArgs, wantDstArgs) {
		t.Fatalf("psql args = %v, want %v", pc.dstArgs, wantDstArgs)
	}

	var targetDSN string
	for _, dsn := range openedDSNs {
		if strings.Contains(dsn, "h-b") && strings.Contains(dsn, "/db_clone") {
			targetDSN = dsn
			break
		}
	}
	if targetDSN == "" {
		t.Fatal("expected target DSN to be opened")
	}
	if !strings.Contains(targetDSN, "/db_clone") {
		t.Fatalf("target DSN should use clone name 'db_clone', got %q", targetDSN)
	}
}

func TestRunRejectsInvalidCloneName(t *testing.T) {
	err := Run(context.Background(), Options{
		SourceDSN: "postgres://u:p@h-a:5432/db_src",
		CloneName: "prod;drop",
	})
	if err == nil {
		t.Fatal("expected validation error for invalid clone name")
	}
	if !strings.Contains(err.Error(), "validate clone name") {
		t.Fatalf("error = %q, want 'validate clone name'", err.Error())
	}

	// Verify no dumpFunc or command runner was called.
	mockRunner := &mockCommandRunner{}
	origDump := dumpFunc
	dumpCalled := false
	dumpFunc = func(ctx context.Context, dbConn *sql.DB, outputDir string, opts ...dump.Option) error {
		dumpCalled = true
		return nil
	}
	defer func() { dumpFunc = origDump }()

	err = Run(context.Background(), Options{
		SourceDSN:     "postgres://u:p@h-a:5432/db_src",
		CloneName:     "prod;drop",
		CommandRunner: mockRunner,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if dumpCalled {
		t.Fatal("dumpFunc should not be called when clone name is invalid")
	}
}

func TestRunCreatesTempChildUnderDumpDir(t *testing.T) {
	baseDir := t.TempDir()

	mockDB, mock := newSQLMock(t)
	defer mockDB.Close()

	origOpen := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) { return mockDB, nil }
	defer func() { sqlOpenDB = origOpen }()

	expectPreflightSchemaReplay(mock, struct {
		sourceDB, cloneName   string
		skipCreate, crossInst bool
		sourceVer, targetVer  int
	}{sourceDB: "db_src", cloneName: "db_clone", skipCreate: true})

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

	origRestoreSeq := restoreSequencesFunc
	restoreSequencesFunc = func(ctx context.Context, srcDB, tgtDB *sql.DB) error { return nil }
	defer func() { restoreSequencesFunc = origRestoreSeq }()

	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origPgDump := pgDumpVersion
	pgDumpVersion = func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil }
	defer func() { pgDumpVersion = origPgDump }()

	mockRunner := &mockCommandRunner{}

	err := Run(context.Background(), Options{
		SourceDSN:     "postgres://u:p@h-a:5432/db_src",
		CloneName:     "db_clone",
		DumpDir:       baseDir,
		SkipCreate:    true,
		Strategy:      "schema-replay",
		CommandRunner: mockRunner,
		DumpOpts:      []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// baseDir should still exist and be empty (the temp child should have been removed)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatalf("read base dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected base dir to be empty after Run, got %d entries", len(entries))
	}
}

func TestRunMaxOpenConnsSequentialRestore(t *testing.T) {
	const baseline = 42
	MaxOpenConns = baseline
	t.Cleanup(func() { MaxOpenConns = 5 })

	origPF := preflightFunc
	seen := 0
	preflightFunc = func(ctx context.Context, opts Options, strat Strategy) error {
		seen = MaxOpenConns
		return errors.New("stop after preflight")
	}
	defer func() { preflightFunc = origPF }()

	err := Run(context.Background(), Options{
		SourceDSN:    "postgres://u:p@h-a:5432/db_src",
		CloneName:    "db_clone",
		Strategy:     "schema-replay",
		MaxOpenConns: 3,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if seen != 3 {
		t.Fatalf("during run MaxOpenConns = %d, want 3", seen)
	}
	if MaxOpenConns != baseline {
		t.Fatalf("after run MaxOpenConns = %d, want baseline %d", MaxOpenConns, baseline)
	}
}

func TestRunMaxOpenConnsConcurrentIsolation(t *testing.T) {
	const baseline = 99
	MaxOpenConns = baseline
	t.Cleanup(func() { MaxOpenConns = 5 })

	origPF := preflightFunc
	defer func() { preflightFunc = origPF }()

	start := make(chan struct{})
	observed := make(chan int, 16)
	preflightFunc = func(ctx context.Context, opts Options, strat Strategy) error {
		observed <- MaxOpenConns
		<-start
		return errors.New("stop after preflight")
	}

	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		want := i + 2
		go func(mc int) {
			defer wg.Done()
			_ = Run(context.Background(), Options{
				SourceDSN:    "postgres://u:p@h-a:5432/db_src",
				CloneName:    fmt.Sprintf("db_clone_%d", mc),
				Strategy:     "schema-replay",
				MaxOpenConns: mc,
			})
		}(want)
	}

	time.Sleep(50 * time.Millisecond)
	close(start)
	wg.Wait()

	close(observed)
	got := make(map[int]int)
	for v := range observed {
		got[v]++
	}
	for i := 0; i < workers; i++ {
		want := i + 2
		if got[want] != 1 {
			t.Fatalf("MaxOpenConns %d observed %d times, want 1", want, got[want])
		}
	}
	if MaxOpenConns != baseline {
		t.Fatalf("after concurrent runs MaxOpenConns = %d, want baseline %d", MaxOpenConns, baseline)
	}
}

func TestEffectiveRunMaxOpenConns(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want int
	}{
		{name: "zero uses default", opts: Options{}, want: 5},
		{name: "negative uses default", opts: Options{MaxOpenConns: -1}, want: 5},
		{name: "custom", opts: Options{MaxOpenConns: 9}, want: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveRunMaxOpenConns(tt.opts); got != tt.want {
				t.Fatalf("effectiveRunMaxOpenConns() = %d, want %d", got, tt.want)
			}
		})
	}
}
