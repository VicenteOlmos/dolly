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
