package restore

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestValidateSchemaMissingTable(t *testing.T) {
	meta := []db.Table{{Name: "missing", Columns: []db.Column{{Name: "id", DataType: "integer", OrdinalPosition: 1}}}}
	target := []db.Table{{Name: "other", Columns: []db.Column{{Name: "id", DataType: "integer", OrdinalPosition: 1}}}}
	err := validateSchema(meta, target)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateSchemaTypeMismatch(t *testing.T) {
	meta := []db.Table{{Name: "t", Columns: []db.Column{{Name: "id", DataType: "integer", OrdinalPosition: 1, PrimaryKey: true}}}}
	target := []db.Table{{Name: "t", Columns: []db.Column{{Name: "id", DataType: "text", OrdinalPosition: 1, PrimaryKey: true}}}}
	err := validateSchema(meta, target)
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateSchemaOK(t *testing.T) {
	col := db.Column{Name: "id", DataType: "integer", OrdinalPosition: 1, PrimaryKey: true, IsNullable: false}
	meta := []db.Table{{Name: "t", Columns: []db.Column{col}}}
	target := []db.Table{{Name: "t", Columns: []db.Column{col}}}
	if err := validateSchema(meta, target); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSchemaExtraTargetColumns(t *testing.T) {
	col := db.Column{Name: "id", DataType: "integer", OrdinalPosition: 1, PrimaryKey: true, IsNullable: false}
	extra := db.Column{Name: "legacy_note", DataType: "text", OrdinalPosition: 2, IsNullable: true}
	meta := []db.Table{{Name: "t", Columns: []db.Column{col}}}
	target := []db.Table{{Name: "t", Columns: []db.Column{col, extra}}}
	if err := validateSchema(meta, target); err != nil {
		t.Fatalf("extra target column should be allowed: %v", err)
	}
}

func TestValidateSchemaTargetMissingColumn(t *testing.T) {
	col := db.Column{Name: "id", DataType: "integer", OrdinalPosition: 1, PrimaryKey: true}
	meta := []db.Table{{Name: "t", Columns: []db.Column{col, {Name: "name", DataType: "text", OrdinalPosition: 2}}}}
	target := []db.Table{{Name: "t", Columns: []db.Column{col}}}
	err := validateSchema(meta, target)
	if err == nil || !strings.Contains(err.Error(), "fewer columns") {
		t.Fatalf("err = %v", err)
	}
}
