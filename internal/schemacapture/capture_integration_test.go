//go:build integration

package schemacapture

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/testutil/pgintegration"
)

func TestIntegrationCapture(t *testing.T) {
	db := pgintegration.Open(t)
	pgintegration.ApplyFixtures(t, db)

	outDir := t.TempDir()
	if err := Capture(context.Background(), os.Getenv(pgintegration.EnvDSN), outDir); err != nil {
		t.Fatal(err)
	}

	schema, err := os.ReadFile(filepath.Join(outDir, "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schema), "CREATE TABLE public.tbl_a") {
		t.Fatalf("captured schema missing public.tbl_a:\n%s", schema)
	}
}
