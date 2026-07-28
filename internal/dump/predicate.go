package dump

import (
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5"
)

// PredicateOp is a supported row-predicate operator.
type PredicateOp string

const (
	PredicateEq     PredicateOp = "eq"
	PredicateIn     PredicateOp = "in"
	PredicateIsNull PredicateOp = "is_null"
)

// RowPredicate selects rows on one table column.
type RowPredicate struct {
	Table  string      `json:"table"`
	Column string      `json:"column"`
	Op     PredicateOp `json:"op"`
	Values []any       `json:"values,omitempty"`
	Value  any         `json:"value,omitempty"`
}

// SubsetLimits caps closure planning and streaming.
type SubsetLimits struct {
	MaxDepth        int `json:"max_depth"`
	MaxTables       int `json:"max_tables"`
	MaxRows         int `json:"max_rows"`
	MaxRowsPerTable int `json:"max_rows_per_table"`
	MaxInListSize   int `json:"max_in_list_size"`
}

// SubsetConfig enables subset dump mode.
type SubsetConfig struct {
	Seeds   []RowPredicate `json:"seeds"`
	Limits  SubsetLimits   `json:"limits"`
	Percent int            `json:"percent,omitempty"`
}

type compiledWhere struct {
	sql  string
	args []any
}

func normalizePredicate(p RowPredicate) RowPredicate {
	if p.Value != nil && len(p.Values) == 0 {
		p.Values = []any{p.Value}
	}
	return p
}

// DefaultSubsetLimits returns v1 default caps.
func DefaultSubsetLimits() SubsetLimits {
	return SubsetLimits{
		MaxDepth:      10,
		MaxTables:     50,
		MaxRows:       100000,
		MaxInListSize: 500,
	}
}

// ApplySubsetLimitDefaults fills zero limit fields with defaults.
func ApplySubsetLimitDefaults(l SubsetLimits) SubsetLimits {
	d := DefaultSubsetLimits()
	if l.MaxDepth <= 0 {
		l.MaxDepth = d.MaxDepth
	}
	if l.MaxTables <= 0 {
		l.MaxTables = d.MaxTables
	}
	if l.MaxRows <= 0 {
		l.MaxRows = d.MaxRows
	}
	if l.MaxInListSize <= 0 {
		l.MaxInListSize = d.MaxInListSize
	}
	return l
}

// ValidateSeeds checks seeds against schema metadata before I/O.
func ValidateSeeds(seeds []RowPredicate, tables []db.Table) error {
	if len(seeds) == 0 {
		return fmt.Errorf("subset: at least one seed predicate is required")
	}
	byName := make(map[string]db.Table, len(tables))
	for _, t := range tables {
		byName[t.Name] = t
	}
	for i, raw := range seeds {
		p := normalizePredicate(raw)
		if p.Table == "" {
			return fmt.Errorf("subset: seed %d: missing table", i)
		}
		tbl, ok := byName[p.Table]
		if !ok {
			return fmt.Errorf("subset: seed %d: unknown table %q", i, p.Table)
		}
		if _, err := primaryKeyColumn(tbl); err != nil {
			return fmt.Errorf("subset: seed %d: %w", i, err)
		}
		if p.Column == "" {
			return fmt.Errorf("subset: seed %d: missing column", i)
		}
		col, ok := findColumn(tbl, p.Column)
		if !ok {
			return fmt.Errorf("subset: seed %d: unknown column %q on table %q", i, p.Column, p.Table)
		}
		switch p.Op {
		case PredicateEq:
			if len(p.Values) != 1 {
				return fmt.Errorf("subset: seed %d: eq requires exactly one value", i)
			}
			if err := validateLiteralAgainstType(p.Values[0], col.DataType); err != nil {
				return fmt.Errorf("subset: seed %d: value type mismatch for column %q: %w", i, p.Column, err)
			}
		case PredicateIn:
			if len(p.Values) == 0 {
				return fmt.Errorf("subset: seed %d: in requires at least one value", i)
			}
			for _, v := range p.Values {
				if err := validateLiteralAgainstType(v, col.DataType); err != nil {
					return fmt.Errorf("subset: seed %d: value type mismatch for column %q: %w", i, p.Column, err)
				}
			}
		case PredicateIsNull:
			if len(p.Values) != 0 {
				return fmt.Errorf("subset: seed %d: is_null must not include values", i)
			}
		default:
			return fmt.Errorf("subset: seed %d: unsupported operator %q", i, p.Op)
		}
	}
	return nil
}

func findColumn(t db.Table, name string) (db.Column, bool) {
	for _, c := range t.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return db.Column{}, false
}

func validateLiteralAgainstType(value any, dataType string) error {
	dt := strings.ToLower(dataType)
	switch dt {
	case "integer", "bigint", "smallint", "int", "int2", "int4", "int8":
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return nil
		}
		return fmt.Errorf("expected integer value, got %T", value)
	case "text", "character varying", "varchar", "character", "char", "bpchar":
		if _, ok := value.(string); ok {
			return nil
		}
		return fmt.Errorf("expected string value, got %T", value)
	case "boolean", "bool":
		if _, ok := value.(bool); ok {
			return nil
		}
		return fmt.Errorf("expected bool value, got %T", value)
	case "uuid":
		if _, ok := value.(string); ok {
			return nil
		}
		return fmt.Errorf("expected string value for uuid, got %T", value)
	case "numeric", "decimal":
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return nil
		}
		return fmt.Errorf("expected numeric value, got %T", value)
	case "timestamp without time zone", "timestamp with time zone", "timestamp", "timestamptz", "date":
		if _, ok := value.(string); ok {
			return nil
		}
		return fmt.Errorf("expected string value for %s, got %T", dataType, value)
	default:
		return nil
	}
}

func primaryKeyColumn(t db.Table) (string, error) {
	var pk []string
	for _, c := range t.Columns {
		if c.PrimaryKey {
			pk = append(pk, c.Name)
		}
	}
	if len(pk) == 0 {
		return "", fmt.Errorf("table %q has no primary key", t.Name)
	}
	if len(pk) > 1 {
		return "", fmt.Errorf("table %q has composite primary key (single-column PK required)", t.Name)
	}
	return pk[0], nil
}

func primaryKeysColumns(t db.Table) ([]string, error) {
	var pk []string
	for _, c := range t.Columns {
		if c.PrimaryKey {
			pk = append(pk, c.Name)
		}
	}
	if len(pk) == 0 {
		return nil, fmt.Errorf("table %q has no primary key", t.Name)
	}
	return pk, nil
}

func compilePredicate(p RowPredicate) (compiledWhere, error) {
	p = normalizePredicate(p)
	col := pgx.Identifier{p.Column}.Sanitize()
	switch p.Op {
	case PredicateEq:
		return compiledWhere{sql: fmt.Sprintf("(%s = $1)", col), args: []any{p.Values[0]}}, nil
	case PredicateIn:
		return compiledWhere{sql: fmt.Sprintf("(%s = ANY($1))", col), args: []any{toDriverArrayArg(p.Values)}}, nil
	case PredicateIsNull:
		return compiledWhere{sql: fmt.Sprintf("(%s IS NULL)", col), args: nil}, nil
	default:
		return compiledWhere{}, fmt.Errorf("unsupported operator %q", p.Op)
	}
}

func compilePKInList(pkCol string, values []any) compiledWhere {
	col := pgx.Identifier{pkCol}.Sanitize()
	return compiledWhere{
		sql:  fmt.Sprintf("(%s = ANY($1))", col),
		args: []any{toDriverArrayArg(values)},
	}
}

func toDriverArrayArg(values []any) any {
	if len(values) == 0 {
		return []any{}
	}
	return values
}
