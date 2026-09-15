package restore

import (
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
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

func TestCoerceTemporalValues(t *testing.T) {
	wantNaive := time.Date(2023, 6, 22, 13, 36, 35, 0, time.UTC)
	wantDate := time.Date(2023, 6, 22, 0, 0, 0, 0, time.UTC)
	wantAware := time.Date(2023, 6, 22, 13, 36, 35, 0, time.UTC)

	tests := []struct {
		name     string
		dataType string
		raw      any
		want     time.Time
	}{
		{"naive RFC3339 with Z from old dumps", "timestamp without time zone", "2023-06-22T13:36:35Z", wantNaive},
		{"naive ISO without offset", "timestamp without time zone", "2023-06-22T13:36:35", wantNaive},
		{"naive postgres text", "timestamp", "2023-06-22 13:36:35", wantNaive},
		{"timestamptz offset", "timestamp with time zone", "2023-06-22T09:36:35-04:00", wantAware},
		{"timestamptz alias", "timestamptz", "2023-06-22T13:36:35Z", wantAware},
		{"date RFC3339 with Z from old dumps", "date", "2023-06-22T00:00:00Z", wantDate},
		{"date only", "date", "2023-06-22", wantDate},
		{"already time.Time", "timestamp without time zone", wantNaive, wantNaive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coerceValue(tt.dataType, tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			tm, ok := got.(time.Time)
			if !ok {
				t.Fatalf("got %T %v, want time.Time", got, got)
			}
			if !tm.Equal(tt.want) {
				t.Fatalf("got %v, want %v", tm, tt.want)
			}
			if strings.Contains(tt.dataType, "without") || tt.dataType == "timestamp" || tt.dataType == "date" {
				if tm.Format("2006-01-02T15:04:05") != tt.want.Format("2006-01-02T15:04:05") {
					t.Fatalf("wall clock got %s, want %s", tm.Format(time.RFC3339Nano), tt.want.Format(time.RFC3339Nano))
				}
			}
		})
	}
}

func TestCoerceTemporalInvalid(t *testing.T) {
	if _, err := coerceValue("timestamp without time zone", "not-a-timestamp"); err == nil {
		t.Fatal("expected error")
	}
	err := mustCoerceError(t, "date", float64(1))
	if !strings.Contains(err.Error(), "date") {
		t.Fatalf("date type error = %q, want dataType in message", err)
	}
}

func TestCoerceTemporalInfinity(t *testing.T) {
	tests := []struct {
		dataType string
		raw      string
		mod      pgtype.InfinityModifier
		check    func(t *testing.T, got any, mod pgtype.InfinityModifier)
	}{
		{"date", "infinity", pgtype.Infinity, checkDateInfinity},
		{"date", "-infinity", pgtype.NegativeInfinity, checkDateInfinity},
		{"timestamp without time zone", "infinity", pgtype.Infinity, checkTimestampInfinity},
		{"timestamp", "-infinity", pgtype.NegativeInfinity, checkTimestampInfinity},
		{"timestamp with time zone", "infinity", pgtype.Infinity, checkTimestamptzInfinity},
		{"timestamptz", "-infinity", pgtype.NegativeInfinity, checkTimestamptzInfinity},
	}
	for _, tt := range tests {
		t.Run(tt.dataType+" "+tt.raw, func(t *testing.T) {
			got, err := coerceValue(tt.dataType, tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, got, tt.mod)
		})
	}
}

func mustCoerceError(t *testing.T, dataType string, raw any) error {
	t.Helper()
	_, err := coerceValue(dataType, raw)
	if err == nil {
		t.Fatal("expected error")
	}
	return err
}

func checkDateInfinity(t *testing.T, got any, mod pgtype.InfinityModifier) {
	t.Helper()
	d, ok := got.(pgtype.Date)
	if !ok || !d.Valid || d.InfinityModifier != mod {
		t.Fatalf("got %#v, want pgtype.Date infinity %d", got, mod)
	}
}

func checkTimestampInfinity(t *testing.T, got any, mod pgtype.InfinityModifier) {
	t.Helper()
	ts, ok := got.(pgtype.Timestamp)
	if !ok || !ts.Valid || ts.InfinityModifier != mod {
		t.Fatalf("got %#v, want pgtype.Timestamp infinity %d", got, mod)
	}
}

func checkTimestamptzInfinity(t *testing.T, got any, mod pgtype.InfinityModifier) {
	t.Helper()
	ts, ok := got.(pgtype.Timestamptz)
	if !ok || !ts.Valid || ts.InfinityModifier != mod {
		t.Fatalf("got %#v, want pgtype.Timestamptz infinity %d", got, mod)
	}
}
