package restore

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
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
	case "date":
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected date string, got %T", raw)
		}
		return s, nil
	default:
		return raw, nil
	}
}
