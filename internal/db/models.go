package db

// Column represents a database column.
type Column struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type"`
	IsNullable      bool   `json:"is_nullable"`
	PrimaryKey      bool   `json:"primary_key"`
	OrdinalPosition int    `json:"ordinal_position"`
	// Generated is true for GENERATED ALWAYS columns. Identity columns stay false
	// so dumped IDs are preserved.
	Generated bool `json:"generated,omitempty"`
}

// ForeignKey represents a foreign-key constraint.
type ForeignKey struct {
	ConstraintName        string `json:"constraint_name"`
	ColumnName            string `json:"column_name"`
	ReferencedTableSchema string `json:"referenced_table_schema"`
	ReferencedTableName   string `json:"referenced_table_name"`
	ReferencedColumnName  string `json:"referenced_column_name"`
}

type UniqueIndexColumn struct {
	Name         string `json:"-"`
	Position     int    `json:"-"`
	IsNullable   bool   `json:"-"`
	Attnum       int16  `json:"-"`
	OpclassOID   uint32 `json:"-"`
	CollationOID uint32 `json:"-"`
	RawIndoption int16  `json:"-"`
}

type UniqueIndexInfo struct {
	IndexName    string              `json:"-"`
	IndexSchema  string              `json:"-"`
	IndexOID     uint32              `json:"-"`
	IsPrimary    bool                `json:"-"`
	IsValid      bool                `json:"-"`
	IsReady      bool                `json:"-"`
	AccessMethod string              `json:"-"`
	HasPredicate bool                `json:"-"`
	IsExpression bool                `json:"-"`
	KeyColumns   []UniqueIndexColumn `json:"-"`
}

// Table represents a database table with its columns and foreign keys.
type Table struct {
	Schema        string            `json:"schema"`
	Name          string            `json:"name"`
	DataFile      *string           `json:"data_file,omitempty"`
	RowCount      *int64            `json:"row_count,omitempty"`
	Columns       []Column          `json:"columns"`
	ForeignKeys   []ForeignKey      `json:"foreign_keys"`
	UniqueIndexes []UniqueIndexInfo `json:"-"`
	// RelKind is pg_class.relkind ("r" ordinary, "p" partitioned parent).
	RelKind string `json:"relkind,omitempty"`
	// PartitionOf is schema.table of the partitioned parent when this row is a partition.
	PartitionOf string `json:"partition_of,omitempty"`
	// PartitionBound is pg_get_expr(relpartbound) (FOR VALUES … or DEFAULT).
	PartitionBound string `json:"partition_bound,omitempty"`
	// PartitionBy is pg_get_partkeydef (for example RANGE (id)) on a partitioned parent.
	PartitionBy string `json:"partition_by,omitempty"`
}

// WithoutPartitionParents drops partitioned parents. Selecting a parent returns
// every child row, so dump and clone copy only the leaves.
func WithoutPartitionParents(tables []Table) []Table {
	out := make([]Table, 0, len(tables))
	for _, table := range tables {
		if table.RelKind == "p" {
			continue
		}
		out = append(out, table)
	}
	return out
}

// DataColumns returns columns that can be written. GENERATED ALWAYS values are
// computed on the destination.
func DataColumns(cols []Column) []Column {
	out := make([]Column, 0, len(cols))
	for _, col := range cols {
		if col.Generated {
			continue
		}
		out = append(out, col)
	}
	return out
}

// HasGenerated reports whether any column is GENERATED ALWAYS.
func HasGenerated(cols []Column) bool {
	for _, col := range cols {
		if col.Generated {
			return true
		}
	}
	return false
}
