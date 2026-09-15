package dump

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
)

const (
	jsonDateLayout      = "2006-01-02"
	jsonTimestampLayout = "2006-01-02T15:04:05.999999999"
)

// marshalRow encodes a dump row as NDJSON. Timezone-naive date/timestamp
// values are written without a UTC offset so restore does not treat them as
// instants. Existing dumps that used RFC 3339 with Z remain readable.
func marshalRow(table db.Table, rowMap map[string]any) ([]byte, error) {
	encodeTemporalRow(table, rowMap)
	return json.Marshal(rowMap)
}

func encodeTemporalRow(table db.Table, rowMap map[string]any) {
	for _, col := range table.Columns {
		raw, ok := rowMap[col.Name]
		if !ok || raw == nil {
			continue
		}
		t, ok := raw.(time.Time)
		if !ok {
			continue
		}
		if formatted, ok := formatTemporal(col.DataType, t); ok {
			rowMap[col.Name] = formatted
		}
	}
}

func formatTemporal(dataType string, t time.Time) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "date":
		return t.Format(jsonDateLayout), true
	case "timestamp without time zone", "timestamp":
		return t.Format(jsonTimestampLayout), true
	case "timestamp with time zone", "timestamptz":
		return t.Format(time.RFC3339Nano), true
	default:
		return "", false
	}
}
