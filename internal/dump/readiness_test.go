package dump

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestBuildFKGraphSupportsSelectedNonPublicSchema(t *testing.T) {
	tables := []db.Table{
		{Schema: "tenant", Name: "parent"},
		{Schema: "tenant", Name: "child", ForeignKeys: []db.ForeignKey{{ConstraintName: "child_parent", ColumnName: "parent_id", ReferencedTableSchema: "tenant", ReferencedTableName: "parent", ReferencedColumnName: "id"}}},
	}
	if _, err := buildFKGraph(tables); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFKGraphKeepsSameNamedTablesInSeparateSchemas(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "parent"},
		{Schema: "public", Name: "events"},
		{Schema: "tenant", Name: "events", ForeignKeys: []db.ForeignKey{{
			ConstraintName:        "events_parent",
			ColumnName:            "parent_id",
			ReferencedTableSchema: "public",
			ReferencedTableName:   "parent",
			ReferencedColumnName:  "id",
		}}},
	}

	graph, err := buildFKGraph(tables)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.childToParents[tableKey("tenant", "events")]) != 1 {
		t.Fatalf("tenant events edge missing: %#v", graph.childToParents)
	}
	if len(graph.childToParents[tableKey("public", "events")]) != 0 {
		t.Fatalf("public events inherited tenant edge: %#v", graph.childToParents)
	}
}

func TestSlowArtifactsUseQualifiedIdentityAndRejectAmbiguousLegacyFiles(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "events"},
		{Schema: "tenant", Name: "events"},
	}
	if slowArtifactStem(tables[0]) == slowArtifactStem(tables[1]) {
		t.Fatal("same-named tables must not share slow artifact names")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.ckpt.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := rejectAmbiguousLegacySlowArtifacts(dir, tables)
	if err == nil || !strings.Contains(err.Error(), "ambiguous legacy slow artifact") {
		t.Fatalf("err = %v", err)
	}
}
