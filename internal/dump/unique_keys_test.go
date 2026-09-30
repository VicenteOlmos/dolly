package dump

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestWriteMetadataPersistsUniqueKeys(t *testing.T) {
	column := func(name string, position int, attnum int16) db.UniqueIndexColumn {
		return db.UniqueIndexColumn{Name: name, Position: position, Attnum: attnum, OpclassOID: 1978}
	}
	tables := []db.Table{{
		Schema: "public",
		Name:   "events",
		Columns: []db.Column{
			{Name: "code", DataType: "text", OrdinalPosition: 1},
			{Name: "note", DataType: "text", OrdinalPosition: 2},
		},
		UniqueIndexes: []db.UniqueIndexInfo{{
			IndexSchema: "public", IndexName: "events_code_key", IndexOID: 42,
			IsValid: true, IsReady: true, AccessMethod: "btree",
			KeyColumns: []db.UniqueIndexColumn{column("code", 1, 1)},
		}},
	}}
	path, err := writeMetadata(t.TempDir(), tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSONField(data, "unique_keys") {
		t.Fatalf("metadata missing unique_keys: %s", data)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Tables) != 1 || len(meta.Tables[0].UniqueKeys) != 1 {
		t.Fatalf("tables = %+v", meta.Tables)
	}
	if len(meta.Tables[0].UniqueKeys[0]) != 1 || meta.Tables[0].UniqueKeys[0][0] != "code" {
		t.Fatalf("unique_keys = %+v", meta.Tables[0].UniqueKeys)
	}
}

func containsJSONField(data []byte, field string) bool {
	return strings.Contains(string(data), `"`+field+`"`)
}
