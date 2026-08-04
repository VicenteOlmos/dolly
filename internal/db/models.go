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
}
