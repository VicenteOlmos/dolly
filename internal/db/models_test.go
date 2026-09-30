package db

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestColumnIdentityJSONRoundTrip(t *testing.T) {
	withIdentity := Column{Name: "id", Identity: "ALWAYS"}
	raw, err := json.Marshal(withIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"identity":"ALWAYS"`) {
		t.Fatalf("marshal = %s", raw)
	}
	var withDecoded Column
	if err := json.Unmarshal(raw, &withDecoded); err != nil {
		t.Fatal(err)
	}
	if withDecoded.Identity != "ALWAYS" {
		t.Fatalf("decoded = %+v", withDecoded)
	}

	legacy := `{"name":"id","data_type":"integer","is_nullable":false,"primary_key":true,"ordinal_position":1}`
	var decoded Column
	if err := json.Unmarshal([]byte(legacy), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Identity != "" {
		t.Fatalf("legacy dump column should decode empty identity, got %q", decoded.Identity)
	}
}

func TestDataColumnsKeepsIdentityDropsGenerated(t *testing.T) {
	cols := []Column{
		{Name: "id", Identity: "ALWAYS"},
		{Name: "total", Generated: true},
		{Name: "note"},
	}
	data := DataColumns(cols)
	if len(data) != 2 || data[0].Name != "id" || data[1].Name != "note" {
		t.Fatalf("data columns = %+v", data)
	}
	if HasGenerated(data) {
		t.Fatal("HasGenerated should be false without generated columns")
	}
	if !HasGenerated(cols) {
		t.Fatal("HasGenerated should be true when generated column present")
	}
}

func TestTableUniqueKeysJSONRoundTrip(t *testing.T) {
	table := Table{
		Schema: "public",
		Name:   "events",
		Columns: []Column{
			{Name: "code", DataType: "text", OrdinalPosition: 1},
		},
		UniqueKeys: [][]string{{"code"}, {"tenant", "seq"}},
	}
	data, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Table
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.UniqueKeys, table.UniqueKeys) {
		t.Fatalf("unique_keys = %+v, want %+v", decoded.UniqueKeys, table.UniqueKeys)
	}
}

func TestTableWithoutUniqueKeysDecodesNil(t *testing.T) {
	const payload = `{"schema":"public","name":"events","columns":[{"name":"id","data_type":"integer","is_nullable":false,"primary_key":true,"ordinal_position":1}]}`
	var table Table
	if err := json.Unmarshal([]byte(payload), &table); err != nil {
		t.Fatal(err)
	}
	if table.UniqueKeys != nil {
		t.Fatalf("unique_keys = %+v, want nil", table.UniqueKeys)
	}
}
