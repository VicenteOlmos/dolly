package dump

import (
	"strings"
	"testing"
)

func TestBuildFKGraphRejectsExternalSchema(t *testing.T) {
	tables := fixtureTables()
	tables[1].ForeignKeys[0].ReferencedTableSchema = "other"
	_, err := buildFKGraph(tables)
	if err == nil || !strings.Contains(err.Error(), "other.departments") {
		t.Fatalf("buildFKGraph() = %v", err)
	}
	if !strings.Contains(err.Error(), "--include-table") {
		t.Fatalf("buildFKGraph() = %v, want --include-table hint", err)
	}
}

func TestBuildFKGraphRejectsUnknownParent(t *testing.T) {
	tables := fixtureTables()
	tables[1].ForeignKeys[0].ReferencedTableName = "outside"
	_, err := buildFKGraph(tables)
	if err == nil || !strings.Contains(err.Error(), "public.outside") {
		t.Fatalf("buildFKGraph() = %v", err)
	}
	if !strings.Contains(err.Error(), "public.tbl_a") {
		t.Fatalf("buildFKGraph() = %v, want child schema.table", err)
	}
}
