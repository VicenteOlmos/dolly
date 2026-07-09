package clone

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestSchemasFromOptionsPrefersDumpOpts(t *testing.T) {
	opts := Options{
		DumpOpts:    []dump.Option{dump.WithSchemas([]string{"app", "billing"})},
		RestoreOpts: []restore.Option{restore.WithSchemas([]string{"public"})},
	}
	got := SchemasFromOptions(opts)
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("got %v", got)
	}
}

func TestRunInProcessUnknownStrategy(t *testing.T) {
	err := RunInProcess(context.Background(), Options{
		SourceDSN: "postgres://u:p@h/db",
		CloneName: "clone_db",
		Strategy:  "magic",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown clone strategy") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunInProcessProductionScaleUnknown(t *testing.T) {
	err := RunInProcess(context.Background(), Options{
		SourceDSN: "postgres://u:p@h/db",
		CloneName: "clone_db",
		Strategy:  "production-scale",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown clone strategy") {
		t.Fatalf("expected unknown clone strategy error, got: %v", err)
	}
}

func TestRunInProcessTemplateDelegates(t *testing.T) {
	origOpen := sqlOpenDB
	defer func() { sqlOpenDB = origOpen }()
	sqlOpenDB = func(string) (*sql.DB, error) {
		return nil, errors.New("stop after template pre-check")
	}

	err := RunInProcess(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		TargetDSN:  "postgres://u:p@h-b:5432/other",
		CloneName:  "db_clone",
		Strategy:   "template",
		SkipCreate: true,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "same PostgreSQL instance") {
		t.Fatalf("err = %v, want same-instance error from template strategy", err)
	}
}

func TestInProcessDumpRestoreAppliesSchemas(t *testing.T) {
	origApply := applySchemasFunc
	origDump := dumpFunc
	origRestore := restoreFunc
	origOpen := sqlOpenDB
	origSeq := restoreSequencesFunc
	defer func() {
		applySchemasFunc = origApply
		dumpFunc = origDump
		restoreFunc = origRestore
		sqlOpenDB = origOpen
		restoreSequencesFunc = origSeq
	}()

	var applied []string
	applySchemasFunc = func(_ context.Context, _, _ *sql.DB, schemas []string) error {
		applied = append([]string(nil), schemas...)
		return nil
	}
	dumpFunc = func(context.Context, *sql.DB, string, ...dump.Option) error { return nil }
	restoreFunc = func(context.Context, *sql.DB, string, ...restore.Option) error { return nil }
	restoreSequencesFunc = func(context.Context, *sql.DB, *sql.DB) error { return nil }

	srcDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	tgtDB, _, err2 := sqlmock.New()
	if err2 != nil {
		t.Fatal(err2)
	}
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		if strings.Contains(dsn, "db_clone") {
			return tgtDB, nil
		}
		return srcDB, nil
	}

	runErr := inProcessDumpRestore(context.Background(), Options{
		SourceDSN:   "postgres://u:p@h-a:5432/db_src",
		CloneName:   "db_clone",
		SkipCreate:  true,
		DumpOpts:    []dump.Option{dump.WithSchemas([]string{"public", "app"})},
		RestoreOpts: []restore.Option{restore.WithSchemas([]string{"public", "app"})},
	}, nil)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if len(applied) != 2 || applied[0] != "public" || applied[1] != "app" {
		t.Fatalf("applied schemas = %v", applied)
	}
}
