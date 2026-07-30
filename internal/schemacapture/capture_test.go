package schemacapture

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const priorSchemaSQL = "-- prior schema\nCREATE TABLE users (id int);\n"

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
	if err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil); err != nil {
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
	assertNoRunCaptureTemps(t, outDir)
}

func TestCaptureRemovesTempOnDumpFailure(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		_, _ = stdout.WriteString("CREATE TABLE public.partial (id integer);")
		return errors.New("boom")
	}

	outDir := t.TempDir()
	err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	assertNoSchemaArtifacts(t, outDir)
}

func TestCapturePreservesPriorSchemaOnDumpFailure(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		if data, err := os.ReadFile(filepath.Join(filepath.Dir(stdout.Name()), "schema.sql")); err != nil {
			t.Fatalf("read prior schema.sql during dump: %v", err)
		} else if !bytes.Equal(data, []byte(priorSchemaSQL)) {
			t.Fatalf("schema.sql mutated during dump:\n%s", data)
		}
		_, _ = stdout.WriteString("CREATE TABLE public.partial (id integer);")
		return errors.New("boom")
	}

	outDir := t.TempDir()
	priorPath := seedPriorSchema(t, outDir)
	err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	assertPriorSchemaUnchanged(t, priorPath)
	assertNoRunCaptureTemps(t, outDir)
}

func TestCapturePreservesPriorSchemaOnSanitizeFailure(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		line := make([]byte, bufio.MaxScanTokenSize+1)
		for i := range line {
			line[i] = 'a'
		}
		if _, err := stdout.Write(line); err != nil {
			return err
		}
		return nil
	}

	outDir := t.TempDir()
	priorPath := seedPriorSchema(t, outDir)
	err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	assertPriorSchemaUnchanged(t, priorPath)
}

func TestCapturePreservesPriorSchemaOnReplaceFailure(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		_, err := stdout.WriteString("CREATE TABLE public.users (id integer);")
		return err
	}
	replaceFile = func(src, dst string) error {
		if !isSchemaCaptureTemp(filepath.Base(src)) || filepath.Base(dst) != "schema.sql" {
			t.Fatalf("replace paths = %q -> %q", src, dst)
		}
		return errors.New("rename blocked")
	}

	outDir := t.TempDir()
	priorPath := seedPriorSchema(t, outDir)
	err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil)
	if err == nil || !strings.Contains(err.Error(), "replace schema.sql") {
		t.Fatalf("Capture error = %v", err)
	}
	assertPriorSchemaUnchanged(t, priorPath)
	assertNoRunCaptureTemps(t, outDir)
}

func TestCaptureUsesSameDirectoryTempAndAtomicReplace(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	var dumpPath string
	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		dumpPath = stdout.Name()
		if !isSchemaCaptureTemp(filepath.Base(dumpPath)) {
			t.Fatalf("tmp base = %q, want %s", filepath.Base(dumpPath), schemaCaptureTempPattern)
		}
		_, err := stdout.WriteString("CREATE TABLE public.users (id integer);")
		return err
	}

	outDir := t.TempDir()
	if err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil); err != nil {
		t.Fatal(err)
	}
	if dumpPath == "" {
		t.Fatal("pg_dump never ran")
	}
	if filepath.Dir(dumpPath) != outDir {
		t.Fatalf("tmp dir = %q, want %q", filepath.Dir(dumpPath), outDir)
	}
}

func TestCaptureRejectsMalformedDSNBeforeRunningCommand(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()
	ran := false
	runCommand = func(context.Context, string, []string, []string, *os.File) error {
		ran = true
		return nil
	}

	err := Capture(context.Background(), "not a postgres DSN", t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "clean DSN") {
		t.Fatalf("Capture error = %v", err)
	}
	if ran {
		t.Fatal("pg_dump ran with a malformed DSN")
	}
}

func TestCapturePassesRepeatedSchemaArgs(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	var gotArgs []string
	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		gotArgs = append([]string(nil), args...)
		_, err := stdout.WriteString("CREATE TABLE app.users (id integer);")
		return err
	}

	outDir := t.TempDir()
	if err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, []string{"app", "billing"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"--schema-only", "--no-owner", "--no-acl"}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Fatalf("args[%d] = %q want %q (full %v)", i, gotArgs[i], want[i], gotArgs)
		}
	}
	var schemaNames []string
	for i := 0; i+1 < len(gotArgs); i++ {
		if gotArgs[i] == "--schema" {
			schemaNames = append(schemaNames, gotArgs[i+1])
		}
	}
	if len(schemaNames) != 2 || schemaNames[0] != "app" || schemaNames[1] != "billing" {
		t.Fatalf("schema args = %v", schemaNames)
	}
	if !strings.HasPrefix(gotArgs[len(gotArgs)-1], "postgres://") {
		t.Fatalf("last arg = %q, want postgres DSN", gotArgs[len(gotArgs)-1])
	}
}

func TestCaptureReplacesExistingSchemaOnSuccess(t *testing.T) {
	restoreSeams := stubSeams(t)
	defer restoreSeams()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		_, err := stdout.WriteString("CREATE TABLE public.new_table (id integer);")
		return err
	}

	outDir := t.TempDir()
	priorPath := seedPriorSchema(t, outDir)
	if err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(priorPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, []byte(priorSchemaSQL)) {
		t.Fatal("schema.sql was not replaced on success")
	}
	if !strings.Contains(string(got), "new_table") {
		t.Fatalf("schema.sql missing new content:\n%s", got)
	}
}

func seedPriorSchema(t *testing.T, outDir string) string {
	t.Helper()
	path := filepath.Join(outDir, "schema.sql")
	if err := os.WriteFile(path, []byte(priorSchemaSQL), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertPriorSchemaUnchanged(t *testing.T, priorPath string) {
	t.Helper()
	got, err := os.ReadFile(priorPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(priorSchemaSQL)) {
		t.Fatalf("prior schema.sql changed:\n%s", got)
	}
}

func assertNoSchemaArtifacts(t *testing.T, outDir string) {
	t.Helper()
	if _, statErr := os.Stat(filepath.Join(outDir, "schema.sql")); !os.IsNotExist(statErr) {
		t.Fatalf("schema.sql should not exist, stat err = %v", statErr)
	}
	assertNoRunCaptureTemps(t, outDir)
}

func assertNoRunCaptureTemps(t *testing.T, outDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(outDir, schemaCaptureTempPattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Fatalf("run capture temps should be removed, found %v", matches)
	}
}

func isSchemaCaptureTemp(name string) bool {
	matched, err := filepath.Match(schemaCaptureTempPattern, name)
	return err == nil && matched
}

func stubSeams(t *testing.T) func() {
	t.Helper()
	origLookPath := lookPath
	origRunCommand := runCommand
	origReplaceFile := replaceFile
	lookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	return func() {
		lookPath = origLookPath
		runCommand = origRunCommand
		replaceFile = origReplaceFile
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
