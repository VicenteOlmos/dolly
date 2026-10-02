package clone

import "testing"

func TestSchemaReplayGapWarningsZeroCounts(t *testing.T) {
	if w := SchemaReplayGapWarnings(SchemaReplayGapCounts{}); len(w) != 0 {
		t.Fatalf("expected no warnings, got %v", w)
	}
}

func TestSchemaReplayGapWarningsNonZero(t *testing.T) {
	w := SchemaReplayGapWarnings(SchemaReplayGapCounts{
		HypotheticalAggregates: 2,
		ForeignTables:          3,
	})
	if len(w) != 2 {
		t.Fatalf("got %d warnings: %v", len(w), w)
	}
	if w[0] != "schema-replay will not copy 2 hypothetical aggregate(s); pg_dump is required for those objects" {
		t.Fatalf("hypothetical: %q", w[0])
	}
	if w[1] != "schema-replay will not copy 3 foreign table(s); pg_dump is required for those objects" {
		t.Fatalf("foreign table: %q", w[1])
	}
}

func TestSchemaReplayGapWarningsPartial(t *testing.T) {
	w := SchemaReplayGapWarnings(SchemaReplayGapCounts{ForeignTables: 1})
	if len(w) != 1 || w[0] != "schema-replay will not copy 1 foreign table(s); pg_dump is required for those objects" {
		t.Fatalf("got %v", w)
	}
}
