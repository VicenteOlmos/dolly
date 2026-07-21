package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// chdir temporarily changes the working directory so that
// runConfigInit operates in an isolated temp dir.
func chdirTemp(t *testing.T) (restore func()) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Chdir(orig) }
}

func TestConfigInit_writesTemplate(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	err := runConfigInit(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile("config.jsonc")
	if err != nil {
		t.Fatalf("config.jsonc not created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("config.jsonc is empty")
	}
}

func TestConfigInit_refusesWithoutForce(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	// Pre-create the file.
	if err := os.WriteFile("config.jsonc", []byte(`{"existing":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runConfigInit(nil)
	if err == nil {
		t.Fatal("expected error when file exists without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error should mention --force, got: %v", err)
	}

	// Existing file must be unchanged.
	data, _ := os.ReadFile("config.jsonc")
	if string(data) != `{"existing":true}` {
		t.Fatalf("existing file was modified: %s", data)
	}
}

func TestConfigInit_forceOverwrites(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := os.WriteFile("config.jsonc", []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runConfigInit([]string{"--force"})
	if err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}

	data, err := os.ReadFile("config.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old content" {
		t.Fatal("--force should have overwritten the file")
	}
	if len(data) == 0 {
		t.Fatal("written file is empty")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat("config.jsonc")
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode = %o, want 600", got)
		}
	}
	if matches, err := filepath.Glob(".config.jsonc.tmp-*"); err != nil || len(matches) != 0 {
		t.Fatalf("temporary config files remain: %v, %v", matches, err)
	}
}

func TestConfigInit_helpFlag(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	// -h must return nil and must NOT create config.jsonc.
	out := captureStderr(func() {
		if err := runConfigInit([]string{"-h"}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	if _, err := os.Stat("config.jsonc"); err == nil {
		t.Fatal("-h should not create config.jsonc")
	}
	_ = out
}

func TestConfigInit_rejectsUnknownFlagsWithoutWriting(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := runConfigInit([]string{"--unknown"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
	if _, err := os.Stat("config.jsonc"); !os.IsNotExist(err) {
		t.Fatalf("unknown flag created config.jsonc: %v", err)
	}
}

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestConfigShow_printsJSON(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	out := captureStdout(func() {
		if err := runConfigShow(nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, `"env"`) {
		t.Fatalf("output missing env section: %s", out)
	}
	if !strings.Contains(out, `"clone"`) {
		t.Fatalf("output missing clone section: %s", out)
	}
}

func TestConfigShow_redactsTargetURLCredentials(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := os.WriteFile("config.jsonc", []byte(`{"clone":{"target_url":"postgres://user:secret@db.example/app?sslmode=require&token=hidden"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(func() {
		if err := runConfigShow(nil); err != nil {
			t.Errorf("runConfigShow: %v", err)
		}
	})
	if strings.Contains(out, "secret") || strings.Contains(out, "hidden") {
		t.Fatalf("config show leaked credential: %s", out)
	}
	if !strings.Contains(out, "db.example") || !strings.Contains(out, "sslmode=require") {
		t.Fatalf("config show lost usable target details: %s", out)
	}
}

func TestConfigShow_exitZero(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	err := runConfigShow(nil)
	if err != nil {
		t.Fatalf("runConfigShow: %v", err)
	}
}

func TestConfigShow_loadError(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	// Write a malformed config so LoadConfig fails.
	if err := os.WriteFile("config.jsonc", []byte(`{bad`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runConfigShow(nil)
	if err == nil {
		t.Fatal("expected error for malformed config")
	}
}

func TestRunConfig_unknownSubcommand(t *testing.T) {
	err := runConfig([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunConfig_noArgs(t *testing.T) {
	err := runConfig(nil)
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunConfig_showSubcommand(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	err := runConfig([]string{"show"})
	if err != nil {
		t.Fatalf("runConfig show: %v", err)
	}
}

func TestRunConfig_initSubcommand(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	err := runConfig([]string{"init"})
	if err != nil {
		t.Fatalf("runConfig init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".", "config.jsonc")); err != nil {
		t.Fatal("config.jsonc was not created by runConfig init")
	}
}
