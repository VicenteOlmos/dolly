package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
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
