package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/dumphistory"
)

const stubDumpMetadataJSON = `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": []
}`

func writeStubDumpMetadata(dir string) error {
	return os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(stubDumpMetadataJSON), 0o644)
}

func slowResumeExpect(sig string, schemas []string, sanitized bool) resumableDumpExpectation {
	return resumableDumpExpectation{
		sourceSignature:     sig,
		schemas:             schemas,
		sanitizationEnabled: sanitized,
	}
}

func TestParseDumpFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    dumpFlags
		wantErr string
	}{
		{
			name: "valid minimal short flags",
			args: []string{"-dsn", "postgres://h-a/db_a", "-output", "/tmp/out"},
			want: dumpFlags{
				DSN:    "postgres://h-a/db_a",
				Output: "/tmp/out",
			},
		},
		{
			name: "valid long flag names",
			args: []string{"--dsn", "postgres://h-a/db_a", "--output", "/tmp/out"},
			want: dumpFlags{
				DSN:    "postgres://h-a/db_a",
				Output: "/tmp/out",
			},
		},
		{
			name: "no transaction enabled",
			args: []string{
				"--dsn", "postgres://h-a/db_a",
				"--output", "/tmp/out",
				"--no-transaction",
			},
			want: dumpFlags{
				DSN:           "postgres://h-a/db_a",
				Output:        "/tmp/out",
				NoTransaction: true,
			},
		},
		{
			name: "slow connection enabled",
			args: []string{
				"--dsn", "postgres://h-a/db_a",
				"--output", "/tmp/out",
				"--slow-connection",
			},
			want: dumpFlags{
				DSN:            "postgres://h-a/db_a",
				Output:         "/tmp/out",
				SlowConnection: true,
			},
		},
		{
			name: "subset limit flags",
			args: []string{
				"--dsn", "postgres://h-a/db_a",
				"--output", "/tmp/out",
				"--seed-file", "seeds.json",
				"--max-depth", "3",
				"--max-tables", "5",
				"--max-rows", "100",
				"--max-in-list-size", "50",
			},
			want: dumpFlags{
				DSN:           "postgres://h-a/db_a",
				Output:        "/tmp/out",
				SeedFile:      "seeds.json",
				MaxDepth:      3,
				MaxTables:     5,
				MaxRows:       100,
				MaxInListSize: 50,
			},
		},
		{
			name: "chunk size flag",
			args: []string{
				"--dsn", "postgres://h-a/db_a",
				"--output", "/tmp/out",
				"--slow-connection",
				"--chunk-size", "500",
			},
			want: dumpFlags{
				DSN:            "postgres://h-a/db_a",
				Output:         "/tmp/out",
				SlowConnection: true,
				ChunkSize:      500,
			},
		},
		{
			name: "retry flags",
			args: []string{
				"--dsn", "postgres://h-a/db_a",
				"--output", "/tmp/out",
				"--slow-connection",
				"--retry-max", "3",
				"--retry-base", "1s",
			},
			want: dumpFlags{
				DSN:            "postgres://h-a/db_a",
				Output:         "/tmp/out",
				SlowConnection: true,
				RetryMax:       3,
				RetryBase:      "1s",
			},
		},
		{
			name: "connection without dsn",
			args: []string{"--connection", "prod", "--output", "/tmp/out"},
			want: dumpFlags{
				Connection: "prod",
				Output:     "/tmp/out",
			},
		},
		{
			name: "missing dsn — both empty OK at parse time (.env fallback deferred to run)",
			args: []string{"--output", "/tmp/out"},
			want: dumpFlags{Output: "/tmp/out"},
		},
		{
			name: "connection and dsn conflict",
			args: []string{
				"--dsn", "postgres://h-a/db_a",
				"--connection", "prod",
				"--output", "/tmp/out",
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "missing output — now optional at parse time",
			args: []string{"-dsn", "postgres://h-a/db_a"},
			want: dumpFlags{DSN: "postgres://h-a/db_a"},
		},
		{
			name:    "unknown flag",
			args:    []string{"-dsn", "postgres://h-a/db_a", "-output", "/tmp/out", "-bogus"},
			wantErr: "flag provided but not defined: -bogus",
		},
		{
			name: "empty args — output now optional at parse time",
			args: nil,
			want: dumpFlags{},
		},
		{
			name:    "short help",
			args:    []string{"-h"},
			wantErr: errHelp.Error(),
		},
		{
			name:    "long help",
			args:    []string{"--help"},
			wantErr: errHelp.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDumpFlags(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErr == errHelp.Error() {
					if !errors.Is(err, errHelp) {
						t.Fatalf("error = %v, want errHelp", err)
					}
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRunDumpHelpNoDB(t *testing.T) {
	err := runDump([]string{"--help"})
	if err != nil {
		t.Fatalf("runDump help: %v", err)
	}
}

func TestRunDumpEngineFailure(t *testing.T) {
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(context.Context, *sql.DB, string, ...dump.Option) error {
		return errors.New("export failed: disk full")
	}

	outDir := t.TempDir()
	args := []string{"--dsn", "postgres://h-a/db_a", "--output", outDir}

	err := runDump(args)
	if err == nil {
		t.Fatal("expected dump engine error")
	}
	if !strings.Contains(err.Error(), "dump:") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %q, want dump engine detail", err.Error())
	}

	stderr := captureStderr(func() {
		exit := dispatch(append([]string{"dolly", "dump"}, args...))
		if exit != 1 {
			t.Fatalf("dispatch exit = %d, want 1", exit)
		}
	})
	if !strings.Contains(stderr, "disk full") {
		t.Fatalf("stderr missing engine detail:\n%s", stderr)
	}
}

func TestBuildDumpOptionsSeedFileReachesDump(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	seedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}],"limits":{"max_depth":3,"max_tables":5,"max_rows":100,"max_in_list_size":50}}`
	if err := os.WriteFile(seedFile, []byte(seedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	flags := dumpFlags{
		DSN:           "postgres://h-a/db_a",
		Output:        t.TempDir(),
		SeedFile:      seedFile,
		MaxDepth:      3,
		MaxTables:     5,
		MaxRows:       100,
		MaxInListSize: 50,
	}

	opts, err := buildDumpOptions(flags, config.DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset == nil {
		t.Fatal("expected subset config in opts")
	}
	if len(subset.Seeds) != 1 {
		t.Fatalf("expected 1 seed, got %d", len(subset.Seeds))
	}
	if subset.Seeds[0].Table != "tbl_a" {
		t.Fatalf("expected table tbl_a, got %q", subset.Seeds[0].Table)
	}
	if subset.Limits.MaxDepth != 3 {
		t.Fatalf("expected max_depth 3, got %d", subset.Limits.MaxDepth)
	}
	if subset.Limits.MaxTables != 5 {
		t.Fatalf("expected max_tables 5, got %d", subset.Limits.MaxTables)
	}
	if subset.Limits.MaxRows != 100 {
		t.Fatalf("expected max_rows 100, got %d", subset.Limits.MaxRows)
	}
	if subset.Limits.MaxInListSize != 50 {
		t.Fatalf("expected max_in_list_size 50, got %d", subset.Limits.MaxInListSize)
	}
}

func TestBuildDumpOptionsLimitOverrides(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	seedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`
	if err := os.WriteFile(seedFile, []byte(seedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	flags := dumpFlags{
		DSN:           "postgres://h-a/db_a",
		Output:        t.TempDir(),
		SeedFile:      seedFile,
		MaxDepth:      7,
		MaxTables:     12,
		MaxRows:       5000,
		MaxInListSize: 200,
	}

	opts, err := buildDumpOptions(flags, config.DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset == nil {
		t.Fatal("expected subset config in opts")
	}
	if subset.Limits.MaxDepth != 7 {
		t.Fatalf("expected max_depth 7, got %d", subset.Limits.MaxDepth)
	}
	if subset.Limits.MaxTables != 12 {
		t.Fatalf("expected max_tables 12, got %d", subset.Limits.MaxTables)
	}
	if subset.Limits.MaxRows != 5000 {
		t.Fatalf("expected max_rows 5000, got %d", subset.Limits.MaxRows)
	}
	if subset.Limits.MaxInListSize != 200 {
		t.Fatalf("expected max_in_list_size 200, got %d", subset.Limits.MaxInListSize)
	}
}

func TestBuildDumpOptionsWithoutSeedFileIsFullDump(t *testing.T) {
	flags := dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
	}

	opts, err := buildDumpOptions(flags, config.DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset != nil {
		t.Fatal("expected no subset config for full dump")
	}
}

func TestBuildDumpOptionsSlowConnection(t *testing.T) {
	flags := dumpFlags{
		DSN:            "postgres://h-a/db_a",
		Output:         t.TempDir(),
		SlowConnection: true,
	}

	opts, err := buildDumpOptions(flags, config.DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !dump.InspectSlowConnection(opts...) {
		t.Fatal("expected slow-connection mode in opts")
	}
}

func TestBuildDumpOptionsSlowConnectionWithSeedFileConflict(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	seedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`
	if err := os.WriteFile(seedFile, []byte(seedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	flags := dumpFlags{
		DSN:            "postgres://h-a/db_a",
		Output:         t.TempDir(),
		SlowConnection: true,
		SeedFile:       seedFile,
	}

	_, err := buildDumpOptions(flags, config.DefaultConfig())
	if err == nil {
		t.Fatal("expected error for --slow-connection + --seed-file conflict")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("error = %q, want 'incompatible'", err.Error())
	}
}

func TestRunDumpConnectionDisabled(t *testing.T) {
	oldLoad := dumpLoadConfig
	dumpLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { dumpLoadConfig = oldLoad })

	err := runDump([]string{"--connection", "prod", "--output", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "save_connections is disabled") {
		t.Fatalf("err = %v, want save_connections disabled", err)
	}
}

func TestRunDumpUnknownSchemaLeavesOutputEmptyAndRedactsDSN(t *testing.T) {
	const secret = "SecretPass123"
	stubDumpListSchemaNames(t, []string{"public", "app"}, nil)

	oldPing := dumpPingContext
	oldRun := dumpRun
	oldLoad := dumpLoadConfig
	t.Cleanup(func() {
		dumpPingContext = oldPing
		dumpRun = oldRun
		dumpLoadConfig = oldLoad
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(context.Context, *sql.DB, string, ...dump.Option) error {
		t.Fatal("dumpRun should not run when schema validation fails")
		return nil
	}

	baseDir := t.TempDir()
	stderr := captureStderr(func() {
		err := runDump([]string{
			"--dsn", "postgres://user:" + secret + "@host:5432/db",
			"--output", baseDir,
			"--schemas", "missing",
		})
		if err == nil {
			t.Fatal("expected unknown schema error")
		}
		if !strings.Contains(err.Error(), "unknown schema") {
			t.Fatalf("err = %q, want unknown schema", err.Error())
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks password: %q", err.Error())
		}
	})
	if strings.Contains(stderr, secret) {
		t.Fatalf("stderr leaks password: %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "1")); !os.IsNotExist(err) {
		t.Fatal("output dir should not be allocated before dump runs")
	}
}

func TestRunDumpConnectionPassesSchemasToDump(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	store, err := connections.OpenStore(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(connections.Connection{
		Name: "prod", Host: "h-a", Port: "5432", Database: "db_a",
		User: "u", Password: "p", Schemas: []string{"app"},
	}); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	stubDumpListSchemaNames(t, []string{"public", "app"}, nil)

	oldLoad := dumpLoadConfig
	dumpLoadConfig = func(path string) (*config.Config, error) {
		c := config.DefaultConfig()
		c.SaveConnections = true
		return c, nil
	}
	t.Cleanup(func() { dumpLoadConfig = oldLoad })

	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	var captured []dump.Option
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, out string, opts ...dump.Option) error {
		captured = opts
		return writeStubDumpMetadata(out)
	}

	outDir := t.TempDir()
	if err := runDump([]string{"--connection", "prod", "--output", outDir}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	got := dump.InspectSchemas(captured...)
	if len(got) != 1 || got[0] != "app" {
		t.Fatalf("schemas = %v, want [app]", got)
	}
}

func TestRunDumpEnvFallbackWithoutDSNFlag(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := dumpLoadConfig
	origPing := dumpPingContext
	origRun := dumpRun
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		dumpLoadConfig = origLoad
		dumpPingContext = origPing
		dumpRun = origRun
	})

	envCalled := false
	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		envCalled = true
		return "postgres://envuser:envpass@envhost/envdb", nil
	}
	dumpLoadConfig = func(string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		return writeStubDumpMetadata(out)
	}

	if err := runDump([]string{"--output", t.TempDir()}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	if !envCalled {
		t.Fatal("expected .env resolution when --dsn and --connection are omitted")
	}
}

func TestRunDumpNoEnvNoFlagsReturnsError(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := dumpLoadConfig
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		dumpLoadConfig = origLoad
	})

	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		return "", config.ErrSourceDSNNotFound
	}
	dumpLoadConfig = func(string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}

	err := runDump([]string{"--output", t.TempDir()})
	if err == nil {
		t.Fatal("expected error when no flags and .env missing")
	}
	if !strings.Contains(err.Error(), ".env") {
		t.Fatalf("error = %q, want mention of .env", err.Error())
	}
}

func TestRunDumpOutputFlagOverridesConfig(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	cfg := config.DefaultConfig()
	cfg.Dump.OutputDir = "config_default_dir"
	dumpLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	flagOutput := t.TempDir()
	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", flagOutput}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	wantOutput := filepath.Join(flagOutput, "1")
	if capturedOutput != wantOutput {
		t.Fatalf("output = %q, want %q (flag base + seq 1)", capturedOutput, wantOutput)
	}
}

func TestRunDumpOutputFallsBackToConfig(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	configDir := filepath.Join(t.TempDir(), "my_config_dump_dir")
	cfg := config.DefaultConfig()
	cfg.Dump.OutputDir = configDir
	dumpLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db"}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	wantOutput := filepath.Join(configDir, "1")
	if capturedOutput != wantOutput {
		t.Fatalf("output = %q, want %q (config base + seq 1)", capturedOutput, wantOutput)
	}
}

func TestRunDumpEmptyOutputConfigErrors(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
	})

	cfg := config.DefaultConfig()
	cfg.Dump.OutputDir = ""
	dumpLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	err := runDump([]string{"--dsn", "postgres://h/db"})
	if err == nil {
		t.Fatal("expected error when --output omitted and config dump.output_dir empty")
	}
	if !strings.Contains(err.Error(), "required flag --output or config dump.output_dir") {
		t.Fatalf("error = %q, want mention of required flag", err.Error())
	}
}

func TestRunDumpMissingDSNExits1(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := dumpLoadConfig
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		dumpLoadConfig = origLoad
	})

	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		return "", config.ErrSourceDSNNotFound
	}
	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }

	err := runDump([]string{"--output", t.TempDir()})
	if err == nil {
		t.Fatal("expected error when DSN cannot be resolved")
	}
}

func TestRunDumpRegisterFailureWarnsAndSucceeds(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	outDir := filepath.Join(workDir, "dumps")
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		return writeStubDumpMetadata(out)
	}

	store, err := dumphistory.OpenStore(config.DefaultConfig(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Register(dumphistory.Record{
		Seq: 1, BaseDir: outDir, Path: filepath.Join(outDir, "0"),
	}); err != nil {
		t.Fatal(err)
	}
	dollyDir := filepath.Join(workDir, ".dolly")
	if err := os.Chmod(dollyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dollyDir, 0o755)
	})

	args := []string{"--dsn", "postgres://h/db", "--output", outDir}
	stderr := captureStderr(func() {
		if err := runDump(args); err != nil {
			t.Fatalf("runDump: %v", err)
		}
	})
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "register dump history") {
		t.Fatalf("stderr = %q, want warning about register dump history", stderr)
	}

	exit := dispatch(append([]string{"dolly", "dump"}, args...))
	if exit != 0 {
		t.Fatalf("dispatch exit = %d, want 0", exit)
	}
}

func TestRunDumpRegistersHistory(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	outDir := filepath.Join(workDir, "dumps")
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		return writeStubDumpMetadata(out)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", outDir}); err != nil {
		t.Fatalf("runDump: %v", err)
	}

	storePath := filepath.Join(workDir, ".dolly", "dump-history.json")
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read history file: %v", err)
	}
	var doc struct {
		Records []dumphistory.Record `json:"records"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse history: %v", err)
	}
	if len(doc.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(doc.Records))
	}
	if doc.Records[0].Seq != 1 {
		t.Fatalf("seq = %d, want 1", doc.Records[0].Seq)
	}
	wantPath := filepath.Join(outDir, "1")
	if doc.Records[0].Path != wantPath {
		t.Fatalf("path = %q, want %q", doc.Records[0].Path, wantPath)
	}
}

func TestRunDumpSlowConnectionResumesInterruptedDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Leave slow-connection artifacts, no final metadata.json, and matching tmp provenance.
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ckpt.json"), []byte(`{"table":"users","pk_column":"id","last_pk":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ndjson.tmp"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
	"source_database": "db",
	"schemas": ["public"]
	,"source_signature": "postgres://@h:5432/db",
	"sanitization_enabled": false
  }
}`
	if err := os.WriteFile(filepath.Join(interruptedDir, "metadata.json.tmp"), []byte(tmpMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--slow-connection"}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	if capturedOutput != interruptedDir {
		t.Fatalf("output = %q, want %q", capturedOutput, interruptedDir)
	}
}

func TestRunDumpSlowConnectionDifferentSourceAllocatesFresh(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Interrupted dir has slow artifacts but provenance from a different source.
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ckpt.json"), []byte(`{"table":"users","pk_column":"id","last_pk":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ndjson.tmp"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_database": "otherdb",
    "schemas": ["billing"]
  }
}`
	if err := os.WriteFile(filepath.Join(interruptedDir, "metadata.json.tmp"), []byte(tmpMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--slow-connection"}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	want := filepath.Join(baseDir, "2")
	if capturedOutput != want {
		t.Fatalf("output = %q, want %q", capturedOutput, want)
	}
}

func TestRunDumpNormalAlwaysAllocatesNewDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ckpt.json"), []byte(`{"table":"users","pk_column":"id","last_pk":5}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	want := filepath.Join(baseDir, "2")
	if capturedOutput != want {
		t.Fatalf("output = %q, want %q", capturedOutput, want)
	}
}

func TestFindResumableSlowDumpDirSkipsNewerNonResumable(t *testing.T) {
	base := t.TempDir()

	older := filepath.Join(base, "1")
	newer := filepath.Join(base, "2")
	if err := os.MkdirAll(older, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatal(err)
	}

	// Older directory is an interrupted slow dump with matching provenance.
	if err := os.WriteFile(filepath.Join(older, "users.ckpt.json"), []byte(`{"table":"users","pk_column":"id","last_pk":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(older, "users.ndjson.tmp"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	olderMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_database": "db",
    "source_signature": "postgres://@h:5432/db",
    "schemas": ["app"],
    "sanitization_enabled": false
  }
}`
	if err := os.WriteFile(filepath.Join(older, "metadata.json.tmp"), []byte(olderMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	// Newer directory is a completed dump with metadata.
	if err := os.WriteFile(filepath.Join(newer, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, seq, ok := findResumableDumpDir(base, slowResumeExpect("postgres://@h:5432/db", []string{"app"}, false))
	if !ok {
		t.Fatal("expected resumable dir")
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1", seq)
	}
	if got != older {
		t.Fatalf("got %q, want %q", got, older)
	}
}

func TestFindResumableSlowDumpDirRequiresMatchingProvenance(t *testing.T) {
	tests := []struct {
		name            string
		sourceSignature string
		schemas         []string
		wantOk          bool
	}{
		{
			name:            "matching database and schemas",
			sourceSignature: "postgres://u@h:5432/db",
			schemas:         []string{"app", "billing"},
			wantOk:          true,
		},
		{
			name:            "different database",
			sourceSignature: "postgres://u@other:5432/db",
			schemas:         []string{"app", "billing"},
			wantOk:          false,
		},
		{
			name:            "different host",
			sourceSignature: "postgres://u@other-host:5432/db",
			schemas:         []string{"app", "billing"},
			wantOk:          false,
		},
		{
			name:            "different port",
			sourceSignature: "postgres://u@h:5433/db",
			schemas:         []string{"app", "billing"},
			wantOk:          false,
		},
		{
			name:            "different user",
			sourceSignature: "postgres://other@h:5432/db",
			schemas:         []string{"app", "billing"},
			wantOk:          false,
		},
		{
			name:            "different schemas",
			sourceSignature: "postgres://u@h:5432/db",
			schemas:         []string{"billing"},
			wantOk:          false,
		},
		{
			name:            "schema order ignored",
			sourceSignature: "postgres://u@h:5432/db",
			schemas:         []string{"billing", "app"},
			wantOk:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "1")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "users.ckpt.json"), []byte(`{"table":"users"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			meta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_database": "db",
    "source_signature": "postgres://u@h:5432/db",
    "schemas": ["app", "billing"],
    "sanitization_enabled": false
  }
}`
			if err := os.WriteFile(filepath.Join(dir, "metadata.json.tmp"), []byte(meta), 0o644); err != nil {
				t.Fatal(err)
			}

			_, _, ok := findResumableDumpDir(base, slowResumeExpect(tt.sourceSignature, tt.schemas, false))
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOk)
			}
		})
	}
}

func TestFindResumableSlowDumpDirRequiresMatchingSanitization(t *testing.T) {
	enabled, disabled := true, false
	base, dir := t.TempDir(), ""
	dir = filepath.Join(base, "1")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ckpt.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(sanitized *bool) {
		meta, err := json.Marshal(dump.Metadata{Schema: "public", Provenance: &dump.Provenance{SourceSignature: "postgres://u@h:5432/db", Sanitized: sanitized}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "metadata.json.tmp"), meta, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(&enabled)
	if _, _, ok := findResumableDumpDir(base, slowResumeExpect("postgres://u@h:5432/db", nil, true)); !ok {
		t.Fatal("matching sanitization did not resume")
	}
	if _, _, ok := findResumableDumpDir(base, slowResumeExpect("postgres://u@h:5432/db", nil, false)); ok {
		t.Fatal("enabled dump resumed with sanitization disabled")
	}
	write(&disabled)
	if _, _, ok := findResumableDumpDir(base, slowResumeExpect("postgres://u@h:5432/db", nil, true)); ok {
		t.Fatal("disabled dump resumed with sanitization enabled")
	}
	write(nil)
	if _, _, ok := findResumableDumpDir(base, slowResumeExpect("postgres://u@h:5432/db", nil, false)); ok {
		t.Fatal("legacy dump resumed without sanitization provenance")
	}
}

func TestBuildDumpOptionsChunkSizeResolution(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.SlowChunkSize = 250

	flags := dumpFlags{
		DSN:            "postgres://h-a/db_a",
		Output:         t.TempDir(),
		SlowConnection: true,
		ChunkSize:      100,
	}
	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectSlowChunkSizeEquals(100, opts...) {
		t.Fatalf("expected chunk size 100 from flag")
	}

	flags.ChunkSize = 0
	opts, err = buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectSlowChunkSizeEquals(250, opts...) {
		t.Fatalf("expected chunk size 250 from config")
	}

	cfg.Dump.SlowChunkSize = 0
	opts, err = buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectSlowChunkSizeEquals(dump.DefaultSlowChunkSize, opts...) {
		t.Fatalf("expected default chunk size %d", dump.DefaultSlowChunkSize)
	}
}

func TestBuildDumpOptionsRetryResolution(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.SlowRetryMax = 2
	cfg.Dump.SlowRetryBase = "2s"

	flags := dumpFlags{
		DSN:            "postgres://h-a/db_a",
		Output:         t.TempDir(),
		SlowConnection: true,
		RetryMax:       5,
		RetryBase:      "1s",
	}
	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	max, base := dump.InspectSlowRetry(opts...)
	if max != 5 || base != time.Second {
		t.Fatalf("retry = (%d, %v), want (5, 1s)", max, base)
	}

	flags.RetryMax = 0
	flags.RetryBase = ""
	opts, err = buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	max, base = dump.InspectSlowRetry(opts...)
	if max != 2 || base != 2*time.Second {
		t.Fatalf("retry = (%d, %v), want (2, 2s)", max, base)
	}
}

func TestBuildDumpOptionsInvalidSeedFilePropagatesError(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	if err := os.WriteFile(seedFile, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	flags := dumpFlags{
		DSN:      "postgres://h-a/db_a",
		Output:   t.TempDir(),
		SeedFile: seedFile,
	}

	_, err := buildDumpOptions(flags, config.DefaultConfig())
	if err == nil {
		t.Fatal("expected error for invalid seed file")
	}
}

func TestRunDumpInvalidOptionsLeaveOutputEmpty(t *testing.T) {
	out := t.TempDir()
	err := runDump([]string{"--dsn", "postgres://h/db", "--output", out, "--percent", "101"})
	if err == nil || !strings.Contains(err.Error(), "between 1 and 100") {
		t.Fatalf("err = %v, want percent validation error", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid dump allocated output: %v", entries)
	}
}

func TestRunDumpNegativePercentLeavesOutputEmpty(t *testing.T) {
	out := t.TempDir()
	err := runDump([]string{"--dsn", "postgres://h/db", "--output", out, "--percent", "-1"})
	if err == nil || !strings.Contains(err.Error(), "between 1 and 100") {
		t.Fatalf("err = %v, want percent validation error", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("negative percent allocated output: %v", entries)
	}
}

func TestBuildDumpOptionsNegativeConfigPercent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Subset.Percent = -1
	_, err := buildDumpOptions(dumpFlags{}, cfg)
	if err == nil || !strings.Contains(err.Error(), "between 1 and 100") {
		t.Fatalf("err = %v, want percent validation error", err)
	}
}

func TestParseDumpFlagsJSON(t *testing.T) {
	got, err := parseDumpFlags([]string{"--dsn", "postgres://h/db", "--output", t.TempDir(), "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.JSON {
		t.Fatal("expected JSON to be true")
	}
}

func TestRunDumpJSONOutput(t *testing.T) {
	oldPing := dumpPingContext
	oldRun := dumpRun
	oldLoad := dumpLoadConfig
	t.Cleanup(func() {
		dumpPingContext = oldPing
		dumpRun = oldRun
		dumpLoadConfig = oldLoad
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		return writeStubDumpMetadata(out)
	}

	outDir := t.TempDir()
	args := []string{"--dsn", "postgres://h/db", "--output", outDir, "--json"}

	stdout := captureStdout(func() {
		if err := runDump(args); err != nil {
			t.Fatalf("runDump: %v", err)
		}
	})

	var result struct {
		OK             bool     `json:"ok"`
		Command        string   `json:"command"`
		OutputDir      string   `json:"output_dir"`
		Seq            int      `json:"seq"`
		SourceDatabase string   `json:"source_database"`
		Schemas        []string `json:"schemas"`
		TableCount     int      `json:"table_count"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if !result.OK {
		t.Fatal("expected ok=true")
	}
	if result.Command != "dump" {
		t.Fatalf("command = %q, want dump", result.Command)
	}
	if result.SourceDatabase != "db" {
		t.Fatalf("source_database = %q, want db", result.SourceDatabase)
	}
	if result.TableCount != 0 {
		t.Fatalf("table_count = %d, want 0 (stub)", result.TableCount)
	}
}

func TestRunDumpJSONErrorWrap(t *testing.T) {
	oldPing := dumpPingContext
	oldLoad := dumpLoadConfig
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpPingContext = oldPing
		dumpLoadConfig = oldLoad
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(context.Context, *sql.DB, string, ...dump.Option) error {
		return errors.New("export failed")
	}

	args := []string{"dolly", "dump", "--dsn", "postgres://h/db", "--output", t.TempDir(), "--json"}
	stdout := captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch(args)
			if exit != 1 {
				t.Fatalf("dispatch exit = %d, want 1", exit)
			}
		})
		// stderr should contain JSON error, no human output
		var errObj struct {
			OK      bool   `json:"ok"`
			Command string `json:"command"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &errObj); err != nil {
			t.Fatalf("stderr not valid JSON: %v\n%s", err, stderr)
		}
		if errObj.OK {
			t.Fatal("expected ok=false in error JSON")
		}
		if errObj.Command != "dump" {
			t.Fatalf("command = %q, want dump", errObj.Command)
		}
		if !strings.Contains(errObj.Error, "export failed") {
			t.Fatalf("error = %q, want 'export failed'", errObj.Error)
		}
	})
	// stdout must be empty
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout should be empty on failure, got %q", stdout)
	}
}

func TestRunDumpJSONNilSchemas(t *testing.T) {
	oldPing := dumpPingContext
	oldRun := dumpRun
	oldLoad := dumpLoadConfig
	t.Cleanup(func() {
		dumpPingContext = oldPing
		dumpRun = oldRun
		dumpLoadConfig = oldLoad
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, out string, opts ...dump.Option) error {
		got := dump.InspectSchemas(opts...)
		if len(got) != 1 || got[0] != "public" {
			t.Fatalf("dump schemas = %v, want [public]", got)
		}
		return writeStubDumpMetadata(out)
	}

	outDir := t.TempDir()
	args := []string{"--dsn", "postgres://h/db", "--output", outDir, "--json"}

	stdout := captureStdout(func() {
		if err := runDump(args); err != nil {
			t.Fatalf("runDump: %v", err)
		}
	})

	var result struct {
		Schemas []string `json:"schemas"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(result.Schemas) != 1 || result.Schemas[0] != "public" {
		t.Fatalf("schemas = %v, want [public]", result.Schemas)
	}
}

func TestRunDumpJSONMutualExclusion(t *testing.T) {
	// --json --dsn X --connection Y must fail with JSON error envelope.
	_ = captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch([]string{"dolly", "dump", "--json", "--dsn", "X", "--connection", "Y"})
			if exit != 1 {
				t.Fatalf("dispatch exit = %d, want 1", exit)
			}
		})
		var errObj struct {
			OK      bool   `json:"ok"`
			Command string `json:"command"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &errObj); err != nil {
			t.Fatalf("stderr not valid JSON: %v\n%s", err, stderr)
		}
		if errObj.OK {
			t.Fatal("expected ok=false in error JSON")
		}
		if errObj.Command != "dump" {
			t.Fatalf("command = %q, want dump", errObj.Command)
		}
		if !strings.Contains(errObj.Error, "mutually exclusive") {
			t.Fatalf("error = %q, want containing 'mutually exclusive'", errObj.Error)
		}
	})
}

func TestRunDumpJSONMissingConnection(t *testing.T) {
	// dump --json with no DSN/connection must produce JSON error envelope
	// when env resolution also fails.
	oldLoad := dumpLoadConfig
	oldResolve := resolveLoadDotEnv
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		resolveLoadDotEnv = oldResolve
	})

	dumpLoadConfig = func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Dump.OutputDir = t.TempDir()
		return cfg, nil
	}
	resolveLoadDotEnv = func(string, config.EnvVarNames) (string, error) {
		return "", config.ErrSourceDSNNotFound
	}

	_ = captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch([]string{"dolly", "dump", "--json"})
			if exit != 1 {
				t.Fatalf("dispatch exit = %d, want 1", exit)
			}
		})
		var errObj struct {
			OK      bool   `json:"ok"`
			Command string `json:"command"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &errObj); err != nil {
			t.Fatalf("stderr not valid JSON: %v\n%s", err, stderr)
		}
		if errObj.OK {
			t.Fatal("expected ok=false in error JSON")
		}
		if errObj.Command != "dump" {
			t.Fatalf("command = %q, want dump", errObj.Command)
		}
		if !strings.Contains(errObj.Error, "no connection") && !strings.Contains(errObj.Error, "--dsn") {
			t.Fatalf("error = %q, want containing 'no connection' or '--dsn'", errObj.Error)
		}
	})
}

func TestBuildDumpOptionsChunkSizeCap(t *testing.T) {
	cfg := config.DefaultConfig()

	flags := dumpFlags{
		DSN:            "postgres://h-a/db_a",
		Output:         t.TempDir(),
		SlowConnection: true,
		ChunkSize:      999999999,
	}
	stderr := captureStderr(func() {
		opts, err := buildDumpOptions(flags, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !dump.InspectSlowChunkSizeEquals(1000000, opts...) {
			t.Fatal("expected chunk size capped at 1000000")
		}
	})
	if !strings.Contains(stderr, "capping to 1000000") {
		t.Fatalf("stderr missing cap warning: %q", stderr)
	}
}

func TestParseDumpFlagsPercent(t *testing.T) {
	got, err := parseDumpFlags([]string{"--dsn", "postgres://h/db", "--output", t.TempDir(), "--percent", "10"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Percent != 10 {
		t.Fatalf("percent = %d, want 10", got.Percent)
	}
}

func TestBuildDumpOptionsPercentMode(t *testing.T) {
	flags := dumpFlags{
		DSN:     "postgres://h-a/db_a",
		Output:  t.TempDir(),
		Percent: 10,
	}

	opts, err := buildDumpOptions(flags, config.DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset == nil {
		t.Fatal("expected subset config in opts")
	}
	if subset.Percent != 10 {
		t.Fatalf("expected Percent 10, got %d", subset.Percent)
	}
}

func TestBuildDumpOptionsPercentSeedFileConflict(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	seedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`
	if err := os.WriteFile(seedFile, []byte(seedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	flags := dumpFlags{
		DSN:      "postgres://h-a/db_a",
		Output:   t.TempDir(),
		SeedFile: seedFile,
		Percent:  10,
	}

	_, err := buildDumpOptions(flags, config.DefaultConfig())
	if err == nil {
		t.Fatal("expected error for --percent + --seed-file conflict")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %q, want 'mutually exclusive'", err.Error())
	}
}

func TestBuildDumpOptionsPercentConfigDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Subset.Percent = 25

	flags := dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
		// --percent not set, falls back to config
	}

	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset == nil {
		t.Fatal("expected subset config from config default")
	}
	if subset.Percent != 25 {
		t.Fatalf("expected Percent 25 from config default, got %d", subset.Percent)
	}
}

func TestBuildDumpOptionsPercentWithLimits(t *testing.T) {
	flags := dumpFlags{
		DSN:           "postgres://h-a/db_a",
		Output:        t.TempDir(),
		Percent:       10,
		MaxDepth:      3,
		MaxTables:     5,
		MaxRows:       100,
		MaxInListSize: 50,
	}

	opts, err := buildDumpOptions(flags, config.DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset == nil {
		t.Fatal("expected subset config")
	}
	if subset.Limits.MaxDepth != 3 {
		t.Fatalf("expected max_depth 3, got %d", subset.Limits.MaxDepth)
	}
	if subset.Limits.MaxTables != 5 {
		t.Fatalf("expected max_tables 5, got %d", subset.Limits.MaxTables)
	}
	if subset.Limits.MaxRows != 100 {
		t.Fatalf("expected max_rows 100, got %d", subset.Limits.MaxRows)
	}
	if subset.Limits.MaxInListSize != 50 {
		t.Fatalf("expected max_in_list_size 50, got %d", subset.Limits.MaxInListSize)
	}
}

func TestBuildDumpOptionsPercentSlowConnectionConflict(t *testing.T) {
	flags := dumpFlags{
		DSN:            "postgres://h-a/db_a",
		Output:         t.TempDir(),
		Percent:        10,
		SlowConnection: true,
	}

	_, err := buildDumpOptions(flags, config.DefaultConfig())
	if err == nil {
		t.Fatal("expected error for --percent + --slow-connection")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("error = %q, want 'incompatible'", err.Error())
	}
}

func TestBuildDumpOptionsConfigSeedFile(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	seedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`
	if err := os.WriteFile(seedFile, []byte(seedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Subset.SeedFile = seedFile

	flags := dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
		// --seed-file NOT set; config subset.seed_file should be used.
	}

	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset == nil {
		t.Fatal("expected subset config from config seed_file")
	}
	if len(subset.Seeds) != 1 {
		t.Fatalf("expected 1 seed, got %d", len(subset.Seeds))
	}
	if subset.Seeds[0].Table != "tbl_a" {
		t.Fatalf("expected table tbl_a, got %q", subset.Seeds[0].Table)
	}
}

func TestBuildDumpOptionsConfigSeedFileAndPercentConflict(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	seedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`
	if err := os.WriteFile(seedFile, []byte(seedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Subset.SeedFile = seedFile
	cfg.Subset.Percent = 50

	flags := dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
	}

	_, err := buildDumpOptions(flags, cfg)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestBuildDumpOptionsCLISeedFileOverridesConfig(t *testing.T) {
	cliSeedFile := filepath.Join(t.TempDir(), "cli-seeds.json")
	cliSeedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":99}]}`
	if err := os.WriteFile(cliSeedFile, []byte(cliSeedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgSeedFile := filepath.Join(t.TempDir(), "cfg-seeds.json")
	cfgSeedJSON := `{"seeds":[{"table":"departments","column":"id","op":"eq","value":1}]}`
	if err := os.WriteFile(cfgSeedFile, []byte(cfgSeedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Subset.SeedFile = cfgSeedFile

	flags := dumpFlags{
		DSN:      "postgres://h-a/db_a",
		Output:   t.TempDir(),
		SeedFile: cliSeedFile, // CLI overrides config.
	}

	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subset := dump.InspectOptions(opts...)
	if subset == nil {
		t.Fatal("expected subset config")
	}
	if subset.Seeds[0].Table != "tbl_a" {
		t.Fatalf("expected CLI seed (tbl_a), got %q", subset.Seeds[0].Table)
	}
	if v, ok := subset.Seeds[0].Value.(float64); !ok || int(v) != 99 {
		t.Fatalf("expected CLI seed value 99, got %v (type %T)", subset.Seeds[0].Value, subset.Seeds[0].Value)
	}
}

func TestBuildDumpOptionsConfigSeedFileSlowConnectionConflict(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	seedJSON := `{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`
	if err := os.WriteFile(seedFile, []byte(seedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Subset.SeedFile = seedFile

	flags := dumpFlags{
		DSN:            "postgres://h-a/db_a",
		Output:         t.TempDir(),
		SlowConnection: true,
	}

	_, err := buildDumpOptions(flags, cfg)
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("expected incompat error for slow-connection + config seed_file, got %v", err)
	}
}

func TestParseDumpFlagsTableSelection(t *testing.T) {
	got, err := parseDumpFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--output", "/tmp/out",
		"--include-table", "public.users",
		"--include-table", "public.orders",
		"--exclude-table", "public.audit_log",
		"--include-table-file", "/tmp/include.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.IncludeTables) != 2 || got.IncludeTables[0] != "public.users" {
		t.Fatalf("include tables = %v", got.IncludeTables)
	}
	if len(got.ExcludeTables) != 1 || got.ExcludeTables[0] != "public.audit_log" {
		t.Fatalf("exclude tables = %v", got.ExcludeTables)
	}
	if len(got.IncludeTableFiles) != 1 {
		t.Fatalf("include files = %v", got.IncludeTableFiles)
	}
}

func TestBuildDumpOptionsTableSelectionCLIReplacesConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.IncludeTables = []string{"public.config_only"}
	cfg.Dump.ExcludeTables = []string{"public.config_exclude"}

	flags := dumpFlags{
		DSN:           "postgres://h-a/db_a",
		Output:        t.TempDir(),
		IncludeTables: []string{"public.cli_only"},
	}

	opts, err := buildDumpOptions(flags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	policy := dump.InspectTableSelection(opts...)
	if policy == nil || len(policy.Includes) != 1 {
		t.Fatalf("policy = %+v", policy)
	}
	if policy.Includes[0].Table != (dump.QualifiedTable{Schema: "public", Name: "cli_only"}) {
		t.Fatalf("include = %+v", policy.Includes[0].Table)
	}
	if len(policy.Excludes) != 1 || policy.Excludes[0].Table.Name != "config_exclude" {
		t.Fatalf("expected config excludes to remain when CLI only replaces include category, got %+v", policy.Excludes)
	}
}

func TestBuildDumpOptionsTableSelectionConfigDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.IncludeTables = []string{"public.users"}
	cfg.Dump.ExcludeTables = []string{"public.audit_log"}

	opts, err := buildDumpOptions(dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	policy := dump.InspectTableSelection(opts...)
	if policy == nil || len(policy.Includes) != 1 || len(policy.Excludes) != 1 {
		t.Fatalf("policy = %+v", policy)
	}
	if policy.Includes[0].Source.Name != "dump.include_tables" {
		t.Fatalf("include source = %+v", policy.Includes[0].Source)
	}
}

func TestBuildDumpOptionsTableSelectionInvalidSelector(t *testing.T) {
	_, err := buildDumpOptions(dumpFlags{
		DSN:           "postgres://h-a/db_a",
		Output:        t.TempDir(),
		IncludeTables: []string{"users"},
	}, config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "unqualified") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildDumpOptionsTableSelectionCombinesDirectAndFile(t *testing.T) {
	includeFile := filepath.Join(t.TempDir(), "include.txt")
	if err := os.WriteFile(includeFile, []byte("public.orders\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts, err := buildDumpOptions(dumpFlags{
		DSN:               "postgres://h-a/db_a",
		Output:            t.TempDir(),
		IncludeTables:     []string{"public.users"},
		IncludeTableFiles: []string{includeFile},
	}, config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	policy := dump.InspectTableSelection(opts...)
	if policy == nil || len(policy.Includes) != 2 {
		t.Fatalf("policy = %+v", policy)
	}
}

func TestRemoveFreshEmptyDumpDir(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeFreshEmptyDumpDir(empty); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Fatal("expected empty dir removed")
	}

	nonEmpty := filepath.Join(dir, "nonempty")
	if err := os.Mkdir(nonEmpty, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "metadata.json.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeFreshEmptyDumpDir(nonEmpty); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(nonEmpty); err != nil {
		t.Fatalf("non-empty dir should remain: %v", err)
	}
}

func TestRunDumpSelectionErrorRemovesFreshEmptyDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, _ string, _ ...dump.Option) error {
		return fmt.Errorf("%w: include table %q not found in database", dump.ErrTableSelection, "public.missing")
	}

	baseDir := t.TempDir()
	err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--include-table", "public.missing"})
	if err == nil {
		t.Fatal("expected selection error")
	}
	if !dump.IsTableSelectionError(err) {
		t.Fatalf("error = %v, want table selection", err)
	}
	if _, statErr := os.Stat(filepath.Join(baseDir, "1")); !os.IsNotExist(statErr) {
		t.Fatalf("fresh empty dump dir should be removed, stat err = %v", statErr)
	}
}

func TestRunDumpNoTablesErrorRemovesFreshEmptyDir(t *testing.T) {
	stubDumpListSchemaNames(t, []string{"empty_schema"}, nil)

	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, _ string, _ ...dump.Option) error {
		return &dump.NoTablesError{Schemas: []string{"empty_schema"}}
	}

	baseDir := t.TempDir()
	err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--schemas", "empty_schema"})
	if err == nil {
		t.Fatal("expected no-tables error")
	}
	if !dump.IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if _, statErr := os.Stat(filepath.Join(baseDir, "1")); !os.IsNotExist(statErr) {
		t.Fatalf("fresh empty dump dir should be removed, stat err = %v", statErr)
	}
}

func TestRunDumpNonSelectionErrorPreservesFreshDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, _ string, _ ...dump.Option) error {
		return errors.New("stream table failed")
	}

	baseDir := t.TempDir()
	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir}); err == nil {
		t.Fatal("expected dump error")
	}
	if _, statErr := os.Stat(filepath.Join(baseDir, "1")); statErr != nil {
		t.Fatalf("fresh dir should remain on non-selection error: %v", statErr)
	}
}

func TestRunDumpSelectionErrorPreservesNonEmptyDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		if err := os.WriteFile(filepath.Join(out, "users.ndjson.tmp"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return fmt.Errorf("%w: include table %q not found in database", dump.ErrTableSelection, "public.missing")
	}

	baseDir := t.TempDir()
	err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--include-table", "public.missing"})
	if err == nil {
		t.Fatal("expected selection error")
	}
	dir := filepath.Join(baseDir, "1")
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("non-empty dir should remain: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "users.ndjson.tmp")); statErr != nil {
		t.Fatalf("partial artifact should remain: %v", statErr)
	}
}

func TestRunDumpResumedSlowDirNotRemovedOnSelectionError(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, _ string, _ ...dump.Option) error {
		return fmt.Errorf("%w: table selection matched no tables", dump.ErrTableSelection)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ckpt.json"), []byte(`{"table":"users"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_database": "db",
    "schemas": ["public"],
    "source_signature": "postgres://@h:5432/db",
    "sanitization_enabled": false
  }
}`
	if err := os.WriteFile(filepath.Join(interruptedDir, "metadata.json.tmp"), []byte(tmpMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--slow-connection", "--include-table", "public.missing"})
	if err == nil {
		t.Fatal("expected selection error")
	}
	if _, statErr := os.Stat(interruptedDir); statErr != nil {
		t.Fatalf("resumable slow dir should remain: %v", statErr)
	}
}

func TestParseDumpFlagsChunkTables(t *testing.T) {
	got, err := parseDumpFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--output", "/tmp/out",
		"--chunk-table", "public.orders",
		"--chunk-table-file", "/tmp/chunk.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ChunkTables) != 1 || got.ChunkTables[0] != "public.orders" {
		t.Fatalf("chunk tables = %v", got.ChunkTables)
	}
	if len(got.ChunkTableFiles) != 1 {
		t.Fatalf("chunk files = %v", got.ChunkTableFiles)
	}
}

func TestBuildDumpOptionsChunkCLIReplacesConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.ChunkTables = []string{"public.config_only"}

	opts, err := buildDumpOptions(dumpFlags{
		DSN:         "postgres://h-a/db_a",
		Output:      t.TempDir(),
		ChunkTables: []string{"public.cli_only"},
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	policy := dump.InspectChunkPolicy(opts...)
	if policy == nil || len(policy.Requests) != 1 {
		t.Fatalf("policy = %+v", policy)
	}
	if policy.Requests[0].Table.Name != "cli_only" {
		t.Fatalf("chunk table = %+v", policy.Requests[0].Table)
	}
}

func TestBuildDumpOptionsChunkInvalidSelector(t *testing.T) {
	_, err := buildDumpOptions(dumpFlags{
		DSN:         "postgres://h-a/db_a",
		Output:      t.TempDir(),
		ChunkTables: []string{"orders"},
	}, config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "unqualified") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildDumpOptionsChunkSubsetConflict(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "seeds.json")
	if err := os.WriteFile(seedFile, []byte(`{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := buildDumpOptions(dumpFlags{
		DSN:         "postgres://h-a/db_a",
		Output:      t.TempDir(),
		SeedFile:    seedFile,
		ChunkTables: []string{"public.users"},
	}, config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunDumpChunkPolicyErrorRemovesFreshEmptyDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(_ context.Context, _ *sql.DB, _ string, _ ...dump.Option) error {
		return fmt.Errorf("%w: chunk table %q not found in selected tables", dump.ErrChunkPolicy, "public.missing")
	}

	baseDir := t.TempDir()
	err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--chunk-table", "public.missing"})
	if err == nil {
		t.Fatal("expected chunk policy error")
	}
	if !dump.IsChunkPolicyError(err) {
		t.Fatalf("error = %v, want chunk policy", err)
	}
	if _, statErr := os.Stat(filepath.Join(baseDir, "1")); !os.IsNotExist(statErr) {
		t.Fatalf("fresh empty dump dir should be removed, stat err = %v", statErr)
	}
}

func TestBuildDumpOptionsChunkForcesNoTransaction(t *testing.T) {
	opts, err := buildDumpOptions(dumpFlags{
		DSN:         "postgres://h-a/db_a",
		Output:      t.TempDir(),
		ChunkTables: []string{"public.users"},
	}, config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectWithoutTransaction(opts...) {
		t.Fatal("chunk policy should force without-transaction mode")
	}
}

func TestRunDumpChunkTableResumesInterruptedDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ckpt.json"), []byte(`{"table":"users","pk_column":"id","last_pk":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ndjson.tmp"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_database": "db",
    "source_signature": "postgres://@h:5432/db",
    "schemas": ["public"],
    "sanitization_enabled": false,
    "chunk_tables": {
      "requested": [{"normalized": "public.users", "source": "flag: --chunk-table"}]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(interruptedDir, "metadata.json.tmp"), []byte(tmpMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir, "--chunk-table", "public.users"}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	if capturedOutput != interruptedDir {
		t.Fatalf("output = %q, want %q", capturedOutput, interruptedDir)
	}
}

func TestRunDumpChunkConfigResumesInterruptedDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	cfg := config.DefaultConfig()
	cfg.Dump.ChunkTables = []string{"public.orders"}
	dumpLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "orders.ckpt.json"), []byte(`{"table":"orders","pk_column":"id","last_pk":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "orders.ndjson.tmp"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_database": "db",
    "source_signature": "postgres://@h:5432/db",
    "schemas": ["public"],
    "sanitization_enabled": false,
    "chunk_tables": {
      "requested": [{"normalized": "public.orders", "source": "config: dump.chunk_tables"}]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(interruptedDir, "metadata.json.tmp"), []byte(tmpMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	if capturedOutput != interruptedDir {
		t.Fatalf("output = %q, want %q", capturedOutput, interruptedDir)
	}
}

func TestFindResumableDumpDirRejectsMismatchedChunkPolicy(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ckpt.json"), []byte(`{"table":"users"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_signature": "postgres://u@h:5432/db",
    "sanitization_enabled": false,
    "chunk_tables": {
      "requested": [{"normalized": "public.users", "source": "flag: --chunk-table"}]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json.tmp"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	exp := resumableDumpExpectation{
		sourceSignature:     "postgres://u@h:5432/db",
		sanitizationEnabled: false,
		chunkFingerprint: &dump.ChunkTableProvenance{
			Requested: []dump.SelectorRecord{{Normalized: "public.orders", Source: "flag: --chunk-table"}},
		},
	}
	if _, _, ok := findResumableDumpDir(base, exp); ok {
		t.Fatal("mismatched chunk policy should not resume")
	}
}

func TestRunDumpDefaultDoesNotResumeChunkInterruptedDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ckpt.json"), []byte(`{"table":"users"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_signature": "postgres://@h:5432/db",
    "sanitization_enabled": false,
    "chunk_tables": {
      "requested": [{"normalized": "public.users", "source": "flag: --chunk-table"}]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(interruptedDir, "metadata.json.tmp"), []byte(tmpMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDump([]string{"--dsn", "postgres://h/db", "--output", baseDir}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	if capturedOutput == interruptedDir {
		t.Fatalf("default dump should allocate fresh dir, got resumed %q", capturedOutput)
	}
}

func TestBuildDumpOptionsConfigChunkSubsetConflict(t *testing.T) {
	t.Run("config chunk + config percent", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Dump.ChunkTables = []string{"public.users"}
		cfg.Subset.Percent = 50
		_, err := buildDumpOptions(dumpFlags{
			DSN:    "postgres://h-a/db_a",
			Output: t.TempDir(),
		}, cfg)
		if err == nil || !strings.Contains(err.Error(), "incompatible") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("config chunk + CLI seed-file", func(t *testing.T) {
		seedFile := filepath.Join(t.TempDir(), "seeds.json")
		if err := os.WriteFile(seedFile, []byte(`{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.DefaultConfig()
		cfg.Dump.ChunkTables = []string{"public.users"}
		_, err := buildDumpOptions(dumpFlags{
			DSN:      "postgres://h-a/db_a",
			Output:   t.TempDir(),
			SeedFile: seedFile,
		}, cfg)
		if err == nil || !strings.Contains(err.Error(), "incompatible") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("config chunk + config seed-file", func(t *testing.T) {
		seedFile := filepath.Join(t.TempDir(), "seeds.json")
		if err := os.WriteFile(seedFile, []byte(`{"seeds":[{"table":"tbl_a","column":"id","op":"eq","value":1}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.DefaultConfig()
		cfg.Dump.ChunkTables = []string{"public.users"}
		cfg.Subset.SeedFile = seedFile
		_, err := buildDumpOptions(dumpFlags{
			DSN:    "postgres://h-a/db_a",
			Output: t.TempDir(),
		}, cfg)
		if err == nil || !strings.Contains(err.Error(), "incompatible") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestProvenanceMatchesResumableChunkFingerprint(t *testing.T) {
	usersChunk := &dump.ChunkTableProvenance{
		Requested: []dump.SelectorRecord{{Normalized: "public.users", Source: "flag: --chunk-table"}},
	}
	ordersChunk := &dump.ChunkTableProvenance{
		Requested: []dump.SelectorRecord{{Normalized: "public.orders", Source: "flag: --chunk-table"}},
	}

	tests := []struct {
		name   string
		meta   *dump.Provenance
		exp    resumableDumpExpectation
		wantOK bool
	}{
		{
			name:   "slow without explicit chunk matches nil metadata chunk provenance",
			meta:   &dump.Provenance{ChunkTables: nil},
			exp:    slowResumeExpect("postgres://@h:5432/db", nil, false),
			wantOK: true,
		},
		{
			name: "slow+chunk matches same chunk policy",
			meta: &dump.Provenance{ChunkTables: usersChunk},
			exp: resumableDumpExpectation{
				sourceSignature:     "postgres://@h:5432/db",
				sanitizationEnabled: false,
				chunkFingerprint:    usersChunk,
			},
			wantOK: true,
		},
		{
			name: "changed chunk policy refuses unsafe reuse",
			meta: &dump.Provenance{ChunkTables: usersChunk},
			exp: resumableDumpExpectation{
				sourceSignature:     "postgres://@h:5432/db",
				sanitizationEnabled: false,
				chunkFingerprint:    ordersChunk,
			},
			wantOK: false,
		},
		{
			name:   "removed chunk policy refuses metadata with chunk provenance",
			meta:   &dump.Provenance{ChunkTables: usersChunk},
			exp:    slowResumeExpect("postgres://@h:5432/db", nil, false),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provenanceMatchesResumable(tt.meta, tt.exp)
			if got != tt.wantOK {
				t.Fatalf("provenanceMatchesResumable() = %v, want %v", got, tt.wantOK)
			}
		})
	}
}

func TestRunDumpSlowConnectionWithChunkTableResumesInterruptedDir(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	dumpLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }

	var capturedOutput string
	dumpRun = func(_ context.Context, _ *sql.DB, out string, _ ...dump.Option) error {
		capturedOutput = out
		return writeStubDumpMetadata(out)
	}

	baseDir := t.TempDir()
	interruptedDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(interruptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ckpt.json"), []byte(`{"table":"users","pk_column":"id","last_pk":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interruptedDir, "users.ndjson.tmp"), []byte(`{"id":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [],
  "provenance": {
    "source_database": "db",
    "source_signature": "postgres://@h:5432/db",
    "schemas": ["public"],
    "sanitization_enabled": false,
    "chunk_tables": {
      "requested": [{"normalized": "public.users", "source": "flag: --chunk-table"}]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(interruptedDir, "metadata.json.tmp"), []byte(tmpMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDump([]string{
		"--dsn", "postgres://h/db",
		"--output", baseDir,
		"--slow-connection",
		"--chunk-table", "public.users",
	}); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	if capturedOutput != interruptedDir {
		t.Fatalf("output = %q, want %q", capturedOutput, interruptedDir)
	}
}

func TestParseDumpFlagsWorkers(t *testing.T) {
	got, err := parseDumpFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--output", "/tmp/out",
		"--workers", "4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Workers != 4 {
		t.Fatalf("Workers = %d, want 4", got.Workers)
	}
	if !got.WorkersSet {
		t.Fatal("WorkersSet = false, want true")
	}
}

func TestParseDumpFlagsWorkersExplicitZeroReject(t *testing.T) {
	flags, err := parseDumpFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--output", "/tmp/out",
		"--workers", "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.WorkersSet {
		t.Fatal("WorkersSet = false, want true")
	}
	_, err = buildDumpOptions(flags, config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "--workers must be between 1 and 16") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseDumpFlagsWorkersExplicitNegativeReject(t *testing.T) {
	flags, err := parseDumpFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--output", "/tmp/out",
		"--workers", "-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.WorkersSet {
		t.Fatal("WorkersSet = false, want true")
	}
	_, err = buildDumpOptions(flags, config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "--workers must be between 1 and 16") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildDumpOptionsWorkersAbsentUsesConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.Workers = 5

	opts, err := buildDumpOptions(dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := dump.InspectWorkers(opts...); got != 5 {
		t.Fatalf("workers = %d, want 5 from config when flag absent", got)
	}
}

func TestBuildDumpOptionsWorkersResolution(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.Workers = 2

	opts, err := buildDumpOptions(dumpFlags{
		DSN:        "postgres://h-a/db_a",
		Output:     t.TempDir(),
		Workers:    4,
		WorkersSet: true,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := dump.InspectWorkers(opts...); got != 4 {
		t.Fatalf("workers = %d, want 4 from CLI override", got)
	}
}

func TestBuildDumpOptionsWorkersConfigDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.Workers = 3

	opts, err := buildDumpOptions(dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := dump.InspectWorkers(opts...); got != 3 {
		t.Fatalf("workers = %d, want 3 from config", got)
	}
}

func TestBuildDumpOptionsWorkersSerialDefault(t *testing.T) {
	opts, err := buildDumpOptions(dumpFlags{
		DSN:    "postgres://h-a/db_a",
		Output: t.TempDir(),
	}, config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got := dump.InspectWorkers(opts...); got != 1 {
		t.Fatalf("workers = %d, want default 1", got)
	}
}

func TestBuildDumpOptionsWorkersMaxReject(t *testing.T) {
	_, err := buildDumpOptions(dumpFlags{
		DSN:        "postgres://h-a/db_a",
		Output:     t.TempDir(),
		Workers:    17,
		WorkersSet: true,
	}, config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "16") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildDumpOptionsWorkersConflicts(t *testing.T) {
	tests := []struct {
		name  string
		flags dumpFlags
		cfg   func() *config.Config
		want  string
	}{
		{
			name: "slow connection",
			flags: dumpFlags{
				DSN:            "postgres://h-a/db_a",
				Output:         t.TempDir(),
				Workers:        2,
				WorkersSet:     true,
				SlowConnection: true,
			},
			cfg:  config.DefaultConfig,
			want: "slow-connection",
		},
		{
			name: "chunk flag",
			flags: dumpFlags{
				DSN:         "postgres://h-a/db_a",
				Output:      t.TempDir(),
				Workers:     2,
				WorkersSet:  true,
				ChunkTables: []string{"public.users"},
			},
			cfg:  config.DefaultConfig,
			want: "chunk",
		},
		{
			name: "config chunk",
			flags: dumpFlags{
				DSN:        "postgres://h-a/db_a",
				Output:     t.TempDir(),
				Workers:    2,
				WorkersSet: true,
			},
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Dump.ChunkTables = []string{"public.users"}
				return cfg
			},
			want: "chunk",
		},
		{
			name: "subset seed",
			flags: dumpFlags{
				DSN:        "postgres://h-a/db_a",
				Output:     t.TempDir(),
				Workers:    2,
				WorkersSet: true,
				SeedFile:   "seeds.json",
			},
			cfg:  config.DefaultConfig,
			want: "seed-file",
		},
		{
			name: "subset percent",
			flags: dumpFlags{
				DSN:        "postgres://h-a/db_a",
				Output:     t.TempDir(),
				Workers:    2,
				WorkersSet: true,
				Percent:    50,
			},
			cfg:  config.DefaultConfig,
			want: "percent",
		},
		{
			name: "no transaction",
			flags: dumpFlags{
				DSN:           "postgres://h-a/db_a",
				Output:        t.TempDir(),
				Workers:       2,
				WorkersSet:    true,
				NoTransaction: true,
			},
			cfg:  config.DefaultConfig,
			want: "no-transaction",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildDumpOptions(tt.flags, tt.cfg())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestRunDumpWorkersInsufficientPoolBeforeAllocation(t *testing.T) {
	oldLoad := dumpLoadConfig
	oldPing := dumpPingContext
	oldRun := dumpRun
	t.Cleanup(func() {
		dumpLoadConfig = oldLoad
		dumpPingContext = oldPing
		dumpRun = oldRun
	})

	ran := false
	dumpPingContext = func(*sql.DB, context.Context) error { return nil }
	dumpRun = func(context.Context, *sql.DB, string, ...dump.Option) error {
		ran = true
		return nil
	}
	dumpLoadConfig = func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.DB.MaxOpenConns = 2
		return cfg, nil
	}

	err := runDump([]string{
		"--dsn", "postgres://h/db",
		"--output", t.TempDir(),
		"--workers", "3",
	})
	if err == nil || !strings.Contains(err.Error(), "max_open_conns") {
		t.Fatalf("error = %v", err)
	}
	if ran {
		t.Fatal("dump should not run when pool headroom fails before allocation")
	}
}
