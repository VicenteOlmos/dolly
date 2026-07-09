package restore

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestCoerceRowInteger(t *testing.T) {
	cols := []db.Column{{Name: "id", DataType: "integer", IsNullable: false}}
	colNames := columnNames(cols)
	args, err := coerceRow(cols, colNames, map[string]any{"id": float64(42)})
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != int64(42) {
		t.Fatalf("got %T %v", args[0], args[0])
	}
}

func TestCoerceRowMissingColumn(t *testing.T) {
	cols := []db.Column{{Name: "id", DataType: "integer", IsNullable: false}}
	colNames := columnNames(cols)
	_, err := coerceRow(cols, colNames, map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCoerceRowUnknownKey(t *testing.T) {
	cols := []db.Column{{Name: "id", DataType: "integer", IsNullable: false}}
	colNames := columnNames(cols)
	_, err := coerceRow(cols, colNames, map[string]any{"id": float64(1), "extra": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}
