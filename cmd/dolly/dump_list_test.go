package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dumphistory"
)

func TestRunDumpListEmpty(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	oldLoad := dumpLoadConfig
	t.Cleanup(func() { dumpLoadConfig = oldLoad })

	cfg := config.DefaultConfig()
	cfg.Dump.OutputDir = filepath.Join(workDir, "dumps")
	dumpLoadConfig = func(string) (*config.Config, error) { return cfg, nil }

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w

	err := runDumpList(nil)

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("runDumpList: %v", err)
	}
	if !strings.Contains(buf.String(), "No dumps") {
		t.Fatalf("output = %q, want no dumps message", buf.String())
	}
}

func TestRunDumpListText(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	baseDir := filepath.Join(workDir, "dumps")
	dir1 := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeStubDumpMetadata(dir1); err != nil {
		t.Fatal(err)
	}

	store, err := dumphistory.OpenStore(config.DefaultConfig(), ".")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Register(dumphistory.Record{
		Seq:            1,
		BaseDir:        baseDir,
		Path:           dir1,
		CreatedAt:      now,
		SourceDatabase: "mydb",
		SchemaLabel:    "public",
		TableCount:     1,
	}); err != nil {
		t.Fatal(err)
	}

	oldLoad := dumpLoadConfig
	t.Cleanup(func() { dumpLoadConfig = oldLoad })
	dumpLoadConfig = func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Dump.OutputDir = baseDir
		return cfg, nil
	}

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w

	if err = runDumpList(nil); err != nil {
		t.Fatalf("runDumpList: %v", err)
	}
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "SEQ") || !strings.Contains(out, dir1) || !strings.Contains(out, "public") {
		t.Fatalf("output = %q, want table header and dump row", out)
	}
}

func TestRunDumpListJSON(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	baseDir := filepath.Join(workDir, "dumps")
	dir1 := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeStubDumpMetadata(dir1); err != nil {
		t.Fatal(err)
	}

	oldLoad := dumpLoadConfig
	t.Cleanup(func() { dumpLoadConfig = oldLoad })
	dumpLoadConfig = func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Dump.OutputDir = baseDir
		return cfg, nil
	}

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w

	err := runDumpList([]string{"--json"})

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("runDumpList: %v", err)
	}
	var recs []dumphistory.Record
	if err := json.Unmarshal(buf.Bytes(), &recs); err != nil {
		t.Fatalf("json: %v body=%q", err, buf.String())
	}
	if len(recs) != 1 || recs[0].Seq != 1 {
		t.Fatalf("records = %+v, want one seq=1", recs)
	}
}

func TestDispatchDumpList(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	baseDir := filepath.Join(workDir, "dumps")
	if err := os.MkdirAll(filepath.Join(baseDir, "1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeStubDumpMetadata(filepath.Join(baseDir, "1")); err != nil {
		t.Fatal(err)
	}

	oldLoad := dumpLoadConfig
	t.Cleanup(func() { dumpLoadConfig = oldLoad })
	dumpLoadConfig = func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Dump.OutputDir = baseDir
		return cfg, nil
	}

	code := dispatch([]string{"dolly", "dump", "list", "--output", baseDir})
	if code != 0 {
		t.Fatalf("dispatch exit = %d, want 0", code)
	}
}

func TestRunDumpListJSONEmpty(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	outDir := filepath.Join(workDir, "dumps")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldLoad := dumpLoadConfig
	t.Cleanup(func() { dumpLoadConfig = oldLoad })
	dumpLoadConfig = func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Dump.OutputDir = outDir
		return cfg, nil
	}

	stdout := captureStdout(func() {
		exit := dispatch([]string{"dolly", "dump", "list", "--json", "--output", outDir})
		if exit != 0 {
			t.Fatalf("dispatch exit = %d, want 0", exit)
		}
	})

	// Must be exactly "[]" (empty array, not null).
	trimmed := strings.TrimSpace(stdout)
	if trimmed != "[]" {
		t.Fatalf("stdout = %q, want exactly []", trimmed)
	}
}

func TestRunDumpListJSONError(t *testing.T) {
	// Root can read anything — skip.
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	outDir := t.TempDir()
	if err := os.Chmod(outDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(outDir, 0o755) })

	oldLoad := dumpLoadConfig
	t.Cleanup(func() { dumpLoadConfig = oldLoad })
	dumpLoadConfig = func(string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Dump.OutputDir = outDir
		return cfg, nil
	}

	_ = captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch([]string{"dolly", "dump", "list", "--json", "--output", outDir})
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
		if errObj.Command != "dump list" {
			t.Fatalf("command = %q, want 'dump list'", errObj.Command)
		}
		if errObj.Error == "" {
			t.Fatal("expected non-empty error message")
		}
	})
}
