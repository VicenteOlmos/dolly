package connections

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func TestValidateConnectionsConfigScope(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Connections.Scope = "global"
	err := ValidateConnectionsConfig(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown connections.scope") {
		t.Fatalf("ValidateConnectionsConfig = %v, want unknown scope error", err)
	}
	cfg.Connections.Scope = "project"
	if err := ValidateConnectionsConfig(cfg, t.TempDir()); err != nil {
		t.Fatalf("project scope: %v", err)
	}
	cfg.Connections.Scope = ""
	if err := ValidateConnectionsConfig(cfg, t.TempDir()); err != nil {
		t.Fatalf("empty scope: %v", err)
	}
}

func TestValidateConnectionsConfigPathParentIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Connections.Path = filepath.Join("blocker", "connections.yaml")
	err := ValidateConnectionsConfig(cfg, dir)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("ValidateConnectionsConfig = %v, want parent not directory", err)
	}
}

func TestValidateConnectionsConfigPathEmptyOK(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Connections.Path = ""
	if err := ValidateConnectionsConfig(cfg, t.TempDir()); err != nil {
		t.Fatalf("empty path: %v", err)
	}
}

func TestValidateDSNTLSFilesMissing(t *testing.T) {
	dsn := "postgres://user@host/db?sslmode=verify-full&sslrootcert=/no/such/root.crt"
	err := ValidateDSNTLSFiles(dsn)
	if err == nil || !strings.Contains(err.Error(), "sslrootcert") || !strings.Contains(err.Error(), "/no/such/root.crt") {
		t.Fatalf("ValidateDSNTLSFiles = %v, want sslrootcert path error", err)
	}
}

func TestValidateDSNTLSFilesReadable(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "root.crt")
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	dsn := "postgres://user@host/db?sslmode=verify-full&sslrootcert=" + cert
	if err := ValidateDSNTLSFiles(dsn); err != nil {
		t.Fatalf("ValidateDSNTLSFiles = %v", err)
	}
}
