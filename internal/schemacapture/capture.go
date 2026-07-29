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

// Capture runs pg_dump --schema-only on dsn and writes a sanitized schema.sql.
// When schemas is non-empty, each name is passed as a separate --schema argv pair.
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

	outPath := filepath.Join(outDir, "schema.sql")
	f, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create schema.sql: %w", err)
	}
	if err := os.Chmod(outPath, 0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(outPath)
		return fmt.Errorf("chmod schema.sql: %w", err)
	}

	if err := runCommand(ctx, "pg_dump", args, env, f); err != nil {
		_ = f.Close()
		_ = os.Remove(outPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("close schema.sql: %w", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("read schema.sql: %w", err)
	}
	sanitized, err := schemasql.Sanitize(raw)
	if err != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("sanitize schema.sql: %w", err)
	}
	if err := os.WriteFile(outPath, sanitized, 0o600); err != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("write schema.sql: %w", err)
	}
	return nil
}
