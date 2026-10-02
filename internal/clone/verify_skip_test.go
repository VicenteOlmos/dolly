package clone

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestSchemaReplaySkipsVerifyWhenDisabled(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		db, _, err := sqlmock.New()
		return db, err
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

	origFidelity := applyTargetFidelity
	applyTargetFidelity = func(ctx context.Context, source, target *sql.DB, restore func() error) error {
		return restore()
	}
	defer func() { applyTargetFidelity = origFidelity }()

	verifyCalled := false
	origVerify := runSchemaReplayVerify
	runSchemaReplayVerify = func(context.Context, Options, *sql.DB, *sql.DB, bool) ([]string, int, error) {
		verifyCalled = true
		return nil, 0, nil
	}
	defer func() { runSchemaReplayVerify = origVerify }()

	mockRunner := &mockCommandRunner{}
	err := (&SchemaReplayStrategy{Runner: mockRunner}).Execute(context.Background(), Options{
		SourceDSN:  "postgres://u:p@h-a:5432/db_src",
		CloneName:  "db_clone",
		SkipCreate: true,
		Verify:     false,
		DumpOpts:   []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if verifyCalled {
		t.Fatal("runSchemaReplayVerify should not run when Verify is false")
	}
}

func TestSchemaReplayRecordsVerifyTableCount(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	defer func() { lookPath = origLookPath }()

	origOpenDB := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) {
		db, _, err := sqlmock.New()
		return db, err
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

	origFidelity := applyTargetFidelity
	applyTargetFidelity = func(ctx context.Context, source, target *sql.DB, restore func() error) error {
		return restore()
	}
	defer func() { applyTargetFidelity = origFidelity }()

	origVerify := runSchemaReplayVerify
	runSchemaReplayVerify = func(context.Context, Options, *sql.DB, *sql.DB, bool) ([]string, int, error) {
		return []string{"verify: public.orders row count source=2 target=1"}, 4, nil
	}
	defer func() { runSchemaReplayVerify = origVerify }()

	var warnings []string
	tables := -1
	err := (&SchemaReplayStrategy{Runner: &mockCommandRunner{}}).Execute(context.Background(), Options{
		SourceDSN:      "postgres://u:p@h-a:5432/db_src",
		CloneName:      "db_clone",
		SkipCreate:     true,
		Verify:         true,
		VerifyWarnings: &warnings,
		VerifyTables:   &tables,
		DumpOpts:       []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tables != 4 {
		t.Fatalf("tables = %d, want 4", tables)
	}
	if len(warnings) != 1 || warnings[0] != "verify: public.orders row count source=2 target=1" {
		t.Fatalf("warnings = %#v", warnings)
	}
}
