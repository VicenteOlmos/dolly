package clone

import "testing"

func TestSchemaReplayGapWarningsZeroCounts(t *testing.T) {
	if w := SchemaReplayGapWarnings(SchemaReplayGapCounts{}); len(w) != 0 {
		t.Fatalf("expected no warnings, got %v", w)
	}
}
