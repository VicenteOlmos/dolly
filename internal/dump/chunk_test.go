package dump

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestBuildChunkPolicyCombinesDirectAndFile(t *testing.T) {
	policy, _, err := BuildChunkPolicy(
		[]string{"public.users"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if policy == nil || len(policy.Requests) != 1 {
		t.Fatalf("policy = %+v", policy)
	}
	if policy.Requests[0].Table.Name != "users" {
		t.Fatalf("table = %+v", policy.Requests[0].Table)
	}
}

func TestPlanChunkStreaming(t *testing.T) {
	safeUnique := func(name string, oid uint32, columns ...db.UniqueIndexColumn) db.UniqueIndexInfo {
		return db.UniqueIndexInfo{
			IndexSchema: "public", IndexName: name, IndexOID: oid,
			IsValid: true, IsReady: true, AccessMethod: "btree", KeyColumns: columns,
		}
	}
	keyColumn := func(name string, position int, attnum int16) db.UniqueIndexColumn {
		return db.UniqueIndexColumn{Name: name, Position: position, Attnum: attnum, OpclassOID: 1978}
	}

	tests := []struct {
		name           string
		tables         []db.Table
		requests       []SelectorEntry
		wantStrategies map[string]KeyStrategy
		wantResumable  map[string]bool
		wantRequested  []string
		wantChunked    []string
		wantFallback   []string
		wantErr        string
	}{
		{
			name:     "unknown exact selector fails closed",
			tables:   []db.Table{{Schema: "public", Name: "users"}},
			requests: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "missing"}}},
			wantErr:  "not found in selected tables",
		},
		{
			name: "primary key receives keyset plan",
			tables: []db.Table{{Schema: "public", Name: "users", Columns: []db.Column{
				{Name: "id", PrimaryKey: true, OrdinalPosition: 1},
			}}},
			requests:       []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "users"}}},
			wantStrategies: map[string]KeyStrategy{tableKey("public", "users"): KeyStrategyPrimaryKey},
			wantResumable:  map[string]bool{tableKey("public", "users"): true},
			wantRequested:  []string{"public.users"},
			wantChunked:    []string{"public.users"},
		},
		{
			name: "eligible unique key receives keyset plan",
			tables: []db.Table{{Schema: "public", Name: "events", Columns: []db.Column{
				{Name: "code", OrdinalPosition: 1},
			}, UniqueIndexes: []db.UniqueIndexInfo{safeUnique("events_code_key", 42, keyColumn("code", 1, 1))}}},
			requests:       []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events"}}},
			wantStrategies: map[string]KeyStrategy{tableKey("public", "events"): KeyStrategyUniqueIndex},
			wantResumable:  map[string]bool{tableKey("public", "events"): true},
			wantRequested:  []string{"public.events"},
			wantChunked:    []string{"public.events"},
		},
		{
			name: "no safe key receives normal stream fallback",
			tables: []db.Table{{Schema: "public", Name: "heap_only", Columns: []db.Column{
				{Name: "id", OrdinalPosition: 1},
			}}},
			requests:       []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "heap_only"}}},
			wantStrategies: map[string]KeyStrategy{tableKey("public", "heap_only"): KeyStrategyNormalStream},
			wantResumable:  map[string]bool{tableKey("public", "heap_only"): false},
			wantRequested:  []string{"public.heap_only"},
			wantFallback:   []string{"public.heap_only"},
		},
		{
			name: "provenance is qualified and deterministic",
			tables: []db.Table{
				{Schema: "public", Name: "zeta", Columns: []db.Column{{Name: "id", PrimaryKey: true, OrdinalPosition: 1}}},
				{Schema: "public", Name: "beta", Columns: []db.Column{{Name: "id", PrimaryKey: true, OrdinalPosition: 1}}},
				{Schema: "private", Name: "alpha", Columns: []db.Column{{Name: "id", OrdinalPosition: 1}}},
				{Schema: "public", Name: "heap", Columns: []db.Column{{Name: "id", OrdinalPosition: 1}}},
			},
			requests: []SelectorEntry{
				{Table: QualifiedTable{Schema: "public", Name: "zeta"}},
				{Table: QualifiedTable{Schema: "public", Name: "heap"}},
				{Table: QualifiedTable{Schema: "private", Name: "alpha"}},
				{Table: QualifiedTable{Schema: "public", Name: "beta"}},
				{Table: QualifiedTable{Schema: "public", Name: "zeta"}},
			},
			wantStrategies: map[string]KeyStrategy{
				tableKey("public", "zeta"):   KeyStrategyPrimaryKey,
				tableKey("public", "beta"):   KeyStrategyPrimaryKey,
				tableKey("private", "alpha"): KeyStrategyNormalStream,
				tableKey("public", "heap"):   KeyStrategyNormalStream,
			},
			wantResumable: map[string]bool{
				tableKey("public", "zeta"):   true,
				tableKey("public", "beta"):   true,
				tableKey("private", "alpha"): false,
				tableKey("public", "heap"):   false,
			},
			wantRequested: []string{"private.alpha", "public.beta", "public.heap", "public.zeta", "public.zeta"},
			wantChunked:   []string{"public.beta", "public.zeta"},
			wantFallback:  []string{"private.alpha", "public.heap"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plans, prov, err := PlanChunkStreaming(tt.tables, &ChunkPolicy{Requests: tt.requests}, nil)
			if tt.wantErr != "" {
				if err == nil || !IsChunkPolicyError(err) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want chunk policy containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plans) != len(tt.wantStrategies) {
				t.Fatalf("plans = %v, want %v", plans, tt.wantStrategies)
			}
			for key, want := range tt.wantStrategies {
				got, ok := plans[key]
				if !ok || got.Strategy != want || got.Resumable != tt.wantResumable[key] {
					t.Fatalf("plan for %q = %+v, want strategy %q resumable %t", key, got, want, tt.wantResumable[key])
				}
			}
			gotRequested := make([]string, len(prov.Requested))
			for i := range prov.Requested {
				gotRequested[i] = prov.Requested[i].Normalized
			}
			if !equalStrings(gotRequested, tt.wantRequested) {
				t.Fatalf("requested provenance = %v, want %v", gotRequested, tt.wantRequested)
			}
			if !equalStrings(prov.Chunked, tt.wantChunked) || !equalStrings(prov.Fallback, tt.wantFallback) {
				t.Fatalf("provenance = %+v, want chunked %v fallback %v", prov, tt.wantChunked, tt.wantFallback)
			}
		})
	}
}

func TestValidateDumpOptionsRejectsWorkersWithChunk(t *testing.T) {
	cfg := &config{
		workers: 2,
		chunkPolicy: &ChunkPolicy{
			Requests: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "users"}}},
		},
	}
	if err := validateDumpOptions(cfg); err == nil {
		t.Fatal("expected workers+chunk rejection")
	}
}

func TestValidateDumpOptionsRejectsWorkersWithSlow(t *testing.T) {
	cfg := &config{workers: 2, slowConnection: true}
	if err := validateDumpOptions(cfg); err == nil {
		t.Fatal("expected workers+slow rejection")
	}
}

func TestUsesResilientStreamingSlowExpandsAll(t *testing.T) {
	cfg := &config{slowConnection: true}
	table := db.Table{Schema: "public", Name: "users"}
	if !usesResilientStreaming(cfg, nil, table) {
		t.Fatal("expected slow-connection to use resilient streaming")
	}
}

func TestUsesResilientStreamingChunkOnlyNamed(t *testing.T) {
	cfg := &config{}
	chunkSet := map[string]struct{}{tableKey("public", "users"): {}}
	if !usesResilientStreaming(cfg, chunkSet, db.Table{Schema: "public", Name: "users"}) {
		t.Fatal("expected chunk-selected table to use resilient streaming")
	}
	if usesResilientStreaming(cfg, chunkSet, db.Table{Schema: "public", Name: "orders"}) {
		t.Fatal("expected non-chunk table to use normal streaming")
	}
}

func TestExecutableChunkSetOnlyIncludesPrimaryKeys(t *testing.T) {
	plans := map[string]KeyDescriptor{
		tableKey("public", "pk_table"):     {Strategy: KeyStrategyPrimaryKey, Resumable: true},
		tableKey("public", "unique_table"): {Strategy: KeyStrategyUniqueIndex, Resumable: true},
		tableKey("public", "heap_table"):   {Strategy: KeyStrategyNormalStream},
	}

	chunkSet := executableChunkSet(plans)
	if len(chunkSet) != 1 {
		t.Fatalf("executable chunk set = %v, want only primary-key plan", chunkSet)
	}
	if _, ok := chunkSet[tableKey("public", "pk_table")]; !ok {
		t.Fatalf("executable chunk set = %v, want primary-key plan", chunkSet)
	}
}
