package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

type dotenvSnap struct {
	bytes []byte
	mode  os.FileMode
	mtime time.Time
}

func snapshotDotenv(t *testing.T, path string, withBytes bool) dotenvSnap {
	t.Helper()
	var s dotenvSnap
	if withBytes {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s.bytes = b
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	s.mode, s.mtime = info.Mode(), info.ModTime()
	return s
}

func assertDotenvUnchanged(t *testing.T, path string, before dotenvSnap) {
	t.Helper()
	if before.bytes != nil {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before.bytes, b) {
			t.Fatal("bytes changed")
		}
	}
	after := snapshotDotenv(t, path, false)
	if after.mode.Perm() != before.mode.Perm() {
		t.Fatalf("mode changed %o -> %o", before.mode.Perm(), after.mode.Perm())
	}
	if !after.mtime.Equal(before.mtime) {
		t.Fatal("mtime changed")
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

func TestLoadDotEnvDoesNotMutateBroadDotenv(t *testing.T) {
	names := defaultNames()
	clearEnv(t, names)

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "DB_URL=postgres://u:p@host/db\n"
	if err := os.WriteFile(dotenv, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotDotenv(t, dotenv, true)

	dsn, err := LoadDotEnv(dotenv, names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dsn != "postgres://u:p@host/db" {
		t.Fatalf("expected URL DSN, got %q", dsn)
	}
	assertDotenvUnchanged(t, dotenv, before)
}

func TestReadDotEnvAdvisoryAndWriter(t *testing.T) {
	dir := t.TempDir()
	broad := filepath.Join(dir, "broad.env")
	safe := filepath.Join(dir, "safe.env")
	missing := filepath.Join(dir, "missing.env")
	if err := os.WriteFile(broad, []byte("DB_HOST=myhost\nDB_NAME=mydb\nDB_USER=myuser\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(safe, []byte("DB_HOST=h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, path, wantHost string
		useWriter, wantWarn  bool
	}{
		{"broad_warns_once", broad, "myhost", true, true},
		{"safe_quiet", safe, "h", true, false},
		{"missing_quiet", missing, "", true, false},
		{"nil_writer", broad, "myhost", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var before dotenvSnap
			if tt.wantWarn {
				before = snapshotDotenv(t, tt.path, true)
			}
			var warnings bytes.Buffer
			var writer io.Writer
			if tt.useWriter {
				writer = &warnings
			}
			env, err := readDotEnv(tt.path, writer)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantHost == "" {
				if env != nil {
					t.Fatalf("expected nil env, got %v", env)
				}
			} else if env["DB_HOST"] != tt.wantHost {
				t.Fatalf("DB_HOST=%q want %q", env["DB_HOST"], tt.wantHost)
			}
			got := strings.TrimSpace(warnings.String())
			if tt.wantWarn {
				if got != broadDotEnvPermissionsWarning {
					t.Fatalf("warning=%q", got)
				}
				assertDotenvUnchanged(t, tt.path, before)
			} else if warnings.Len() != 0 {
				t.Fatalf("unexpected warning %q", warnings.String())
			}
		})
	}
	t.Run("parse_error_wrapped", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.env")
		if err := os.WriteFile(path, []byte("DB_HOST=\nINVALID LINE\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := readDotEnv(path, nil)
		if err == nil || !strings.Contains(err.Error(), "read .env") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestReadDotEnvWarningDoesNotLeakSecrets(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	if err := os.WriteFile(dotenv, []byte("DB_URL=postgres://leakuser:leakpass@leakhost:5432/leakdb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	if _, err := readDotEnv(dotenv, &warnings); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{dotenv, "leakuser", "leakpass", "leakhost", "leakdb", "postgres://", "DB_URL"} {
		if strings.Contains(warnings.String(), forbidden) {
			t.Fatalf("warning leaks %q", forbidden)
		}
	}
}

func TestReadDotEnvSymlinkNonMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink metadata tests are Unix-focused")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")
	link := filepath.Join(dir, ".env")
	if err := os.WriteFile(target, []byte("DB_HOST=symlink-host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	beforeTarget := snapshotDotenv(t, target, true)
	env, err := readDotEnv(link, &bytes.Buffer{})
	if err != nil || env["DB_HOST"] != "symlink-host" {
		t.Fatalf("env=%v err=%v", env, err)
	}
	assertDotenvUnchanged(t, target, beforeTarget)
}
