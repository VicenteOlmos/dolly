package connections

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func clearConnectionEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{"DB_URL", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"} {
		t.Setenv(v, "")
	}
}

func TestConnectionFromDotEnvAllFields(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar:      "DB_URL",
		HostVar:     "DB_HOST",
		PortVar:     "DB_PORT",
		NameVar:     "DB_NAME",
		UserVar:     "DB_USER",
		PasswordVar: "DB_PASSWORD",
	}

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_HOST=myhost\nDB_PORT=5432\nDB_NAME=mydb\nDB_USER=myuser\nDB_PASSWORD=mypass\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := ConnectionFromDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Name != "local" {
		t.Fatalf("Name = %q, want local", conn.Name)
	}
	if conn.Host != "myhost" || conn.Port != "5432" || conn.Database != "mydb" || conn.User != "myuser" || conn.Password != "mypass" {
		t.Fatalf("got %+v", conn)
	}
}

func TestConnectionFromDotEnvMissingFile(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar: "DB_URL", HostVar: "DB_HOST", PortVar: "DB_PORT",
		NameVar: "DB_NAME", UserVar: "DB_USER", PasswordVar: "DB_PASSWORD",
	}

	_, err := ConnectionFromDotEnv(filepath.Join(t.TempDir(), ".env"), names)
	if err == nil {
		t.Fatal("expected error when .env is missing")
	}
	if !errors.Is(err, config.ErrSourceDSNNotFound) {
		t.Fatalf("expected ErrSourceDSNNotFound, got %v", err)
	}
}

func TestConnectionFromDotEnvEmptyPath(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar: "DB_URL", HostVar: "DB_HOST", PortVar: "DB_PORT",
		NameVar: "DB_NAME", UserVar: "DB_USER", PasswordVar: "DB_PASSWORD",
	}

	_, err := ConnectionFromDotEnv("", names)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.Is(err, config.ErrSourceDSNNotFound) {
		t.Fatalf("expected ErrSourceDSNNotFound, got %v", err)
	}
}

func TestConnectionFromDotEnvMinimalFields(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar: "DB_URL", HostVar: "DB_HOST", PortVar: "DB_PORT",
		NameVar: "DB_NAME", UserVar: "DB_USER", PasswordVar: "DB_PASSWORD",
	}

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_HOST=h\nDB_NAME=n\nDB_USER=u\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := ConnectionFromDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Name != "local" {
		t.Fatalf("Name = %q, want local", conn.Name)
	}
	if conn.Host != "h" || conn.Database != "n" || conn.User != "u" {
		t.Fatalf("got %+v", conn)
	}
	if conn.Password != "" || conn.Port != "" {
		t.Fatalf("expected empty port/password, got port=%q password=%q", conn.Port, conn.Password)
	}
}

func TestConnectionFromDotEnvURLVar(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar: "DB_URL", HostVar: "DB_HOST", PortVar: "DB_PORT",
		NameVar: "DB_NAME", UserVar: "DB_USER", PasswordVar: "DB_PASSWORD",
	}

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_URL=postgres://urluser:urlpass@urlhost:5433/urldb\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := ConnectionFromDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Name != "local" {
		t.Fatalf("Name = %q, want local", conn.Name)
	}
	if conn.Host != "urlhost" || conn.Port != "5433" || conn.Database != "urldb" || conn.User != "urluser" || conn.Password != "urlpass" {
		t.Fatalf("got %+v", conn)
	}
}

func TestConnectionFromDotEnvReturnsProfileNamedLocal(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar: "DB_URL", HostVar: "DB_HOST", PortVar: "DB_PORT",
		NameVar: "DB_NAME", UserVar: "DB_USER", PasswordVar: "DB_PASSWORD",
	}

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_HOST=h\nDB_NAME=n\nDB_USER=u\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := ConnectionFromDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Name != "local" {
		t.Fatalf("expected profile name 'local', got %q", conn.Name)
	}
}

func TestConnectionFromDotEnVPreservesSSLMODEFromURL(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar:      "DB_URL",
		HostVar:     "DB_HOST",
		PortVar:     "DB_PORT",
		NameVar:     "DB_NAME",
		UserVar:     "DB_USER",
		PasswordVar: "DB_PASSWORD",
	}

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_URL=postgres://urluser:urlpass@urlhost:5433/urldb?sslmode=require\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := ConnectionFromDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.SSLMODE != "require" {
		t.Fatalf("SSLMODE = %q, want require", conn.SSLMODE)
	}
	if conn.Host != "urlhost" || conn.Database != "urldb" {
		t.Fatalf("host=%q db=%q", conn.Host, conn.Database)
	}
}

func TestConnectionFromDotEnvURLWithoutSSLMODE(t *testing.T) {
	clearConnectionEnv(t)
	names := config.EnvVarNames{
		URLVar:      "DB_URL",
		HostVar:     "DB_HOST",
		PortVar:     "DB_PORT",
		NameVar:     "DB_NAME",
		UserVar:     "DB_USER",
		PasswordVar: "DB_PASSWORD",
	}

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_URL=postgres://urluser:urlpass@urlhost:5433/urldb\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := ConnectionFromDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.SSLMODE != "" {
		t.Fatalf("SSLMODE = %q, want empty (no sslmode in URL)", conn.SSLMODE)
	}
}
