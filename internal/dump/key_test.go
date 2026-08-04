package dump

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestKeySelection(t *testing.T) {
	safe := func(name string, oid uint32, columns ...db.UniqueIndexColumn) db.UniqueIndexInfo {
		return db.UniqueIndexInfo{
			IndexSchema: "public", IndexName: name, IndexOID: oid,
			IsValid: true, IsReady: true, AccessMethod: "btree", KeyColumns: columns,
		}
	}
	column := func(name string, position int, attnum int16) db.UniqueIndexColumn {
		return db.UniqueIndexColumn{Name: name, Position: position, Attnum: attnum, OpclassOID: 1978}
	}
	tests := []struct {
		name      string
		table     db.Table
		strategy  KeyStrategy
		columns   []string
		indexName string
		indexOID  uint32
		resumable bool
	}{
		{
			name: "primary key wins", strategy: KeyStrategyPrimaryKey, columns: []string{"id"}, resumable: true,
			table: db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "id", PrimaryKey: true, OrdinalPosition: 1}, {Name: "code", OrdinalPosition: 2}},
				UniqueIndexes: []db.UniqueIndexInfo{safe("events_code_key", 20, column("code", 1, 2))}},
		},
		{
			name: "simple unique", strategy: KeyStrategyUniqueIndex, columns: []string{"code"}, indexName: "events_code_key", resumable: true,
			table: db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", OrdinalPosition: 1}},
				UniqueIndexes: []db.UniqueIndexInfo{safe("events_code_key", 20, column("code", 1, 1))}},
		},
		{
			name: "composite unique preserves order", strategy: KeyStrategyUniqueIndex, columns: []string{"tenant", "sequence"}, indexName: "events_tenant_sequence_key", resumable: true,
			table: db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "tenant", OrdinalPosition: 1}, {Name: "sequence", OrdinalPosition: 2}},
				UniqueIndexes: []db.UniqueIndexInfo{safe("events_tenant_sequence_key", 20, column("tenant", 1, 1), column("sequence", 2, 2))}},
		},
		{
			name: "shortest unique wins", strategy: KeyStrategyUniqueIndex, columns: []string{"code"}, indexName: "events_code_key", resumable: true,
			table: db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", OrdinalPosition: 1}, {Name: "tenant", OrdinalPosition: 2}},
				UniqueIndexes: []db.UniqueIndexInfo{
					safe("events_composite_key", 10, column("tenant", 1, 2), column("code", 2, 1)),
					safe("events_code_key", 30, column("code", 1, 1)),
				}},
		},
		{
			name: "qualified name breaks tie before OID", strategy: KeyStrategyUniqueIndex, columns: []string{"code"}, indexName: "a_key", resumable: true,
			table: db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", OrdinalPosition: 1}},
				UniqueIndexes: []db.UniqueIndexInfo{safe("z_key", 1, column("code", 1, 1)), safe("a_key", 99, column("code", 1, 1))}},
		},
		{
			name: "OID breaks identical name tie", strategy: KeyStrategyUniqueIndex, columns: []string{"code"}, indexName: "same_key", indexOID: 4, resumable: true,
			table: db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", OrdinalPosition: 1}},
				UniqueIndexes: []db.UniqueIndexInfo{safe("same_key", 9, column("code", 1, 1)), safe("same_key", 4, column("code", 1, 1))}},
		},
		{
			name: "no safe key falls back", strategy: KeyStrategyNormalStream,
			table: db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", IsNullable: true, OrdinalPosition: 1}},
				UniqueIndexes: []db.UniqueIndexInfo{safe("events_code_key", 20, column("code", 1, 1))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectKeyDescriptor(tt.table)
			if got.Strategy != tt.strategy || got.Resumable != tt.resumable {
				t.Fatalf("strategy/resumable = %q/%v, want %q/%v", got.Strategy, got.Resumable, tt.strategy, tt.resumable)
			}
			if !equalStrings(got.ColumnNames(), tt.columns) {
				t.Fatalf("columns = %v, want %v", got.ColumnNames(), tt.columns)
			}
			if tt.indexName != "" && (got.Index == nil || got.Index.Name != tt.indexName) {
				t.Fatalf("index = %#v, want %q", got.Index, tt.indexName)
			}
			if tt.indexOID != 0 && got.Index.OID != tt.indexOID {
				t.Fatalf("index OID = %d, want %d", got.Index.OID, tt.indexOID)
			}
			if len(got.Fingerprint) != 64 {
				t.Fatalf("fingerprint length = %d, want 64", len(got.Fingerprint))
			}
		})
	}
}

func TestKeyRejectsUnsafeUniqueIndexes(t *testing.T) {
	base := db.UniqueIndexInfo{
		IndexSchema: "public", IndexName: "events_code_key", IndexOID: 10,
		IsValid: true, IsReady: true, AccessMethod: "btree",
		KeyColumns: []db.UniqueIndexColumn{{Name: "code", Position: 1, Attnum: 1, OpclassOID: 1978}},
	}
	tests := []struct {
		name   string
		mutate func(*db.UniqueIndexInfo)
	}{
		{"invalid", func(i *db.UniqueIndexInfo) { i.IsValid = false }},
		{"unready", func(i *db.UniqueIndexInfo) { i.IsReady = false }},
		{"non-btree", func(i *db.UniqueIndexInfo) { i.AccessMethod = "hash" }},
		{"partial", func(i *db.UniqueIndexInfo) { i.HasPredicate = true }},
		{"expression", func(i *db.UniqueIndexInfo) { i.IsExpression = true }},
		{"primary index duplicate", func(i *db.UniqueIndexInfo) { i.IsPrimary = true }},
		{"missing identity", func(i *db.UniqueIndexInfo) { i.IndexOID = 0 }},
		{"missing columns", func(i *db.UniqueIndexInfo) { i.KeyColumns = nil }},
		{"nullable", func(i *db.UniqueIndexInfo) { i.KeyColumns[0].IsNullable = true }},
		{"unnamed", func(i *db.UniqueIndexInfo) { i.KeyColumns[0].Name = "" }},
		{"unordered", func(i *db.UniqueIndexInfo) { i.KeyColumns[0].Position = 2 }},
		{"expression attnum", func(i *db.UniqueIndexInfo) { i.KeyColumns[0].Attnum = 0 }},
		{"attnum does not match table column", func(i *db.UniqueIndexInfo) { i.KeyColumns[0].Attnum = 2 }},
		{"missing opclass", func(i *db.UniqueIndexInfo) { i.KeyColumns[0].OpclassOID = 0 }},
		{"unknown sort flags", func(i *db.UniqueIndexInfo) { i.KeyColumns[0].RawIndoption = 4 }},
		{"duplicate columns", func(i *db.UniqueIndexInfo) {
			i.KeyColumns = append(i.KeyColumns, db.UniqueIndexColumn{Name: "code", Position: 2, Attnum: 1, OpclassOID: 1978})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := base
			index.KeyColumns = append([]db.UniqueIndexColumn(nil), base.KeyColumns...)
			tt.mutate(&index)
			table := db.Table{Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", OrdinalPosition: 1}}, UniqueIndexes: []db.UniqueIndexInfo{index}}
			if got := SelectKeyDescriptor(table); got.Strategy != KeyStrategyNormalStream {
				t.Fatalf("strategy = %q, want fallback", got.Strategy)
			}
		})
	}
}

func TestKeyFingerprint(t *testing.T) {
	base := SelectKeyDescriptor(db.Table{
		Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", OrdinalPosition: 1}},
		UniqueIndexes: []db.UniqueIndexInfo{{
			IndexSchema: "public", IndexName: "events_code_key", IndexOID: 10,
			IsValid: true, IsReady: true, AccessMethod: "btree",
			KeyColumns: []db.UniqueIndexColumn{{Name: "code", Position: 1, Attnum: 1, OpclassOID: 1978}},
		}},
	})
	if got := FingerprintKeyDescriptor(base); got != base.Fingerprint {
		t.Fatalf("stable fingerprint = %q, want %q", got, base.Fingerprint)
	}
	mutations := []struct {
		name   string
		mutate func(*KeyDescriptor)
	}{
		{"kind", func(d *KeyDescriptor) { d.Strategy = KeyStrategyPrimaryKey }},
		{"table", func(d *KeyDescriptor) { d.TableName = "other" }},
		{"index OID", func(d *KeyDescriptor) { d.Index.OID++ }},
		{"index name", func(d *KeyDescriptor) { d.Index.Name = "other_key" }},
		{"column order", func(d *KeyDescriptor) { d.Columns[0].Position++ }},
		{"column name", func(d *KeyDescriptor) { d.Columns[0].Name = "other" }},
		{"sort metadata", func(d *KeyDescriptor) { d.Columns[0].RawIndoption = 1 }},
		{"safety metadata", func(d *KeyDescriptor) { d.Index.Ready = false }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			changed := base
			index := *base.Index
			changed.Index = &index
			changed.Columns = append([]KeyColumn(nil), base.Columns...)
			tt.mutate(&changed)
			if got := FingerprintKeyDescriptor(changed); got == base.Fingerprint {
				t.Fatal("mutation did not change fingerprint")
			}
		})
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
