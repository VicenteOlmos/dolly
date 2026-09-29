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

func nestedPartitionFixtureTables() []db.Table {
	return []db.Table{
		{Schema: "sales.eu", Name: "events", RelKind: "p"},
		{Schema: "sales.eu", Name: "events_2024", RelKind: "p", PartitionOf: "sales.eu.events"},
		{Schema: "sales.eu", Name: "events_jan", PartitionOf: "sales.eu.events_2024"},
		{Schema: "sales.eu", Name: "events_feb", PartitionOf: "sales.eu.events_2024"},
		{Schema: "sales.eu", Name: "events_2025", PartitionOf: "sales.eu.events"},
		{Schema: "sales.eu", Name: "users"},
	}
}

func TestRejectIncludedPartitionParentNoLeaves(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "events", RelKind: "p"},
		{Schema: "public", Name: "users"},
	}
	policy := &SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events"}}},
	}
	err := rejectIncludedPartitionParents(tables, policy)
	if !IsTableSelectionError(err) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "no leaf partitions in scope") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "include leaf partitions:") {
		t.Fatalf("must not print empty leaf list: %v", err)
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

func TestRejectIncludedNestedPartitionParentListsLeaves(t *testing.T) {
	tables := nestedPartitionFixtureTables()
	policy := &SelectionPolicy{Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "sales.eu", Name: "events"}}}}
	err := rejectIncludedPartitionParents(tables, policy)
	if !IsTableSelectionError(err) {
		t.Fatalf("err = %v", err)
	}
	for _, name := range []string{`"sales.eu".events_jan`, `"sales.eu".events_feb`, `"sales.eu".events_2025`} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("missing leaf %q: %v", name, err)
		}
	}
	if strings.Contains(err.Error(), `"sales.eu".events_2024`) {
		t.Fatalf("intermediate parent listed as leaf: %v", err)
	}
}

func TestExpandExcludedPartitionParentDropsLeaves(t *testing.T) {
	tables := partitionFixtureTables()
	policy := expandExcludedPartitionParents(tables, &SelectionPolicy{
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events"}}},
	})
	leaves := db.WithoutPartitionParents(tables)
	filtered, prov, err := PlanTableSelection(leaves, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "users" {
		t.Fatalf("filtered = %+v", filtered)
	}
	for _, w := range prov.Warnings {
		if strings.Contains(w, "events") {
			t.Fatalf("unexpected warning: %s", w)
		}
	}
}

func TestExpandExcludedPartitionParentKeepsIncludedLeaf(t *testing.T) {
	tables := partitionFixtureTables()
	policy := expandExcludedPartitionParents(tables, &SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events_2024"}}},
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "events"}}},
	})
	leaves := db.WithoutPartitionParents(tables)
	filtered, _, err := PlanTableSelection(leaves, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "events_2024" {
		t.Fatalf("filtered = %+v", filtered)
	}
}

func TestExpandExcludedNestedPartitionParentWithDottedSchema(t *testing.T) {
	tables := nestedPartitionFixtureTables()
	policy := &SelectionPolicy{
		Excludes: []SelectorEntry{{Table: QualifiedTable{Schema: "sales.eu", Name: "events"}, Source: SelectorSource{Kind: "flag", Name: "--exclude-table"}}},
	}
	filtered, prov, err := planPartitionTableSelection(tables, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "users" || len(prov.Warnings) != 0 {
		t.Fatalf("filtered = %+v, warnings = %v", filtered, prov.Warnings)
	}
	if !SelectionResumeProvenanceMatches(SelectionPolicyResumeFingerprint(policy), &prov) {
		t.Fatalf("requested excludes do not match original policy: %+v", prov.RequestedExcludes)
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
