package dump

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// KeyStrategy identifies how a table is streamed.
type KeyStrategy string

const (
	KeyStrategyPrimaryKey   KeyStrategy = "primary_key"
	KeyStrategyUniqueIndex  KeyStrategy = "unique_index"
	KeyStrategyNormalStream KeyStrategy = "normal_stream"
)

// KeyColumn records one ordered column in a resumable key.
type KeyColumn struct {
	Name         string `json:"name"`
	Position     int    `json:"position"`
	Attnum       int16  `json:"attnum"`
	OpclassOID   uint32 `json:"opclass_oid"`
	CollationOID uint32 `json:"collation_oid"`
	RawIndoption int16  `json:"raw_indoption"`
	NotNull      bool   `json:"not_null"`
}

// IndexIdentity records selected index identity and eligibility properties.
type IndexIdentity struct {
	Schema       string `json:"schema"`
	Name         string `json:"name"`
	OID          uint32 `json:"oid"`
	AccessMethod string `json:"access_method"`
	Primary      bool   `json:"primary"`
	Valid        bool   `json:"valid"`
	Ready        bool   `json:"ready"`
	HasPredicate bool   `json:"has_predicate"`
	IsExpression bool   `json:"is_expression"`
}

// KeyDescriptor is the deterministic streaming plan for one table.
type KeyDescriptor struct {
	Strategy    KeyStrategy    `json:"strategy"`
	TableSchema string         `json:"table_schema"`
	TableName   string         `json:"table_name"`
	Columns     []KeyColumn    `json:"columns,omitempty"`
	Index       *IndexIdentity `json:"index,omitempty"`
	Fingerprint string         `json:"fingerprint"`
	Resumable   bool           `json:"resumable"`
}

// ColumnNames returns key columns in catalog order for keyset SQL generation.
func (d KeyDescriptor) ColumnNames() []string {
	names := make([]string, len(d.Columns))
	for i := range d.Columns {
		names[i] = d.Columns[i].Name
	}
	return names
}

// SelectKeyDescriptor chooses PK, then the shortest safe unique key, or fallback.
func SelectKeyDescriptor(table db.Table) KeyDescriptor {
	if descriptor, ok := primaryKeyDescriptor(table); ok {
		return fingerprintDescriptor(descriptor)
	}

	candidates := make([]KeyDescriptor, 0, len(table.UniqueIndexes))
	for _, index := range table.UniqueIndexes {
		if descriptor, ok := uniqueKeyDescriptor(table, index); ok {
			candidates = append(candidates, descriptor)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if len(left.Columns) != len(right.Columns) {
			return len(left.Columns) < len(right.Columns)
		}
		if left.Index.Schema != right.Index.Schema {
			return left.Index.Schema < right.Index.Schema
		}
		if left.Index.Name != right.Index.Name {
			return left.Index.Name < right.Index.Name
		}
		return left.Index.OID < right.Index.OID
	})
	if len(candidates) > 0 {
		return fingerprintDescriptor(candidates[0])
	}
	return fingerprintDescriptor(KeyDescriptor{
		Strategy: KeyStrategyNormalStream, TableSchema: table.Schema, TableName: table.Name,
	})
}

func primaryKeyDescriptor(table db.Table) (KeyDescriptor, bool) {
	columns := make([]KeyColumn, 0)
	seen := make(map[string]struct{})
	for _, column := range table.Columns {
		if !column.PrimaryKey {
			continue
		}
		if column.Name == "" || column.IsNullable {
			return KeyDescriptor{}, false
		}
		if _, duplicate := seen[column.Name]; duplicate {
			return KeyDescriptor{}, false
		}
		seen[column.Name] = struct{}{}
		columns = append(columns, KeyColumn{
			Name: column.Name, Position: len(columns) + 1,
			Attnum: int16(column.OrdinalPosition), NotNull: true,
		})
	}
	if len(columns) == 0 {
		return KeyDescriptor{}, false
	}
	return KeyDescriptor{
		Strategy: KeyStrategyPrimaryKey, TableSchema: table.Schema, TableName: table.Name,
		Columns: columns, Resumable: true,
	}, true
}

func uniqueKeyDescriptor(table db.Table, index db.UniqueIndexInfo) (KeyDescriptor, bool) {
	if index.IndexSchema == "" || index.IndexName == "" || index.IndexOID == 0 ||
		index.IsPrimary || !index.IsValid || !index.IsReady || index.AccessMethod != "btree" ||
		index.HasPredicate || index.IsExpression || len(index.KeyColumns) == 0 {
		return KeyDescriptor{}, false
	}
	tableColumns := make(map[string]db.Column, len(table.Columns))
	for _, column := range table.Columns {
		tableColumns[column.Name] = column
	}
	columns := make([]KeyColumn, len(index.KeyColumns))
	seenNames := make(map[string]struct{}, len(columns))
	seenAttnums := make(map[int16]struct{}, len(columns))
	for i, column := range index.KeyColumns {
		tableColumn, exists := tableColumns[column.Name]
		if column.Name == "" || column.Position != i+1 || column.IsNullable ||
			column.Attnum <= 0 || column.OpclassOID == 0 || column.RawIndoption < 0 ||
			column.RawIndoption&^int16(3) != 0 || !exists || tableColumn.IsNullable ||
			int(column.Attnum) != tableColumn.OrdinalPosition {
			return KeyDescriptor{}, false
		}
		if _, duplicate := seenNames[column.Name]; duplicate {
			return KeyDescriptor{}, false
		}
		if _, duplicate := seenAttnums[column.Attnum]; duplicate {
			return KeyDescriptor{}, false
		}
		seenNames[column.Name] = struct{}{}
		seenAttnums[column.Attnum] = struct{}{}
		columns[i] = KeyColumn{
			Name: column.Name, Position: column.Position, Attnum: column.Attnum,
			OpclassOID: column.OpclassOID, CollationOID: column.CollationOID,
			RawIndoption: column.RawIndoption, NotNull: true,
		}
	}
	return KeyDescriptor{
		Strategy: KeyStrategyUniqueIndex, TableSchema: table.Schema, TableName: table.Name,
		Columns: columns, Resumable: true,
		Index: &IndexIdentity{
			Schema: index.IndexSchema, Name: index.IndexName, OID: index.IndexOID,
			AccessMethod: index.AccessMethod, Primary: index.IsPrimary,
			Valid: index.IsValid, Ready: index.IsReady,
			HasPredicate: index.HasPredicate, IsExpression: index.IsExpression,
		},
	}, true
}

// FingerprintKeyDescriptor computes canonical SHA-256 provenance for a descriptor.
func FingerprintKeyDescriptor(descriptor KeyDescriptor) string {
	descriptor.Fingerprint = ""
	payload, _ := json.Marshal(struct {
		Version int           `json:"version"`
		Key     KeyDescriptor `json:"key"`
	}{Version: 1, Key: descriptor})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func fingerprintDescriptor(descriptor KeyDescriptor) KeyDescriptor {
	descriptor.Fingerprint = FingerprintKeyDescriptor(descriptor)
	return descriptor
}
