package tui

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestProductionDumpRunnerHonorsExcludeSchemas(t *testing.T) {
	var dumpSchemas []string
	var captureSchemas []string

	oldLoad := tuiLoadConfig
	oldRun := tuiDumpRun
	oldCapture := tuiSchemaCapture
	t.Cleanup(func() {
		tuiLoadConfig = oldLoad
		tuiDumpRun = oldRun
		tuiSchemaCapture = oldCapture
	})

	cfg := config.DefaultConfig()
	cfg.Dump.ExcludeSchemas = []string{"staging"}
	tuiLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	tuiDumpRun = func(_ context.Context, _ *sql.DB, _ string, opts ...dump.Option) error {
		dumpSchemas = append([]string(nil), dump.InspectSchemas(opts...)...)
		return nil
	}
	tuiSchemaCapture = func(_ context.Context, _ string, _ string, schemas []string) error {
		captureSchemas = append([]string(nil), schemas...)
		return nil
	}

	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := productionDumpRunner{}
	err = runner.Run(context.Background(), conn, t.TempDir(), DumpDraft{}, []string{"app", "staging"}, "db", "postgres://u:p@h/db", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(dumpSchemas) != 1 || dumpSchemas[0] != "app" {
		t.Fatalf("dump schemas = %v, want [app]", dumpSchemas)
	}
	if len(captureSchemas) != 1 || captureSchemas[0] != "app" {
		t.Fatalf("schema capture schemas = %v, want [app]", captureSchemas)
	}
}

func TestProductionDumpRunnerExcludeSchemasEmptyScope(t *testing.T) {
	oldLoad := tuiLoadConfig
	oldRun := tuiDumpRun
	t.Cleanup(func() {
		tuiLoadConfig = oldLoad
		tuiDumpRun = oldRun
	})

	cfg := config.DefaultConfig()
	cfg.Dump.ExcludeSchemas = []string{"app"}
	tuiLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	tuiDumpRun = func(context.Context, *sql.DB, string, ...dump.Option) error {
		t.Fatal("dump should not run when exclusions empty schema scope")
		return nil
	}

	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := productionDumpRunner{}
	err = runner.Run(context.Background(), conn, t.TempDir(), DumpDraft{}, []string{"app"}, "db", "", nil)
	if err == nil || !strings.Contains(err.Error(), "empty after applying exclude schemas") {
		t.Fatalf("err = %v", err)
	}
}
