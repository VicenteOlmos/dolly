package clone

import (
	"testing"
)

func TestFormatRowCountMismatch(t *testing.T) {
	got := formatRowCountMismatch("public", "orders", 10, 8)
	want := "verify: public.orders row count source=10 target=8"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSequenceLastValueMismatch(t *testing.T) {
	got := formatSequenceLastValueMismatch("public", "orders_id_seq", 5, 1)
	want := "verify: public.orders_id_seq last_value source=5 target=1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestVerifyGapWarnings(t *testing.T) {
	w := verifyGapWarnings(SchemaReplayGapCounts{HypotheticalAggregates: 2, UserOperatorClasses: 1})
	if len(w) != 2 {
		t.Fatalf("warnings = %d, want 2", len(w))
	}
	if w[0] != "verify: schema-replay did not copy 2 hypothetical aggregate(s); pg_dump is required for those objects" {
		t.Fatalf("unexpected hypothetical warning: %q", w[0])
	}
}
