package schemacapture

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/schemasql"
)

const schemaCaptureTempPattern = ".schema.sql.tmp-*"

var lookPath = exec.LookPath

// runCommand is a test seam for pg_dump execution.
var runCommand = func(ctx context.Context, name string, args []string, env []string, stdout *os.File) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Stdout = stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w (stderr: %s)", err, connections.RedactMessage(stderr.String()))
	}
	return nil
}

// replaceFile is a test seam for atomic schema.sql publication.
var replaceFile = atomicReplaceFile

// Capture runs pg_dump --schema-only on dsn and writes a sanitized schema.sql.
// When schemas is non-empty, each name is passed as a separate --schema argv pair.
// pg_dump writes to a same-directory unguessable private temp; schema.sql is
// replaced only after sanitize succeeds. Any error removes the run temp and
// leaves prior schema.sql byte-for-byte unchanged. Host-process SIGKILL cannot
// run deferred cleanup and may leave an orphaned run temp; prior schema.sql is
// never published or corrupted.
func Capture(ctx context.Context, dsn, outDir string, schemas []string) error {
	if _, err := lookPath("pg_dump"); err != nil {
		return fmt.Errorf("pg_dump not on PATH, schema.sql skipped")
	}
	cleanDSN, password, err := connections.SubprocessDSN(dsn)
	if err != nil {
		return fmt.Errorf("clean DSN: %w", err)
	}
	args := []string{"--schema-only", "--no-owner", "--no-acl"}
	for _, schema := range schemas {
		args = append(args, "--schema", schema)
	}
	args = append(args, cleanDSN)
	env := clone.StripSensitiveEnv(os.Environ())
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}

	finalPath := filepath.Join(outDir, "schema.sql")

	tmp, err := os.CreateTemp(outDir, schemaCaptureTempPattern)
	if err != nil {
		return fmt.Errorf("create schema capture temp: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	cleanup := func() {
		if committed {
			return
		}
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	defer cleanup()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod schema capture temp: %w", err)
	}

	if err := runCommand(ctx, "pg_dump", args, env, tmp); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync schema capture temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close schema capture temp: %w", err)
	}

	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read schema capture temp: %w", err)
	}
	sanitized, err := schemasql.Sanitize(raw)
	if err != nil {
		return fmt.Errorf("sanitize schema.sql: %w", err)
	}
	if err := os.WriteFile(tmpPath, sanitized, 0o600); err != nil {
		return fmt.Errorf("write schema capture temp: %w", err)
	}
	if err := syncPath(tmpPath); err != nil {
		return fmt.Errorf("sync schema capture temp: %w", err)
	}

	if err := replaceFile(tmpPath, finalPath); err != nil {
		return fmt.Errorf("replace schema.sql: %w", err)
	}
	committed = true
	return nil
}

func syncPath(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
