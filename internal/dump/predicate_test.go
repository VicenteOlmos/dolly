package dump

import (
	"fmt"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func fixtureTables() []db.Table {
	return []db.Table{
		{
			Schema: "public",
			Name:   "departments",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "name", DataType: "text", OrdinalPosition: 2},
			},
		},
		{
			Schema: "public",
			Name:   "tbl_a",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "department_id", DataType: "integer", OrdinalPosition: 2},
				{Name: "name", DataType: "text", OrdinalPosition: 3},
			},
			ForeignKeys: []db.ForeignKey{
				{
					ConstraintName:        "tbl_a_department_id_fkey",
					ColumnName:            "department_id",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "departments",
					ReferencedColumnName:  "id",
				},
			},
		},
	}
}

func TestValidateSeeds(t *testing.T) {
	tables := fixtureTables()

	tests := []struct {
		name    string
		seeds   []RowPredicate
		wantErr string
	}{
		{
			name: "valid eq",
			seeds: []RowPredicate{
				{Table: "tbl_a", Column: "id", Op: PredicateEq, Value: 1},
			},
		},
		{
			name: "valid in",
			seeds: []RowPredicate{
				{Table: "tbl_a", Column: "id", Op: PredicateIn, Values: []any{1, 2}},
			},
		},
		{
			name: "valid is_null",
			seeds: []RowPredicate{
				{Table: "tbl_a", Column: "name", Op: PredicateIsNull},
			},
		},
		{
			name:    "unknown table",
			seeds:   []RowPredicate{{Table: "missing", Column: "id", Op: PredicateEq, Value: 1}},
			wantErr: "unknown table",
		},
		{
			name:    "unknown column",
			seeds:   []RowPredicate{{Table: "tbl_a", Column: "nope", Op: PredicateEq, Value: 1}},
			wantErr: "unknown column",
		},
		{
			name:    "unsupported op",
			seeds:   []RowPredicate{{Table: "tbl_a", Column: "name", Op: "like", Value: "x"}},
			wantErr: "unsupported operator",
		},
		{
			name:    "eq wrong arity",
			seeds:   []RowPredicate{{Table: "tbl_a", Column: "id", Op: PredicateEq, Values: []any{1, 2}}},
			wantErr: "eq requires exactly one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSeeds(tt.seeds, tables)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSeeds() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSeeds() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSeedsTyped(t *testing.T) {
	tables := []db.Table{
		{
			Name: "orders",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true},
				{Name: "code", DataType: "text"},
				{Name: "active", DataType: "boolean"},
				{Name: "uuid_col", DataType: "uuid"},
				{Name: "amount", DataType: "numeric"},
				{Name: "created_at", DataType: "timestamp without time zone"},
			},
		},
	}

	tests := []struct {
		name    string
		seeds   []RowPredicate
		wantErr string
	}{
		{
			name:  "valid integer",
			seeds: []RowPredicate{{Table: "orders", Column: "id", Op: PredicateEq, Value: 1}},
		},
		{
			name:    "invalid integer string",
			seeds:   []RowPredicate{{Table: "orders", Column: "id", Op: PredicateEq, Value: "not-an-int"}},
			wantErr: "expected integer value",
		},
		{
			name:  "valid text",
			seeds: []RowPredicate{{Table: "orders", Column: "code", Op: PredicateEq, Value: "ABC"}},
		},
		{
			name:    "invalid text number",
			seeds:   []RowPredicate{{Table: "orders", Column: "code", Op: PredicateEq, Value: 123}},
			wantErr: "expected string value",
		},
		{
			name:  "valid bool",
			seeds: []RowPredicate{{Table: "orders", Column: "active", Op: PredicateEq, Value: true}},
		},
		{
			name:    "invalid bool string",
			seeds:   []RowPredicate{{Table: "orders", Column: "active", Op: PredicateEq, Value: "true"}},
			wantErr: "expected bool value",
		},
		{
			name:  "valid uuid",
			seeds: []RowPredicate{{Table: "orders", Column: "uuid_col", Op: PredicateEq, Value: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"}},
		},
		{
			name:    "invalid uuid number",
			seeds:   []RowPredicate{{Table: "orders", Column: "uuid_col", Op: PredicateEq, Value: 123}},
			wantErr: "expected string value for uuid",
		},
		{
			name:  "valid numeric",
			seeds: []RowPredicate{{Table: "orders", Column: "amount", Op: PredicateEq, Value: 99.99}},
		},
		{
			name:  "valid timestamp string",
			seeds: []RowPredicate{{Table: "orders", Column: "created_at", Op: PredicateEq, Value: "2024-01-01T00:00:00Z"}},
		},
		{
			name:    "invalid timestamp number",
			seeds:   []RowPredicate{{Table: "orders", Column: "created_at", Op: PredicateEq, Value: 1704067200}},
			wantErr: "expected string value",
		},
		{
			name:  "valid in mixed types",
			seeds: []RowPredicate{{Table: "orders", Column: "id", Op: PredicateIn, Values: []any{1, 2, 3}}},
		},
		{
			name:    "invalid in mixed types",
			seeds:   []RowPredicate{{Table: "orders", Column: "id", Op: PredicateIn, Values: []any{1, "two"}}},
			wantErr: "expected integer value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSeeds(tt.seeds, tables)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSeeds() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSeeds() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestCompilePredicateUsesBoundArgs(t *testing.T) {
	tests := []struct {
		name     string
		pred     RowPredicate
		wantSQL  string
		wantArgs int
	}{
		{
			name:     "eq",
			pred:     RowPredicate{Column: "id", Op: PredicateEq, Values: []any{42}},
			wantSQL:  `("id" = $1)`,
			wantArgs: 1,
		},
		{
			name:     "injection literal in value",
			pred:     RowPredicate{Column: "name", Op: PredicateEq, Values: []any{"'; DROP TABLE tbl_a; --"}},
			wantSQL:  `("name" = $1)`,
			wantArgs: 1,
		},
		{
			name:     "in",
			pred:     RowPredicate{Column: "id", Op: PredicateIn, Values: []any{1, 2}},
			wantSQL:  `("id" = ANY($1))`,
			wantArgs: 1,
		},
		{
			name:     "is_null",
			pred:     RowPredicate{Column: "nickname", Op: PredicateIsNull},
			wantSQL:  `("nickname" IS NULL)`,
			wantArgs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw, err := compilePredicate(tt.pred)
			if err != nil {
				t.Fatal(err)
			}
			if cw.sql != tt.wantSQL {
				t.Fatalf("sql = %q, want %q", cw.sql, tt.wantSQL)
			}
			if len(cw.args) != tt.wantArgs {
				t.Fatalf("args len = %d, want %d", len(cw.args), tt.wantArgs)
			}
			if tt.wantArgs > 0 && strings.Contains(fmt.Sprint(cw.args[0]), "DROP TABLE") {
				if !strings.Contains(cw.sql, "$1") {
					t.Fatal("literal must not appear in SQL text")
				}
			}
		})
	}
}
