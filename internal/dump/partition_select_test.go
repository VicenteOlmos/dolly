package dump

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func partitionFixtureTables() []db.Table {
	return []db.Table{
		{Schema: "public", Name: "events", RelKind: "p"},
		{Schema: "public", Name: "events_2024", PartitionOf: "public.events"},
		{Schema: "public", Name: "events_2025", PartitionOf: "public.events"},
		{Schema: "public", Name: "users"},
	}
}

func TestRejectIncludedPartitionParent(t *testing.T) {
	tables := partitionFixtureTables()
	policy := &SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events"}}},
	}
	err := rejectIncludedPartitionParents(tables, policy)
	if !IsTableSelectionError(err) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "public.events_2024") || !strings.Contains(err.Error(), "public.events_2025") {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectIncludedPartitionParentBeforeExclude(t *testing.T) {
	tables := partitionFixtureTables()
	policy := &SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events"}}},
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events"}}},
	}
	err := rejectIncludedPartitionParents(tables, policy)
	if !IsTableSelectionError(err) {
		t.Fatalf("include of parent should fail before exclude expansion: %v", err)
	}
}
