package dump

import (
	"strings"
	"testing"
)

func TestBuildFKGraphRejectsExternalSchema(t *testing.T) {
	tables := fixtureTables()
	tables[1].ForeignKeys[0].ReferencedTableSchema = "other"
	_, err := buildFKGraph(tables)
	if err == nil || !strings.Contains(err.Error(), "external table") {
		t.Fatalf("buildFKGraph() = %v", err)
	}
}

func TestBuildFKGraphRejectsUnknownParent(t *testing.T) {
	tables := fixtureTables()
	tables[1].ForeignKeys[0].ReferencedTableName = "outside"
	_, err := buildFKGraph(tables)
	if err == nil || !strings.Contains(err.Error(), "external table") {
		t.Fatalf("buildFKGraph() = %v", err)
	}
}
