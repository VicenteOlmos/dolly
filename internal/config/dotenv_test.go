package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearEnv(t *testing.T, names EnvVarNames) {
	t.Setenv(names.URLVar, "")
	t.Setenv(names.HostVar, "")
	t.Setenv(names.PortVar, "")
	t.Setenv(names.NameVar, "")
	t.Setenv(names.UserVar, "")
	t.Setenv(names.PasswordVar, "")
}

func defaultNames() EnvVarNames {
	return EnvVarNames{
		URLVar:      "DB_URL",
		HostVar:     "DB_HOST",
		PortVar:     "DB_PORT",
		NameVar:     "DB_NAME",
		UserVar:     "DB_USER",
		PasswordVar: "DB_PASSWORD",
	}
}

func TestLoadDotEnvURLVarWins(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_URL=postgres://u:p@host/db\nDB_HOST=h-c\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dsn, err := LoadDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dsn != "postgres://u:p@host/db" {
		t.Fatalf("expected URL DSN, got %q", dsn)
	}
}

func TestLoadDotEnvDiscreteVarsBuildDSN(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_HOST=h-a\nDB_PORT=5432\nDB_NAME=db_a\nDB_USER=u\nDB_PASSWORD=p\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dsn, err := LoadDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "postgres://u:p@h-a:5432/db_a"
	if dsn != want {
		t.Fatalf("expected %q, got %q", want, dsn)
	}
}

func TestLoadDotEnvDiscreteVarsNoPassword(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_HOST=h-a\nDB_NAME=db_a\nDB_USER=u\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dsn, err := LoadDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "postgres://u@h-a/db_a"
	if dsn != want {
		t.Fatalf("expected %q, got %q", want, dsn)
	}
}

func TestLoadDotEnvMissingAllReturnsError(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	_, err := LoadDotEnv(filepath.Join(t.TempDir(), ".env"), names)
	if err == nil {
		t.Fatal("expected error when DSN vars are missing")
	}
	if !errors.Is(err, ErrSourceDSNNotFound) {
		t.Fatalf("expected ErrSourceDSNNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "DB_URL") {
		t.Fatalf("expected actionable error, got %q", err.Error())
	}
}

func TestLoadDotEnvConfigurableNames(t *testing.T) {
	names := EnvVarNames{
		URLVar:      "URL_DB",
		HostVar:     "HOST_DB",
		PortVar:     "PORT_DB",
		NameVar:     "NAME_DB",
		UserVar:     "USER_DB",
		PasswordVar: "PASS_DB",
	}
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "HOST_DB=h-b\nPORT_DB=5433\nNAME_DB=db_x\nUSER_DB=u\nPASS_DB=p\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dsn, err := LoadDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "postgres://u:p@h-b:5433/db_x"
	if dsn != want {
		t.Fatalf("expected %q, got %q", want, dsn)
	}
}

func TestLoadDotEnvQuotedValues(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := `DB_URL="postgres://u:p@host/db"` + "\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dsn, err := LoadDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dsn != "postgres://u:p@host/db" {
		t.Fatalf("expected unquoted URL, got %q", dsn)
	}
}

func TestLoadDotEnvMissingFileFallsBackToShell(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	t.Setenv("DB_URL", "postgres://h-env/db_a")

	dsn, err := LoadDotEnv(filepath.Join(t.TempDir(), "missing.env"), names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dsn != "postgres://h-env/db_a" {
		t.Fatalf("expected shell DSN, got %q", dsn)
	}
}

// LoadDotEnvComponents tests

func TestLoadDotEnvComponentsAllVarsPresent(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_HOST=myhost\nDB_PORT=5432\nDB_NAME=mydb\nDB_USER=myuser\nDB_PASSWORD=mypass\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	host, port, name, user, password, err := LoadDotEnvComponents(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "myhost" || port != "5432" || name != "mydb" || user != "myuser" || password != "mypass" {
		t.Fatalf("got host=%q port=%q name=%q user=%q password=%q", host, port, name, user, password)
	}
}

func TestLoadDotEnvComponentsAbsentFileShellFallback(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)
	t.Setenv("DB_HOST", "shellhost")
	t.Setenv("DB_NAME", "shelldb")
	t.Setenv("DB_USER", "shelluser")

	host, port, name, user, _, err := LoadDotEnvComponents(filepath.Join(t.TempDir(), ".env"), names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "shellhost" || name != "shelldb" || user != "shelluser" {
		t.Fatalf("got host=%q name=%q user=%q", host, name, user)
	}
	if port != "" {
		t.Fatalf("expected empty port, got %q", port)
	}
}

func TestLoadDotEnvComponentsMissingRequiredReturnsError(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	if err := os.WriteFile(dotenv, []byte("DB_PASSWORD=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, _, _, _, err := LoadDotEnvComponents(dotenv, names)
	if err == nil {
		t.Fatal("expected ErrSourceDSNNotFound")
	}
	if !errors.Is(err, ErrSourceDSNNotFound) {
		t.Fatalf("expected ErrSourceDSNNotFound, got %v", err)
	}
}

func TestLoadDotEnvComponentsPortAbsentPartialResult(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_HOST=h\nDB_NAME=n\nDB_USER=u\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	host, port, name, user, _, err := LoadDotEnvComponents(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host == "" || name == "" || user == "" {
		t.Fatalf("expected non-empty host/name/user, got host=%q name=%q user=%q", host, name, user)
	}
	if port != "" {
		t.Fatalf("expected empty port, got %q", port)
	}
}

func TestLoadDotEnvComponentsURLVarParsedToComponents(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_URL=postgres://urluser:urlpass@urlhost:5433/urldb\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	host, port, name, user, password, err := LoadDotEnvComponents(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "urlhost" || port != "5433" || name != "urldb" || user != "urluser" || password != "urlpass" {
		t.Fatalf("got host=%q port=%q name=%q user=%q password=%q", host, port, name, user, password)
	}
}
