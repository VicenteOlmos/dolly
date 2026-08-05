package dump

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestWriteMetadata(t *testing.T) {
	dir := t.TempDir()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "users",
			Columns: []db.Column{
				{Name: "id", DataType: "integer"},
			},
			ForeignKeys: []db.ForeignKey{
				{ConstraintName: "fk_users_group", ColumnName: "group_id", ReferencedTableSchema: "public", ReferencedTableName: "groups", ReferencedColumnName: "id"},
			},
		},
	}

	path, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(path) != "metadata.json.tmp" {
		t.Fatalf("unexpected path basename: %s", filepath.Base(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}

	if meta.Schema != "public" {
		t.Fatalf("schema = %q, want public", meta.Schema)
	}
	if len(meta.Tables) != 1 || meta.Tables[0].Name != "users" {
		t.Fatalf("tables = %+v, want 1 table named users", meta.Tables)
	}
	if len(meta.Tables[0].Columns) != 1 || meta.Tables[0].Columns[0].Name != "id" {
		t.Fatalf("columns = %+v, want 1 column named id", meta.Tables[0].Columns)
	}
	if len(meta.Tables[0].ForeignKeys) != 1 || meta.Tables[0].ForeignKeys[0].ConstraintName != "fk_users_group" {
		t.Fatalf("foreign_keys = %+v, want 1 fk named fk_users_group", meta.Tables[0].ForeignKeys)
	}

	if _, err := time.Parse(time.RFC3339, meta.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}
}

func TestWriteMetadataEmpty(t *testing.T) {
	dir := t.TempDir()

	path, err := writeMetadata(dir, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}

	if len(meta.Tables) != 0 {
		t.Fatalf("expected 0 tables, got %d", len(meta.Tables))
	}
}

func TestWriteMetadataDeterministic(t *testing.T) {
	dir := t.TempDir()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "z_table",
			Columns: []db.Column{
				{Name: "b_col", DataType: "text"},
				{Name: "a_col", DataType: "integer"},
			},
			ForeignKeys: []db.ForeignKey{
				{ConstraintName: "fk_b", ColumnName: "b_col", ReferencedTableSchema: "public", ReferencedTableName: "other", ReferencedColumnName: "id"},
				{ConstraintName: "fk_a", ColumnName: "a_col", ReferencedTableSchema: "public", ReferencedTableName: "other", ReferencedColumnName: "id"},
			},
		},
		{
			Schema: "public",
			Name:   "a_table",
			Columns: []db.Column{
				{Name: "id", DataType: "integer"},
			},
		},
	}

	path1, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data1, err := os.ReadFile(path1)
	if err != nil {
		t.Fatal(err)
	}

	path2, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatal(err)
	}

	var meta1, meta2 Metadata
	if err := json.Unmarshal(data1, &meta1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data2, &meta2); err != nil {
		t.Fatal(err)
	}

	meta1.GeneratedAt = ""
	meta2.GeneratedAt = ""

	out1, err := json.Marshal(meta1)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := json.Marshal(meta2)
	if err != nil {
		t.Fatal(err)
	}

	if string(out1) != string(out2) {
		t.Fatal("metadata output is not deterministic")
	}
}

func TestReadMetadata(t *testing.T) {
	dir := t.TempDir()
	tables := []db.Table{
		{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", DataType: "integer"}}},
	}
	path, err := writeMetadata(dir, tables, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Schema != "public" || len(meta.Tables) != 1 || meta.Tables[0].Name != "users" {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestWriteMetadataMultiSchemaLabel(t *testing.T) {
	dir := t.TempDir()
	tables := []db.Table{
		{Schema: "app", Name: "orders"},
		{Schema: "billing", Name: "invoices"},
	}
	path, err := writeMetadata(dir, tables, nil, []string{"app", "billing"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Schema != "multi" {
		t.Fatalf("schema = %q, want multi", meta.Schema)
	}
}

func TestReadMetadataMissing(t *testing.T) {
	_, err := ReadMetadata(t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildStrategyRecordsDeterministicOrder(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "events", Columns: []db.Column{{Name: "code", OrdinalPosition: 1}}},
		{Schema: "public", Name: "heap", Columns: []db.Column{{Name: "note", OrdinalPosition: 1}}},
		{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", PrimaryKey: true, OrdinalPosition: 1}}},
	}
	eventsPlan := fingerprintDescriptor(KeyDescriptor{
		Strategy: KeyStrategyUniqueIndex, TableSchema: "public", TableName: "events",
		Columns:   []KeyColumn{{Name: "code", Position: 1, Attnum: 1, NotNull: true}},
		Index:     &IndexIdentity{Schema: "public", Name: "events_code_key", OID: 42, AccessMethod: "btree", Valid: true, Ready: true},
		Resumable: true,
	})
	heapPlan := fingerprintDescriptor(KeyDescriptor{
		Strategy: KeyStrategyNormalStream, TableSchema: "public", TableName: "heap",
	})
	usersPlan := fingerprintDescriptor(KeyDescriptor{
		Strategy: KeyStrategyPrimaryKey, TableSchema: "public", TableName: "users",
		Columns:   []KeyColumn{{Name: "id", Position: 1, Attnum: 1, NotNull: true}},
		Resumable: true,
	})

	plansForward := map[string]KeyDescriptor{}
	plansForward[tableKey("public", "events")] = eventsPlan
	plansForward[tableKey("public", "heap")] = heapPlan
	plansForward[tableKey("public", "users")] = usersPlan

	plansReverse := map[string]KeyDescriptor{}
	plansReverse[tableKey("public", "users")] = usersPlan
	plansReverse[tableKey("public", "heap")] = heapPlan
	plansReverse[tableKey("public", "events")] = eventsPlan

	records := BuildStrategyRecords(tables, plansForward)
	otherOrder := BuildStrategyRecords(tables, plansReverse)
	if !reflect.DeepEqual(records, otherOrder) {
		t.Fatalf("map insertion order leaked:\nforward = %+v\nreverse = %+v", records, otherOrder)
	}
	if len(records) != 3 {
		t.Fatalf("records = %+v", records)
	}

	want := []TableStrategyRecord{
		{
			Table: "public.events", Strategy: KeyStrategyUniqueIndex, Resumable: true,
			KeyColumns: eventsPlan.ColumnNames(), Fingerprint: eventsPlan.Fingerprint,
		},
		{Table: "public.heap", Strategy: KeyStrategyNormalStream, Resumable: false},
		{
			Table: "public.users", Strategy: KeyStrategyPrimaryKey, Resumable: true,
			KeyColumns: usersPlan.ColumnNames(), Fingerprint: usersPlan.Fingerprint,
		},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("records = %+v, want %+v", records, want)
	}
}

func TestBuildStrategyRecordsChunkOnlyScope(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "events"},
		{Schema: "public", Name: "users"},
	}
	plans := map[string]KeyDescriptor{
		tableKey("public", "events"): fingerprintDescriptor(KeyDescriptor{
			Strategy: KeyStrategyUniqueIndex, TableSchema: "public", TableName: "events",
			Columns:   []KeyColumn{{Name: "code", Position: 1, Attnum: 1, NotNull: true}},
			Resumable: true,
		}),
	}
	records := BuildStrategyRecords(tables, plans)
	if len(records) != 1 || records[0].Table != "public.events" {
		t.Fatalf("records = %+v", records)
	}
}

func TestMetadataStrategyProvenanceRoundtrip(t *testing.T) {
	wantStrategies := []TableStrategyRecord{
		{
			Table: "public.users", Strategy: KeyStrategyPrimaryKey, Resumable: true,
			KeyColumns: []string{"id"}, Fingerprint: "pk-fp",
		},
		{
			Table: "public.events", Strategy: KeyStrategyUniqueIndex, Resumable: true,
			KeyColumns: []string{"code"}, Fingerprint: "uniq-fp",
		},
		{Table: "public.logs", Strategy: KeyStrategyNormalStream, Resumable: false},
	}

	t.Run("write_read", func(t *testing.T) {
		dir := t.TempDir()
		prov := &Provenance{Strategies: wantStrategies}
		path, err := writeMetadata(dir, nil, nil, []string{"public"}, nil, prov)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path, filepath.Join(dir, "metadata.json")); err != nil {
			t.Fatal(err)
		}
		meta, err := ReadMetadata(dir)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Provenance == nil {
			t.Fatal("expected provenance")
		}
		if !reflect.DeepEqual(meta.Provenance.Strategies, wantStrategies) {
			t.Fatalf("strategies = %+v, want %+v", meta.Provenance.Strategies, wantStrategies)
		}
	})

	t.Run("raw_json", func(t *testing.T) {
		raw := `{
			"generated_at": "2026-01-01T00:00:00Z",
			"schema": "public",
			"tables": [],
			"provenance": {
				"seq": 1,
				"base_dir": "/tmp",
				"table_count": 0,
				"strategies": [
					{"table":"public.users","strategy":"primary_key","resumable":true,"key_columns":["id"],"fingerprint":"pk-fp"},
					{"table":"public.events","strategy":"unique_index","resumable":true,"key_columns":["code"],"fingerprint":"uniq-fp"},
					{"table":"public.logs","strategy":"normal_stream","resumable":false}
				]
			}
		}`
		var meta Metadata
		if err := json.Unmarshal([]byte(raw), &meta); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(meta.Provenance.Strategies, wantStrategies) {
			t.Fatalf("strategies = %+v, want %+v", meta.Provenance.Strategies, wantStrategies)
		}
	})
}

func TestFallbackStrategyRecordJSONShape(t *testing.T) {
	rec := tableStrategyRecord(fingerprintDescriptor(KeyDescriptor{
		Strategy: KeyStrategyNormalStream, TableSchema: "public", TableName: "logs",
	}))
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	if !strings.Contains(raw, `"resumable":false`) {
		t.Fatalf("fallback must emit explicit resumable false: %s", raw)
	}
	for _, forbidden := range []string{`"attnum"`, `"opclass_oid"`, `"collation_oid"`, `"index_oid"`, `"password"`} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("leaked field %s in %s", forbidden, raw)
		}
	}
	if strings.Contains(raw, `"key_columns"`) || strings.Contains(raw, `"fingerprint"`) {
		t.Fatalf("fallback must omit key identity fields: %s", raw)
	}
}

func TestReadMetadataLegacyWithoutStrategies(t *testing.T) {
	base := `{
		"generated_at": "2026-01-01T00:00:00Z",
		"schema": "public",
		"tables": [{"schema":"public","name":"users"}],
		"provenance": {
			"seq": 1,
			"base_dir": "/tmp",
			"table_count": 1,
			"chunk_tables": {
				"requested": [{"normalized":"public.users","source":"flag:--chunk-table"}],
				"chunked": ["public.users"]
			}%s
		}
	}`

	t.Run("absent_field", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(fmt.Sprintf(base, "")), 0o600); err != nil {
			t.Fatal(err)
		}
		meta, err := ReadMetadata(dir)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Provenance == nil || meta.Provenance.ChunkTables == nil {
			t.Fatal("expected legacy chunk_tables provenance")
		}
		if meta.Provenance.Strategies != nil {
			t.Fatalf("absent strategies = %+v, want nil", meta.Provenance.Strategies)
		}
	})

	t.Run("explicit_empty_slice", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(fmt.Sprintf(base, `,"strategies":[]`)), 0o600); err != nil {
			t.Fatal(err)
		}
		meta, err := ReadMetadata(dir)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Provenance == nil {
			t.Fatal("expected provenance")
		}
		if meta.Provenance.Strategies == nil {
			t.Fatal("explicit [] must decode to non-nil empty slice")
		}
		if len(meta.Provenance.Strategies) != 0 {
			t.Fatalf("strategies = %+v, want empty slice", meta.Provenance.Strategies)
		}
	})
}
