package db

// Column represents a database column.
type Column struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type"`
	IsNullable      bool   `json:"is_nullable"`
	PrimaryKey      bool   `json:"primary_key"`
	OrdinalPosition int    `json:"ordinal_position"`
}

// ForeignKey represents a foreign-key constraint.
type ForeignKey struct {
	ConstraintName        string `json:"constraint_name"`
	ColumnName            string `json:"column_name"`
	ReferencedTableSchema string `json:"referenced_table_schema"`
	ReferencedTableName   string `json:"referenced_table_name"`
	ReferencedColumnName  string `json:"referenced_column_name"`
}

// Table represents a database table with its columns and foreign keys.
type Table struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	RowCount    *int64       `json:"row_count,omitempty"`
	Columns     []Column     `json:"columns"`
	ForeignKeys []ForeignKey `json:"foreign_keys"`
}
