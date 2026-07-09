package schemacapture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureWritesSanitizedSchemaWithPrivateMode(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		if name != "pg_dump" {
			t.Fatalf("name = %q, want pg_dump", name)
		}
		if !contains(args, "--schema-only") {
			t.Fatalf("args missing --schema-only: %v", args)
		}
		if !contains(env, "PGPASSWORD=secret") {
			t.Fatalf("env missing PGPASSWORD: %v", env)
		}
		_, err := stdout.WriteString(strings.Join([]string{
			"SET transaction_timeout = 0;",
			"CREATE TABLE public.users (id integer);",
		}, "\n"))
		return err
	}

	outDir := t.TempDir()
	if err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(outDir, "schema.sql")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "transaction_timeout") {
		t.Fatalf("schema was not sanitized:\n%s", data)
	}
	if !strings.Contains(string(data), "CREATE TABLE public.users") {
		t.Fatalf("schema missing table:\n%s", data)
	}
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestCaptureRemovesSchemaOnDumpFailure(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		_, _ = stdout.WriteString("CREATE TABLE public.partial (id integer);")
		return errors.New("boom")
	}

	outDir := t.TempDir()
	err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "schema.sql")); !os.IsNotExist(statErr) {
		t.Fatalf("schema.sql should be removed, stat err = %v", statErr)
	}
}

func stubSeams(t *testing.T) func() {
	t.Helper()
	origLookPath := lookPath
	origRunCommand := runCommand
	lookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	return func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
