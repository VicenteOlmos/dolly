package dump

import (
	"strings"
	"testing"
	"time"
)

func TestFormatTemporal(t *testing.T) {
	naive := time.Date(2023, 6, 22, 13, 36, 35, 0, time.UTC)
	aware := time.Date(2023, 6, 22, 9, 36, 35, 0, time.FixedZone("EDT", -4*3600))
	day := time.Date(2023, 6, 22, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		dataType string
		value    time.Time
		want     string
		naive    bool
	}{
		{"timestamp without time zone", naive, "2023-06-22T13:36:35", true},
		{"timestamp", naive, "2023-06-22T13:36:35", true},
		{"date", day, "2023-06-22", true},
		{"timestamp with time zone", aware, "2023-06-22T09:36:35-04:00", false},
		{"timestamptz", aware, "2023-06-22T09:36:35-04:00", false},
	}
	for _, tt := range tests {
		got, ok := formatTemporal(tt.dataType, tt.value)
		if !ok {
			t.Fatalf("%s: formatTemporal returned false", tt.dataType)
		}
		if got != tt.want {
			t.Fatalf("%s: got %q, want %q", tt.dataType, got, tt.want)
		}
		if tt.naive && (strings.Contains(got, "Z") || strings.Contains(got, "+") || strings.Contains(got[10:], "-")) {
			t.Fatalf("%s: timezone-naive value %q asserted an offset", tt.dataType, got)
		}
	}
	if _, ok := formatTemporal("text", naive); ok {
		t.Fatal("text column should not be formatted as temporal")
	}
}
