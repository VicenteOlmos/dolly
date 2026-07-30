//go:build unix

package schemacapture

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCaptureKilledPgDumpPreservesPriorSchema(t *testing.T) {
	if os.Getenv("DOLLY_SCHEMA_CAPTURE_PGDUMP_HELPER") == "1" {
		schemaCapturePgDumpHelper(t)
		return
	}

	restore := stubSeams(t)
	defer restore()

	outDir := t.TempDir()
	priorPath := seedPriorSchema(t, outDir)
	readyFile := filepath.Join(outDir, ".pgdump-ready")

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		dumpCmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCaptureKilledPgDumpPreservesPriorSchema$", "-test.count=1")
		dumpCmd.Env = append(env,
			"DOLLY_SCHEMA_CAPTURE_PGDUMP_HELPER=1",
			"DOLLY_SCHEMA_CAPTURE_READY="+readyFile,
		)
		dumpCmd.Stdout = stdout
		var stderr strings.Builder
		dumpCmd.Stderr = &stderr
		if err := dumpCmd.Start(); err != nil {
			return fmt.Errorf("pg_dump failed: %w", err)
		}

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(readyFile); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := os.Stat(readyFile); err != nil {
			_ = dumpCmd.Process.Kill()
			_ = dumpCmd.Wait()
			return fmt.Errorf("pg_dump helper never became ready")
		}
		if err := dumpCmd.Process.Signal(syscall.SIGKILL); err != nil {
			return fmt.Errorf("kill pg_dump: %w", err)
		}
		waitErr := dumpCmd.Wait()
		if waitErr == nil {
			return fmt.Errorf("pg_dump exited cleanly after SIGKILL")
		}
		return fmt.Errorf("pg_dump failed: %w (stderr: %s)", waitErr, stderr.String())
	}

	err := Capture(context.Background(), "postgres://u:secret@localhost/db", outDir, nil)
	if err == nil {
		t.Fatal("expected error after killed pg_dump")
	}

	assertPriorSchemaUnchanged(t, priorPath)
	assertNoRunCaptureTemps(t, outDir)
}

func schemaCapturePgDumpHelper(t *testing.T) {
	readyFile := os.Getenv("DOLLY_SCHEMA_CAPTURE_READY")
	if readyFile == "" {
		t.Fatal("missing helper env")
	}
	if _, err := os.Stdout.WriteString("CREATE TABLE public.partial (id integer);\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readyFile, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestCaptureOrphanTempNeverPublishes(t *testing.T) {
	restore := stubSeams(t)
	defer restore()

	runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
		_, err := stdout.WriteString("CREATE TABLE public.users (id integer);")
		return err
	}

	outDir := t.TempDir()
	priorPath := seedPriorSchema(t, outDir)
	orphanPath := filepath.Join(outDir, ".schema.sql.tmp-orphan")
	if err := os.WriteFile(orphanPath, []byte("-- leaked partial\n"), 0o600); err != nil {
		t.Fatal(err)
	}

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
	if strings.Contains(string(got), "leaked partial") {
		t.Fatalf("orphan temp content published into schema.sql:\n%s", got)
	}
	if !strings.Contains(string(got), "CREATE TABLE public.users") {
		t.Fatalf("schema.sql missing new content:\n%s", got)
	}
}
