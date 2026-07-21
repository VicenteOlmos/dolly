package restore

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestTrustedSchemaSQLIsExplicit(t *testing.T) {
	var cfg config
	if cfg.schemaSQL {
		t.Fatal("schema SQL must default off")
	}
	WithTrustedSchemaSQL()(&cfg)
	if !cfg.schemaSQL {
		t.Fatal("trusted schema option did not enable replay")
	}
	if !cfg.withoutTransaction {
		t.Fatal("trusted schema option must disable transactions")
	}
}

func TestValidateSchemaKeepsSameNamedSchemasDistinct(t *testing.T) {
	col := db.Column{Name: "id", DataType: "integer", OrdinalPosition: 1}
	meta := []db.Table{{Schema: "audit", Name: "events", Columns: []db.Column{col}}}
	target := []db.Table{{Schema: "public", Name: "events", Columns: []db.Column{col}}}
	if err := validateSchema(meta, target); err == nil {
		t.Fatal("validation accepted table from different schema")
	}
}
