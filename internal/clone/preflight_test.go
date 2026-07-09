package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestParseServerMajor(t *testing.T) {
	tests := []struct {
		name    string
		num     int
		want    int
		wantErr bool
	}{
		{name: "PG 15", num: 150002, want: 15},
		{name: "PG 16", num: 160000, want: 16},
		{name: "invalid", num: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseServerMajor(tt.num)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func TestParsePgDumpMajor(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    int
		wantErr bool
	}{
		{name: "standard", out: "pg_dump (PostgreSQL) 16.2", want: 16},
		{name: "short", out: "pg_dump (PostgreSQL) 15.0", want: 15},
		{name: "postgres keyword", out: "pg_dump (PostgreSQL) 14.1", want: 14},
		{name: "garbage", out: "unknown", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePgDumpMajor(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func TestPreflightErrorMessage(t *testing.T) {
	err := &PreflightError{
		Kind:     PreflightPermission,
		Strategy: "schema-replay",
		Role:     "app_user",
		Database: "postgres",
		Hint:     "grant CREATEDB",
	}
	msg := err.Error()
	for _, sub := range []string{"preflight permission", "schema-replay", "app_user", "grant CREATEDB"} {
		if !strings.Contains(msg, sub) {
			t.Fatalf("missing %q in %q", sub, msg)
		}
	}
}

func expectSourceReadPrivilegeQueries(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM pg_constraint con`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM pg_sequences`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM pg_namespace n`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM pg_type t`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM pg_proc p`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM pg_extension`).
		WillReturnRows(sqlmock.NewRows([]string{"extname"}))
}

func expectTargetRestoreQueries(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`FROM pg_extension`).
		WillReturnRows(sqlmock.NewRows([]string{"extname"}))
	mock.ExpectQuery(`SELECT DISTINCT n.nspname`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname"}))
}

func expectPreflightSchemaReplay(mock sqlmock.Sqlmock, opts struct {
	sourceDB   string
	cloneName  string
	skipCreate bool
	crossInst  bool
	sourceVer  int
	targetVer  int
}) {
	if opts.sourceVer == 0 {
		opts.sourceVer = 150002
	}
	if opts.targetVer == 0 {
		opts.targetVer = opts.sourceVer
	}

	mock.ExpectPing()
	if opts.crossInst {
		mock.ExpectPing()
	}

	mock.ExpectQuery(`SELECT current_user`).
		WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
	mock.ExpectQuery(`SELECT has_database_privilege`).
		WithArgs(opts.sourceDB).
		WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
	expectSourceReadPrivilegeQueries(mock)

	if opts.skipCreate {
		mock.ExpectQuery(`SELECT 1 FROM pg_database`).
			WithArgs(opts.cloneName).
			WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
		mock.ExpectQuery(`SELECT has_database_privilege`).
			WithArgs(opts.cloneName).
			WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
		expectTargetRestoreQueries(mock)
	} else {
		mock.ExpectQuery(`SELECT rolcreatedb`).
			WillReturnRows(sqlmock.NewRows([]string{"rolcreatedb"}).AddRow(true))
		mock.ExpectQuery(`FROM pg_extension`).
			WillReturnRows(sqlmock.NewRows([]string{"extname"}))
	}

	mock.ExpectQuery(`SHOW server_version_num`).
		WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(opts.sourceVer))
	if opts.crossInst {
		mock.ExpectQuery(`SHOW server_version_num`).
			WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(opts.targetVer))
	}
}

func replicationTargetDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestPreflightMatrix(t *testing.T) {
	sourceDSN := "postgres://u:p@h-a:5432/db_src"
	cloneName := "db_clone"
	replTarget := replicationTargetDir(t)

	tests := []struct {
		name      string
		opts      Options
		strategy  Strategy
		setup     func(sqlmock.Sqlmock)
		lookPath  func(string) (string, error)
		pgDumpVer func() (string, error)
		wantErr   bool
		errKind   PreflightKind
		errSubstr string
	}{
		{
			name:     "schema-replay happy path skip create",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				expectPreflightSchemaReplay(m, struct {
					sourceDB, cloneName   string
					skipCreate, crossInst bool
					sourceVer, targetVer  int
				}{sourceDB: "db_src", cloneName: cloneName, skipCreate: true})
			},
			lookPath:  func(string) (string, error) { return "/usr/bin/x", nil },
			pgDumpVer: func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil },
		},
		{
			name:     "source unreachable stops early",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing().WillReturnError(errors.New("connection refused"))
			},
			wantErr:   true,
			errKind:   PreflightReachability,
			errSubstr: "db_src",
		},
		{
			name:     "missing CREATEDB",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: false},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("app_user"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				expectSourceReadPrivilegeQueries(m)
				m.ExpectQuery(`SELECT rolcreatedb`).
					WillReturnRows(sqlmock.NewRows([]string{"rolcreatedb"}).AddRow(false))
			},
			lookPath:  func(string) (string, error) { return "/usr/bin/x", nil },
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "CREATEDB",
		},
		{
			name: "cross-major downgrade",
			opts: Options{
				SourceDSN:  sourceDSN,
				CloneName:  cloneName,
				TargetDSN:  "postgres://u:p@h-b:5432/db_b",
				SkipCreate: true,
			},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				expectPreflightSchemaReplay(m, struct {
					sourceDB, cloneName   string
					skipCreate, crossInst bool
					sourceVer, targetVer  int
				}{
					sourceDB: "db_src", cloneName: cloneName, skipCreate: true, crossInst: true,
					sourceVer: 160000, targetVer: 150002,
				})
			},
			lookPath: func(string) (string, error) { return "/usr/bin/x", nil },
			wantErr:  true,
			errKind:  PreflightVersion,
		},
		{
			name:     "pg_dump major mismatch",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				expectPreflightSchemaReplay(m, struct {
					sourceDB, cloneName   string
					skipCreate, crossInst bool
					sourceVer, targetVer  int
				}{sourceDB: "db_src", cloneName: cloneName, skipCreate: true, sourceVer: 150002})
			},
			lookPath:  func(string) (string, error) { return "/usr/bin/pg_dump", nil },
			pgDumpVer: func() (string, error) { return "pg_dump (PostgreSQL) 16.0", nil },
			wantErr:   true,
			errKind:   PreflightVersion,
			errSubstr: "pg_dump",
		},
		{
			name: "template cross-instance blocked",
			opts: Options{
				SourceDSN: sourceDSN,
				CloneName: cloneName,
				TargetDSN: "postgres://u:p@h-b:5432/db_b",
			},
			strategy: &TemplateStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectPing()
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "cross-server",
		},
		{
			name:     "physical-backup happy path",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, TargetDir: replTarget},
			strategy: &ReplicationStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SHOW wal_level`).
					WillReturnRows(sqlmock.NewRows([]string{"wal_level"}).AddRow("replica"))
				m.ExpectQuery(`SHOW max_wal_senders`).
					WillReturnRows(sqlmock.NewRows([]string{"max_wal_senders"}).AddRow(4))
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("repl"))
				m.ExpectQuery(`SELECT rolreplication, rolsuper`).
					WillReturnRows(sqlmock.NewRows([]string{"rolreplication", "rolsuper"}).AddRow(true, false))
			},
			lookPath: func(name string) (string, error) {
				if name == "pg_basebackup" {
					return "/usr/bin/pg_basebackup", nil
				}
				return "/usr/bin/x", nil
			},
		},
		{
			name:     "physical-backup missing target dir",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName},
			strategy: &ReplicationStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "target-dir",
		},
		{
			name:     "physical-backup wal_level too low",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, TargetDir: replTarget},
			strategy: &ReplicationStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SHOW wal_level`).
					WillReturnRows(sqlmock.NewRows([]string{"wal_level"}).AddRow("minimal"))
			},
			wantErr:   true,
			errKind:   PreflightVersion,
			errSubstr: "wal_level",
		},
		{
			name:     "physical-backup max_wal_senders too low",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, TargetDir: replTarget},
			strategy: &ReplicationStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SHOW wal_level`).
					WillReturnRows(sqlmock.NewRows([]string{"wal_level"}).AddRow("replica"))
				m.ExpectQuery(`SHOW max_wal_senders`).
					WillReturnRows(sqlmock.NewRows([]string{"max_wal_senders"}).AddRow(1))
			},
			wantErr:   true,
			errKind:   PreflightVersion,
			errSubstr: "max_wal_senders",
		},
		{
			name:     "physical-backup missing REPLICATION privilege",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, TargetDir: replTarget},
			strategy: &ReplicationStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SHOW wal_level`).
					WillReturnRows(sqlmock.NewRows([]string{"wal_level"}).AddRow("replica"))
				m.ExpectQuery(`SHOW max_wal_senders`).
					WillReturnRows(sqlmock.NewRows([]string{"max_wal_senders"}).AddRow(4))
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("app_user"))
				m.ExpectQuery(`SELECT rolreplication, rolsuper`).
					WillReturnRows(sqlmock.NewRows([]string{"rolreplication", "rolsuper"}).AddRow(false, false))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "REPLICATION",
		},
		{
			name:     "physical-backup pg_basebackup not on PATH",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, TargetDir: replTarget},
			strategy: &ReplicationStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SHOW wal_level`).
					WillReturnRows(sqlmock.NewRows([]string{"wal_level"}).AddRow("replica"))
				m.ExpectQuery(`SHOW max_wal_senders`).
					WillReturnRows(sqlmock.NewRows([]string{"max_wal_senders"}).AddRow(4))
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("repl"))
				m.ExpectQuery(`SELECT rolreplication, rolsuper`).
					WillReturnRows(sqlmock.NewRows([]string{"rolreplication", "rolsuper"}).AddRow(true, false))
			},
			lookPath:  func(string) (string, error) { return "", fmt.Errorf("not found") },
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "pg_basebackup",
		},
		{
			name:     "logical-stream happy path with tools",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: false},
			strategy: &CopyStreamStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT rolcreatedb`).
					WillReturnRows(sqlmock.NewRows([]string{"rolcreatedb"}).AddRow(true))
				m.ExpectQuery(`SHOW server_version_num`).
					WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(150002))
			},
			lookPath:  func(string) (string, error) { return "/usr/bin/x", nil },
			pgDumpVer: func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil },
		},
		{
			name:     "logical-stream skips pg_dump and source-read checks",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: false},
			strategy: &CopyStreamStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT rolcreatedb`).
					WillReturnRows(sqlmock.NewRows([]string{"rolcreatedb"}).AddRow(true))
				m.ExpectQuery(`SHOW server_version_num`).
					WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(150002))
			},
			lookPath: func(string) (string, error) { return "", fmt.Errorf("not found") },
		},
		{
			name: "extension unavailable on target server",
			opts: Options{
				SourceDSN:  sourceDSN,
				CloneName:  cloneName,
				TargetDSN:  "postgres://u:p@h-b:5432/db_b",
				SkipCreate: true,
			},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_proc p`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`pg_available_extensions`).
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "postgis",
		},
		{
			name:     "insufficient read on user table",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("reader"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "relkind"}).AddRow("billing", "accounts", "r"))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "billing.accounts",
		},
		{
			name:     "insufficient read on view",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("reader"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "relkind"}).AddRow("billing", "v_accounts", "v"))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "view",
		},
		{
			name:     "FK referenced table missing SELECT",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("reader"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnRows(sqlmock.NewRows([]string{"from_schema", "from_table", "ref_schema", "ref_table"}).
						AddRow("app", "orders", "app", "customers"))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "app.customers",
		},
		{
			name:     "sequence missing USAGE",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("reader"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname"}).AddRow("app", "users_id_seq"))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "sequence",
		},
		{
			name: "admin unreachable on cross-instance",
			opts: Options{
				SourceDSN:  sourceDSN,
				CloneName:  cloneName,
				TargetDSN:  "postgres://u:p@h-b:5432/db_b",
				SkipCreate: true,
			},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectPing().WillReturnError(errors.New("admin unreachable"))
			},
			wantErr:   true,
			errKind:   PreflightReachability,
			errSubstr: "postgres",
		},
		{
			name:     "skip create missing target database",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_proc p`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}))
				m.ExpectQuery(`SELECT 1 FROM pg_database`).
					WithArgs(cloneName).
					WillReturnError(sql.ErrNoRows)
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: cloneName,
		},
		{
			name:     "pg_dump not on PATH",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_proc p`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}))
				m.ExpectQuery(`SELECT 1 FROM pg_database`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				expectTargetRestoreQueries(m)
			},
			lookPath:  func(string) (string, error) { return "", fmt.Errorf("not found") },
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "pg_dump",
		},
		{
			name:     "missing schema USAGE on source",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("reader"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname"}).AddRow("app"))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "app (schema)",
		},
		{
			name:     "missing type USAGE on source",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("reader"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname", "typname"}).AddRow("app", "status"))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "app.status",
		},
		{
			name:     "function not dump-visible on source",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("reader"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_proc p`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname", "proname"}).AddRow("app", "secret_fn"))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "app.secret_fn",
		},
		{
			name:     "target extension not installed and not creatable",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_proc p`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`pg_available_extensions`).
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				m.ExpectQuery(`SELECT 1 FROM pg_database`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_extension WHERE extname`).
					WithArgs("postgis").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
				m.ExpectQuery(`pg_available_extensions`).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(false))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "postgis",
		},
		{
			name:     "target extension creatable when not installed",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_proc p`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`pg_available_extensions`).
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				m.ExpectQuery(`SELECT 1 FROM pg_database`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_extension WHERE extname`).
					WithArgs("postgis").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
				m.ExpectQuery(`pg_available_extensions`).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT DISTINCT n.nspname`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname"}))
				m.ExpectQuery(`SHOW server_version_num`).
					WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(150002))
			},
			lookPath:  func(string) (string, error) { return "/usr/bin/x", nil },
			pgDumpVer: func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil },
		},
		{
			name:     "target extension already installed skips creatable probe",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`SELECT n.nspname, c.relname, c.relkind`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_constraint con`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_sequences`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_namespace n`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_type t`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_proc p`).
					WillReturnError(sql.ErrNoRows)
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`pg_available_extensions`).
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				m.ExpectQuery(`SELECT 1 FROM pg_database`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_extension WHERE extname`).
					WithArgs("postgis").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				m.ExpectQuery(`SELECT DISTINCT n.nspname`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname"}))
				m.ExpectQuery(`SHOW server_version_num`).
					WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(150002))
			},
			lookPath:  func(string) (string, error) { return "/usr/bin/x", nil },
			pgDumpVer: func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil },
		},
		{
			name:     "missing target schema USAGE",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				expectSourceReadPrivilegeQueries(m)
				m.ExpectQuery(`SELECT 1 FROM pg_database`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}))
				m.ExpectQuery(`SELECT DISTINCT n.nspname`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname"}).AddRow("app"))
				m.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_namespace WHERE nspname`).
					WithArgs("app").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				m.ExpectQuery(`has_schema_privilege`).
					WithArgs("app").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(false))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "USAGE",
		},
		{
			name:     "missing target schema CREATE",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: true},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("u"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				expectSourceReadPrivilegeQueries(m)
				m.ExpectQuery(`SELECT 1 FROM pg_database`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs(cloneName).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}))
				m.ExpectQuery(`SELECT DISTINCT n.nspname`).
					WillReturnRows(sqlmock.NewRows([]string{"nspname"}).AddRow("app"))
				m.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_namespace WHERE nspname`).
					WithArgs("app").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				m.ExpectQuery(`has_schema_privilege`).
					WithArgs("app").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				m.ExpectQuery(`has_schema_privilege`).
					WithArgs("app").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(false))
			},
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "CREATE",
		},
		{
			name:     "admin cannot CREATE EXTENSION when skip create false",
			opts:     Options{SourceDSN: sourceDSN, CloneName: cloneName, SkipCreate: false},
			strategy: &SchemaReplayStrategy{},
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectPing()
				m.ExpectQuery(`SELECT current_user`).
					WillReturnRows(sqlmock.NewRows([]string{"current_user"}).AddRow("app_user"))
				m.ExpectQuery(`SELECT has_database_privilege`).
					WithArgs("db_src").
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				expectSourceReadPrivilegeQueries(m)
				m.ExpectQuery(`SELECT rolcreatedb`).
					WillReturnRows(sqlmock.NewRows([]string{"rolcreatedb"}).AddRow(true))
				m.ExpectQuery(`FROM pg_extension`).
					WillReturnRows(sqlmock.NewRows([]string{"extname"}).AddRow("postgis"))
				m.ExpectQuery(`pg_available_extensions`).
					WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(false))
			},
			lookPath:  func(string) (string, error) { return "/usr/bin/x", nil },
			wantErr:   true,
			errKind:   PreflightPermission,
			errSubstr: "postgis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mock := newSQLMock(t)
			defer mockDB.Close()

			origOpen := sqlOpenDB
			sqlOpenDB = func(dsn string) (*sql.DB, error) { return mockDB, nil }
			defer func() { sqlOpenDB = origOpen }()

			origLook := lookPath
			if tt.lookPath != nil {
				lookPath = tt.lookPath
			} else {
				lookPath = func(string) (string, error) { return "/usr/bin/x", nil }
			}
			defer func() { lookPath = origLook }()

			origDump := pgDumpVersion
			if tt.pgDumpVer != nil {
				pgDumpVersion = tt.pgDumpVer
			} else {
				pgDumpVersion = func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil }
			}
			defer func() { pgDumpVersion = origDump }()

			tt.setup(mock)

			err := Preflight(context.Background(), tt.opts, tt.strategy)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				var pe *PreflightError
				if !errors.As(err, &pe) {
					t.Fatalf("expected PreflightError, got %T: %v", err, err)
				}
				if pe.Kind != tt.errKind {
					t.Fatalf("kind %q want %q", pe.Kind, tt.errKind)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q missing %q", err.Error(), tt.errSubstr)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatalf("unexpected follow-up queries: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateReplicationTargetDir(t *testing.T) {
	t.Run("non-existent path", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "new-data")
		if err := validateReplicationTargetDir(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := validateReplicationTargetDir(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-empty directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := validateReplicationTargetDir(dir)
		if err == nil {
			t.Fatal("expected error")
		}
		var pe *PreflightError
		if !errors.As(err, &pe) || pe.Kind != PreflightPermission {
			t.Fatalf("got %v", err)
		}
		if !strings.Contains(err.Error(), "empty or non-existent") {
			t.Fatalf("error = %v", err)
		}
	})
}
