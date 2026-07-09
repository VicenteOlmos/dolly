package restore

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestSchemasFromMetadataSingleSchema(t *testing.T) {
	meta := dump.Metadata{Schema: "app"}
	got := schemasFromMetadata(meta)
	if len(got) != 1 || got[0] != "app" {
		t.Fatalf("got %v, want [app]", got)
	}
}

func TestSchemasFromMetadataMulti(t *testing.T) {
	meta := dump.Metadata{
		Schema: "multi",
		Tables: []db.Table{
			{Schema: "app", Name: "t1"},
			{Schema: "billing", Name: "t2"},
			{Schema: "app", Name: "t3"},
		},
	}
	got := schemasFromMetadata(meta)
	if len(got) != 2 {
		t.Fatalf("got %v, want two schemas", got)
	}
}
