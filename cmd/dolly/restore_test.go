package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestParseRestoreFlags(t *testing.T) {
	got, err := parseRestoreFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--input", "/tmp/in",
		"--on-conflict", "skip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DSN != "postgres://h-a/db_a" || got.Input != "/tmp/in" || got.OnConflict != "skip" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseRestoreFlagsReplaceConflict(t *testing.T) {
	_, err := parseRestoreFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--input", "/tmp/in",
		"--replace",
		"--on-conflict", "upsert",
	})
	if err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRestoreFlagsMissingDSN(t *testing.T) {
	// both-empty is now valid at parse time; .env fallback happens at run time
	got, err := parseRestoreFlags([]string{"--input", "/tmp/in"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DSN != "" || got.Connection != "" {
		t.Fatalf("expected empty DSN/Connection, got %+v", got)
	}
}

func TestParseRestoreFlagsConnection(t *testing.T) {
	got, err := parseRestoreFlags([]string{"--connection", "prod", "--input", "/tmp/in"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Connection != "prod" || got.DSN != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseRestoreFlagsConnectionDSNConflict(t *testing.T) {
	_, err := parseRestoreFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--connection", "prod",
		"--input", "/tmp/in",
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRestoreFlagsNoTransactionRequiresYes(t *testing.T) {
	_, err := parseRestoreFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--input", "/tmp/in",
		"--no-transaction",
	})
	if err == nil || !strings.Contains(err.Error(), "--yes to confirm") {
		t.Fatalf("err = %v", err)
	}

	_, err = parseRestoreFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--input", "/tmp/in",
		"--no-transaction",
		"--json",
	})
	if err == nil || !strings.Contains(err.Error(), "--yes to confirm") {
		t.Fatalf("json err = %v", err)
	}

	got, err := parseRestoreFlags([]string{
		"--dsn", "postgres://h-a/db_a",
		"--input", "/tmp/in",
		"--no-transaction",
		"--yes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.NoTransaction || !got.Yes {
		t.Fatalf("got %+v", got)
	}
}

func TestRunRestoreConnectionDisabled(t *testing.T) {
	oldLoad := restoreLoadConfig
	restoreLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { restoreLoadConfig = oldLoad })

	err := runRestore([]string{"--connection", "prod", "--input", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "save_connections is disabled") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRestoreFlagsHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out := captureStderr(func() {
				_, err := parseRestoreFlags(args)
				if !errors.Is(err, errHelp) {
					t.Fatalf("err = %v, want errHelp", err)
				}
			})
			for _, sub := range []string{"--dsn", "--connection", "--input", "--on-conflict"} {
				if !strings.Contains(out, sub) {
					t.Fatalf("restore usage missing %q:\n%s", sub, out)
				}
			}
			if strings.Contains(out, "required flag") {
				t.Fatalf("help should not require flags:\n%s", out)
			}
		})
	}
}

func TestRunRestoreConnectionPassesSchemasToRestore(t *testing.T) {
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

	oldLoad := restoreLoadConfig
	restoreLoadConfig = func(path string) (*config.Config, error) {
		c := config.DefaultConfig()
		c.SaveConnections = true
		return c, nil
	}
	t.Cleanup(func() { restoreLoadConfig = oldLoad })

	var captured []restore.Option
	oldRestore := restoreRestore
	restoreRestore = func(_ context.Context, _ *sql.DB, _ string, opts ...restore.Option) error {
		captured = opts
		return errors.New("stop after capture")
	}
	t.Cleanup(func() { restoreRestore = oldRestore })

	oldPing := restorePingContext
	restorePingContext = func(*sql.DB, context.Context) error { return nil }
	t.Cleanup(func() { restorePingContext = oldPing })

	err = runRestore([]string{"--connection", "prod", "--input", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "stop after capture") {
		t.Fatalf("runRestore err = %v", err)
	}
	got := restore.InspectSchemas(captured...)
	if len(got) != 1 || got[0] != "app" {
		t.Fatalf("schemas = %v, want [app]", got)
	}
}

func TestRunRestoreHelpNoDB(t *testing.T) {
	err := runRestore([]string{"-h"})
	if err != nil {
		t.Fatalf("runRestore help: %v", err)
	}
}

func TestRunRestoreEnvFallbackWithoutDSNFlag(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := restoreLoadConfig
	origPing := restorePingContext
	origRestore := restoreRestore
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		restoreLoadConfig = origLoad
		restorePingContext = origPing
		restoreRestore = origRestore
	})

	envCalled := false
	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		envCalled = true
		return "postgres://envuser:envpass@envhost/envdb", nil
	}
	restoreLoadConfig = func(string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	restorePingContext = func(*sql.DB, context.Context) error { return nil }
	restoreRestore = func(context.Context, *sql.DB, string, ...restore.Option) error { return nil }

	if err := runRestore([]string{"--input", t.TempDir()}); err != nil {
		t.Fatalf("runRestore: %v", err)
	}
	if !envCalled {
		t.Fatal("expected .env resolution when --dsn and --connection are omitted")
	}
}

func TestRunRestoreNoEnvNoFlagsReturnsError(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := restoreLoadConfig
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		restoreLoadConfig = origLoad
	})

	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		return "", config.ErrSourceDSNNotFound
	}
	restoreLoadConfig = func(string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}

	err := runRestore([]string{"--input", t.TempDir()})
	if err == nil {
		t.Fatal("expected error when no flags and .env missing")
	}
	if !strings.Contains(err.Error(), ".env") {
		t.Fatalf("error = %q, want mention of .env", err.Error())
	}
}

func TestRunRestoreUnreachableDB(t *testing.T) {
	t.Parallel()
	err := runRestore([]string{
		"--dsn", "postgres://u:p@127.0.0.1:1/db_x?connect_timeout=1",
		"--input", t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected reachability error")
	}
	if !strings.Contains(err.Error(), "ping") {
		t.Fatalf("error = %q, want ping failure", err.Error())
	}
}

func TestParseRestoreFlagsJSON(t *testing.T) {
	got, err := parseRestoreFlags([]string{"--dsn", "postgres://h/db", "--input", t.TempDir(), "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.JSON {
		t.Fatal("expected JSON to be true")
	}
}

func TestRunRestoreJSONOutput(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := restoreLoadConfig
	origPing := restorePingContext
	origRestore := restoreRestore
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		restoreLoadConfig = origLoad
		restorePingContext = origPing
		restoreRestore = origRestore
	})

	inputDir := t.TempDir()
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stubMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [{"name":"users","schema":"public","columns":[{"name":"id","data_type":"integer"}]}]
}`
	if err := os.WriteFile(filepath.Join(inputDir, "metadata.json"), []byte(stubMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	resolveLoadDotEnv = func(string, config.EnvVarNames) (string, error) {
		return "postgres://u:p@h/db", nil
	}
	restoreLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	restorePingContext = func(*sql.DB, context.Context) error { return nil }
	restoreRestore = func(context.Context, *sql.DB, string, ...restore.Option) error { return nil }

	args := []string{"--dsn", "postgres://h/db", "--input", inputDir, "--json"}

	stdout := captureStdout(func() {
		if err := runRestore(args); err != nil {
			t.Fatalf("runRestore: %v", err)
		}
	})

	var result struct {
		OK             bool     `json:"ok"`
		Command        string   `json:"command"`
		InputDir       string   `json:"input_dir"`
		TargetDatabase string   `json:"target_database"`
		Schemas        []string `json:"schemas"`
		TableCount     int      `json:"table_count"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if !result.OK {
		t.Fatal("expected ok=true")
	}
	if result.Command != "restore" {
		t.Fatalf("command = %q, want restore", result.Command)
	}
	if result.InputDir != inputDir {
		t.Fatalf("input_dir = %q, want %q", result.InputDir, inputDir)
	}
	if result.TableCount != 1 {
		t.Fatalf("table_count = %d, want 1", result.TableCount)
	}
}

func TestRunRestoreJSONErrorWrap(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := restoreLoadConfig
	origPing := restorePingContext
	origRestore := restoreRestore
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		restoreLoadConfig = origLoad
		restorePingContext = origPing
		restoreRestore = origRestore
	})

	resolveLoadDotEnv = func(string, config.EnvVarNames) (string, error) {
		return "postgres://u:p@h/db", nil
	}
	restoreLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	restorePingContext = func(*sql.DB, context.Context) error { return nil }
	restoreRestore = func(context.Context, *sql.DB, string, ...restore.Option) error {
		return errors.New("restore engine failure")
	}

	args := []string{"dolly", "restore", "--dsn", "postgres://h/db", "--input", t.TempDir(), "--json"}
	_ = captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch(args)
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
		if errObj.Command != "restore" {
			t.Fatalf("command = %q, want restore", errObj.Command)
		}
		if !strings.Contains(errObj.Error, "restore engine failure") {
			t.Fatalf("error = %q, want 'restore engine failure'", errObj.Error)
		}
	})
}

func TestRunRestoreJSONNilSchemas(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := restoreLoadConfig
	origPing := restorePingContext
	origRestore := restoreRestore
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		restoreLoadConfig = origLoad
		restorePingContext = origPing
		restoreRestore = origRestore
	})

	inputDir := t.TempDir()
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stubMeta := `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": []
}`
	if err := os.WriteFile(filepath.Join(inputDir, "metadata.json"), []byte(stubMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	resolveLoadDotEnv = func(string, config.EnvVarNames) (string, error) {
		return "postgres://u:p@h/db", nil
	}
	restoreLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	restorePingContext = func(*sql.DB, context.Context) error { return nil }
	restoreRestore = func(context.Context, *sql.DB, string, ...restore.Option) error { return nil }

	args := []string{"--dsn", "postgres://h/db", "--input", inputDir, "--json"}
	stdout := captureStdout(func() {
		if err := runRestore(args); err != nil {
			t.Fatalf("runRestore: %v", err)
		}
	})

	var result struct {
		Schemas []string `json:"schemas"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if result.Schemas == nil {
		t.Fatal("schemas must be empty array [], not null")
	}
}

func TestRunRestoreJSONMissingInput(t *testing.T) {
	// --json without --input must produce a JSON error envelope.
	_ = captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch([]string{"dolly", "restore", "--json"})
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
		if errObj.Command != "restore" {
			t.Fatalf("command = %q, want restore", errObj.Command)
		}
		if !strings.Contains(errObj.Error, "required flag --input") {
			t.Fatalf("error = %q, want 'required flag --input'", errObj.Error)
		}
	})
}

func TestRunRestoreJSONReplaceValidation(t *testing.T) {
	// Write a stub metadata so --input points to something real-ish
	// (the IO failure is later; we only test flag validation here).
	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, "public"), 0o755)

	tests := []struct {
		name    string
		flags   []string
		wantErr string
	}{
		{
			name:    "replace+on-conflict skip",
			flags:   []string{"--json", "--input", tmpDir, "--replace", "--on-conflict", "skip"},
			wantErr: "--replace cannot be combined with --on-conflict",
		},
		{
			name:    "replace without yes",
			flags:   []string{"--json", "--input", tmpDir, "--replace"},
			wantErr: "--yes to confirm",
		},
		{
			name:    "replace+no-transaction",
			flags:   []string{"--json", "--input", tmpDir, "--replace", "--yes", "--no-transaction"},
			wantErr: "--replace and --no-transaction",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = captureStdout(func() {
				stderr := captureStderr(func() {
					args := append([]string{"dolly", "restore"}, tt.flags...)
					exit := dispatch(args)
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
				if errObj.Command != "restore" {
					t.Fatalf("command = %q, want restore", errObj.Command)
				}
				if !strings.Contains(errObj.Error, tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", errObj.Error, tt.wantErr)
				}
			})
		})
	}
}

func TestRunRestoreReplaceYesTargetInfo(t *testing.T) {
	origResolve := resolveLoadDotEnv
	origLoad := restoreLoadConfig
	origPing := restorePingContext
	origRestore := restoreRestore
	t.Cleanup(func() {
		resolveLoadDotEnv = origResolve
		restoreLoadConfig = origLoad
		restorePingContext = origPing
		restoreRestore = origRestore
	})

	inputDir := t.TempDir()
	resolveLoadDotEnv = func(string, config.EnvVarNames) (string, error) {
		return "postgres://u:p@h/target_db", nil
	}
	restoreLoadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	restorePingContext = func(*sql.DB, context.Context) error { return nil }
	restoreRestore = func(context.Context, *sql.DB, string, ...restore.Option) error { return nil }

	stderr := captureStderr(func() {
		if err := runRestore([]string{"--dsn", "postgres://u:p@h/target_db", "--input", inputDir, "--replace", "--yes"}); err != nil {
			t.Fatalf("runRestore: %v", err)
		}
	})
	if !strings.Contains(stderr, "info: target database: target_db") {
		t.Fatalf("stderr = %q, want target database info", stderr)
	}
}
