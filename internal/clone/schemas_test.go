package clone

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestSchemasFromOptionsPrefersDumpOpts(t *testing.T) {
	opts := Options{
		DumpOpts:    []dump.Option{dump.WithSchemas([]string{"app", "billing"})},
		RestoreOpts: []restore.Option{restore.WithSchemas([]string{"public"})},
	}
	got := SchemasFromOptions(opts)
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("got %v", got)
	}
}

func TestSchemasFromOptionsFallbackToRestoreOpts(t *testing.T) {
	opts := Options{
		RestoreOpts: []restore.Option{restore.WithSchemas([]string{"audit"})},
	}
	got := SchemasFromOptions(opts)
	if len(got) != 1 || got[0] != "audit" {
		t.Fatalf("got %v, want [audit]", got)
	}
}

func TestSchemasFromOptionsNilWhenNoSchemas(t *testing.T) {
	got := SchemasFromOptions(Options{})
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}
