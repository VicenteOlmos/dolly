package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestResolveDataSourceDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	_, _, err := resolveDataSource(cfg, ".", "prod", "")
	if err == nil || !strings.Contains(err.Error(), "save_connections is disabled") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveDataSourceNotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true

	_, _, err := resolveDataSource(cfg, dir, "missing", "")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveDataSourceReturnsDSNAndSchemas(t *testing.T) {
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
		User: "u", Password: "p", Schemas: []string{"app", "billing"},
	}); err != nil {
		t.Fatal(err)
	}

	dsn, schemas, err := resolveDataSource(cfg, dir, "prod", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "h-a:5432/db_a") {
		t.Fatalf("dsn = %q", dsn)
	}
	if len(schemas) != 2 || schemas[0] != "app" {
		t.Fatalf("schemas = %v", schemas)
	}
}

func TestValidateDSNOrConnectionMutualExclusion(t *testing.T) {
	if err := validateDSNOrConnection("prod", "postgres://x"); err == nil {
		t.Fatal("expected mutual exclusion error")
	}
	if !strings.Contains(validateDSNOrConnection("prod", "postgres://x").Error(), "mutually exclusive") {
		t.Fatal("expected 'mutually exclusive' in error message")
	}
}

func TestValidateDSNOrConnectionBothEmptyNowOK(t *testing.T) {
	if err := validateDSNOrConnection("", ""); err != nil {
		t.Fatalf("both-empty should now succeed, got: %v", err)
	}
}

// .env fallback tests using the resolveLoadDotEnv test seam

func TestResolveDataSourceBothEmptyUsesEnvDSN(t *testing.T) {
	orig := resolveLoadDotEnv
	t.Cleanup(func() { resolveLoadDotEnv = orig })

	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		return "postgres://envuser:envpass@envhost/envdb", nil
	}

	cfg := config.DefaultConfig()
	dsn, schemas, err := resolveDataSource(cfg, ".", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if dsn != "postgres://envuser:envpass@envhost/envdb" {
		t.Fatalf("expected env DSN, got %q", dsn)
	}
	if len(schemas) != 0 {
		t.Fatalf("expected no schemas from env, got %v", schemas)
	}
}

func TestResolveDataSourceDSNFlagOverridesEnv(t *testing.T) {
	orig := resolveLoadDotEnv
	t.Cleanup(func() { resolveLoadDotEnv = orig })
	called := false
	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		called = true
		return "postgres://env/db", nil
	}

	cfg := config.DefaultConfig()
	dsn, _, err := resolveDataSource(cfg, ".", "", "postgres://flag/db")
	if err != nil {
		t.Fatal(err)
	}
	if dsn != "postgres://flag/db" {
		t.Fatalf("expected flag DSN, got %q", dsn)
	}
	if called {
		t.Fatal("resolveLoadDotEnv should not be called when --dsn flag is set")
	}
}

func TestResolveDataSourceConnectionFlagOverridesEnv(t *testing.T) {
	orig := resolveLoadDotEnv
	t.Cleanup(func() { resolveLoadDotEnv = orig })
	called := false
	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		called = true
		return "postgres://env/db", nil
	}

	dir := t.TempDir()
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	dotenv := filepath.Join(dir, ".env")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	cfg.Env.Path = dotenv
	store, err := connections.OpenStore(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(connections.Connection{
		Name: "myconn", Host: "h", Port: "5432", Database: "db", User: "u",
	}); err != nil {
		t.Fatal(err)
	}

	dsn, _, err := resolveDataSource(cfg, dir, "myconn", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "h:5432/db") {
		t.Fatalf("expected profile DSN, got %q", dsn)
	}
	if called {
		t.Fatal("resolveLoadDotEnv should not be called when --connection flag is set")
	}
}

func TestResolveDataSourceNoEnvNoFlagsReturnsError(t *testing.T) {
	orig := resolveLoadDotEnv
	t.Cleanup(func() { resolveLoadDotEnv = orig })
	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		return "", config.ErrSourceDSNNotFound
	}

	cfg := config.DefaultConfig()
	_, _, err := resolveDataSource(cfg, ".", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--dsn") || !strings.Contains(err.Error(), "--connection") || !strings.Contains(err.Error(), ".env") {
		t.Fatalf("expected friendly error message, got: %q", err.Error())
	}
}

func TestResolveDataSourceMutualExclusionError(t *testing.T) {
	err := validateDSNOrConnection("conn", "dsn")
	if err == nil {
		t.Fatal("expected mutual exclusion error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected 'mutually exclusive', got: %q", err.Error())
	}
}

func TestEnvNamesFromConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	names := envNamesFromConfig(cfg)
	if names.URLVar != cfg.Env.URLVar {
		t.Fatalf("URLVar mismatch: %q vs %q", names.URLVar, cfg.Env.URLVar)
	}
	if names.HostVar != cfg.Env.HostVar {
		t.Fatalf("HostVar mismatch")
	}
}

func TestResolveDataSourceEnvNonDSNError(t *testing.T) {
	orig := resolveLoadDotEnv
	t.Cleanup(func() { resolveLoadDotEnv = orig })

	sentinelErr := errors.New("parse error")
	resolveLoadDotEnv = func(_ string, _ config.EnvVarNames) (string, error) {
		return "", sentinelErr
	}

	cfg := config.DefaultConfig()
	_, _, err := resolveDataSource(cfg, ".", "", "")
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestAppendQueryParamDelegatesURLAndKeyword(t *testing.T) {
	got, err := appendQueryParam("postgres://user@localhost/db?sslmode=disable", "statement_timeout", "30s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "statement_timeout=30s") || !strings.Contains(got, "sslmode=disable") {
		t.Fatalf("url inject = %q", got)
	}

	got, err = appendQueryParam("host=localhost port=5432", "statement_timeout", "5min")
	if err != nil {
		t.Fatal(err)
	}
	if got != "host=localhost port=5432 statement_timeout=5min" {
		t.Fatalf("keyword inject = %q", got)
	}
}

func TestAppendQueryParamMalformedRedacted(t *testing.T) {
	const secret = "TOPSECRET99"
	for _, tc := range []struct{ name, dsn string }{
		{"url_bad_scheme", "https://user:" + secret + "@localhost/db"},
		{"keyword_trailing_garbage", "host=localhost password=" + secret + " notvalid"},
		{"unclassifiable", "not a dsn with " + secret},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := appendQueryParam(tc.dsn, "statement_timeout", "30s")
			if err == nil {
				t.Fatal("expected malformed DSN error")
			}
			if !errors.Is(err, connections.ErrMalformedDSN) {
				t.Fatalf("err = %v, want ErrMalformedDSN", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), tc.dsn) {
				t.Fatalf("error leaks secret or DSN: %q", err.Error())
			}
		})
	}
}

func assertMalformedDSNAbort(t *testing.T, err error, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected malformed DSN error")
	}
	if !errors.Is(err, connections.ErrMalformedDSN) {
		t.Fatalf("err = %v, want ErrMalformedDSN", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks secret: %q", err.Error())
	}
}

func timeoutConfigLoader(timeout string) func(string) (*config.Config, error) {
	return func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.DB.StatementTimeout = timeout
		return cfg, nil
	}
}

func withDumpSeams(t *testing.T) (pingCalled, runCalled *bool) {
	origLoad, origPing, origRun := dumpLoadConfig, dumpPingContext, dumpRun
	t.Cleanup(func() { dumpLoadConfig, dumpPingContext, dumpRun = origLoad, origPing, origRun })
	ping, run := false, false
	dumpPingContext = func(*sql.DB, context.Context) error { ping = true; return nil }
	dumpRun = func(context.Context, *sql.DB, string, ...dump.Option) error { run = true; return nil }
	dumpLoadConfig = timeoutConfigLoader("30s")
	return &ping, &run
}

func withRestoreSeams(t *testing.T) (pingCalled, runCalled *bool) {
	origLoad, origPing, origRun := restoreLoadConfig, restorePingContext, restoreRestore
	t.Cleanup(func() { restoreLoadConfig, restorePingContext, restoreRestore = origLoad, origPing, origRun })
	ping, run := false, false
	restorePingContext = func(*sql.DB, context.Context) error { ping = true; return nil }
	restoreRestore = func(context.Context, *sql.DB, string, ...restore.Option) error { run = true; return nil }
	restoreLoadConfig = timeoutConfigLoader("30s")
	return &ping, &run
}

func TestRunDumpMalformedDSNBeforePing(t *testing.T) {
	const secret = "DUMPSECRET99"
	pingCalled, runCalled := withDumpSeams(t)
	err := runDump([]string{"--dsn", "host=localhost password=" + secret + " notvalid", "--output", t.TempDir()})
	assertMalformedDSNAbort(t, err, secret)
	if *pingCalled || *runCalled {
		t.Fatalf("side effects: ping=%v run=%v", *pingCalled, *runCalled)
	}
}

func TestRunRestoreMalformedDSNBeforePing(t *testing.T) {
	const secret = "RESTORESECRET99"
	pingCalled, runCalled := withRestoreSeams(t)
	err := runRestore([]string{"--dsn", "https://user:" + secret + "@localhost/db", "--input", t.TempDir()})
	assertMalformedDSNAbort(t, err, secret)
	if *pingCalled || *runCalled {
		t.Fatalf("side effects: ping=%v run=%v", *pingCalled, *runCalled)
	}
}

func TestRunCloneExecuteMalformedDSNBeforeCloneRun(t *testing.T) {
	const secret = "CLONESECRET99"
	origRun := cloneRun
	t.Cleanup(func() { cloneRun = origRun })
	runCalled := false
	cloneRun = func(context.Context, clone.Options) error { runCalled = true; return nil }
	cfg := config.DefaultConfig()
	cfg.DB.StatementTimeout = "5min"
	err := runCloneExecute(context.Background(), cloneFlags{}, cfg,
		"host=localhost password="+secret+" notvalid", "clone_x", "", nil, "schema-replay")
	assertMalformedDSNAbort(t, err, secret)
	if runCalled {
		t.Fatal("cloneRun invoked on malformed source DSN")
	}
}
