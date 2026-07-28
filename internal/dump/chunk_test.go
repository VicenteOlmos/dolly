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

func TestPlanChunkStreamingUnmatchedFails(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "users"}}
	policy := &ChunkPolicy{
		Requests: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "missing"}}},
	}
	_, _, err := PlanChunkStreaming(tables, policy, nil)
	if err == nil || !IsChunkPolicyError(err) {
		t.Fatalf("error = %v, want chunk policy", err)
	}
	if !strings.Contains(err.Error(), "not found in selected tables") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanChunkStreamingNoPKFails(t *testing.T) {
	tables := []db.Table{{
		Schema: "public",
		Name:   "heap_only",
		Columns: []db.Column{
			{Name: "id", DataType: "integer"},
		},
	}}
	policy := &ChunkPolicy{
		Requests: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "heap_only"}}},
	}
	_, _, err := PlanChunkStreaming(tables, policy, nil)
	if err == nil || !IsChunkPolicyError(err) {
		t.Fatalf("error = %v, want chunk policy", err)
	}
	if !strings.Contains(err.Error(), "no primary key") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanChunkStreamingResolvesSelected(t *testing.T) {
	tables := []db.Table{
		{
			Schema: "public",
			Name:   "users",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true},
			},
		},
		{
			Schema: "public",
			Name:   "orders",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true},
			},
		},
	}
	policy := &ChunkPolicy{
		Requests: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "users"}}},
	}
	chunkSet, prov, err := PlanChunkStreaming(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunkSet) != 1 {
		t.Fatalf("chunk set = %v", chunkSet)
	}
	if _, ok := chunkSet[tableKey("public", "users")]; !ok {
		t.Fatalf("chunk set = %v", chunkSet)
	}
	if len(prov.Chunked) != 1 || prov.Chunked[0] != "public.users" {
		t.Fatalf("chunked = %v", prov.Chunked)
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
