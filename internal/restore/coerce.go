package restore

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func coerceRow(columns []db.Column, colNames map[string]bool, row map[string]any) ([]any, error) {
	args := make([]any, len(columns))
	for i, col := range columns {
		raw, ok := row[col.Name]
		if !ok {
			if !col.IsNullable {
				return nil, fmt.Errorf("missing non-nullable column %q", col.Name)
			}
			args[i] = nil
			continue
		}
		if raw == nil {
			if !col.IsNullable {
				return nil, fmt.Errorf("null value for non-nullable column %q", col.Name)
			}
			args[i] = nil
			continue
		}

		v, err := coerceValue(col.DataType, raw)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", col.Name, err)
		}
		args[i] = v
	}

	for key := range row {
		if !colNames[key] {
			return nil, fmt.Errorf("unknown column %q in row", key)
		}
	}

	return args, nil
}

func columnNames(columns []db.Column) map[string]bool {
	m := make(map[string]bool, len(columns))
	for _, c := range columns {
		m[c.Name] = true
	}
	return m
}

func coerceValue(dataType string, raw any) (any, error) {
	switch strings.ToLower(dataType) {
	case "integer", "bigint", "smallint":
		switch n := raw.(type) {
		case float64:
			if math.Trunc(n) != n {
				return nil, fmt.Errorf("non-integer number %v", n)
			}
			return int64(n), nil
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				return nil, err
			}
			return i, nil
		case int64:
			return n, nil
		case int:
			return int64(n), nil
		default:
			return nil, fmt.Errorf("expected number, got %T", raw)
		}
	case "boolean":
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", raw)
		}
		return b, nil
	case "text", "character varying", "varchar":
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", raw)
		}
		return s, nil
	case "date", "timestamp without time zone", "timestamp", "timestamp with time zone", "timestamptz":
		return coerceTemporal(dataType, raw)
	default:
		return raw, nil
	}
}

func coerceTemporal(dataType string, raw any) (any, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		if inf, ok := temporalInfinity(dataType, v); ok {
			return inf, nil
		}
		t, err := parseTemporal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s value %q", dataType, v)
		}
		return t, nil
	default:
		return nil, fmt.Errorf("expected %s string, got %T", dataType, raw)
	}
}

func temporalInfinity(dataType, v string) (any, bool) {
	var mod pgtype.InfinityModifier
	switch v {
	case "infinity":
		mod = pgtype.Infinity
	case "-infinity":
		mod = pgtype.NegativeInfinity
	default:
		return nil, false
	}
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "date":
		return pgtype.Date{Valid: true, InfinityModifier: mod}, true
	case "timestamp with time zone", "timestamptz":
		return pgtype.Timestamptz{Valid: true, InfinityModifier: mod}, true
	default:
		return pgtype.Timestamp{Valid: true, InfinityModifier: mod}, true
	}
}

func parseTemporal(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp")
}
