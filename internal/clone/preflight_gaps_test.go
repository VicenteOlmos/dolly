package clone

import "testing"

func TestSchemaReplayGapWarningsZeroCounts(t *testing.T) {
	if w := SchemaReplayGapWarnings(SchemaReplayGapCounts{}); len(w) != 0 {
		t.Fatalf("expected no warnings, got %v", w)
	}
}

func TestSchemaReplayGapWarningsNonZero(t *testing.T) {
	w := SchemaReplayGapWarnings(SchemaReplayGapCounts{ForeignTables: 3})
	if len(w) != 1 {
		t.Fatalf("got %d warnings: %v", len(w), w)
	}
	if w[0] != "schema-replay will recreate 3 foreign table(s); their foreign servers must already exist on the target" {
		t.Fatalf("foreign table: %q", w[0])
	}
}

func TestSchemaReplayGapWarningsPartial(t *testing.T) {
	w := SchemaReplayGapWarnings(SchemaReplayGapCounts{ForeignTables: 1})
	if len(w) != 1 || w[0] != "schema-replay will recreate 1 foreign table(s); their foreign servers must already exist on the target" {
		t.Fatalf("got %v", w)
	}
}
