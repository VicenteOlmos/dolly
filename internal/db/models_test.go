package db

import (
	"encoding/json"
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
