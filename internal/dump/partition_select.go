package dump

import (
	"fmt"
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func rejectIncludedPartitionParents(tables []db.Table, policy *SelectionPolicy) error {
	if policy == nil || len(policy.Includes) == 0 {
		return nil
	}
	byKey := make(map[string]db.Table, len(tables))
	for _, table := range tables {
		byKey[tableKey(table.Schema, table.Name)] = table
	}
	for _, inc := range policy.Includes {
		table, ok := byKey[inc.Table.key()]
		if !ok || table.RelKind != "p" {
			continue
		}
		var leaves []string
		for _, leaf := range nestedPartitionLeaves(qualifiedName(table.Schema, table.Name), tables) {
			leaves = append(leaves, selectorIdent(leaf.Schema)+"."+selectorIdent(leaf.Name))
		}
		return fmt.Errorf(
			"%w: include partitioned table %q is not supported; include leaf partitions: %s",
			ErrTableSelection,
			inc.Table.Normalized(),
			strings.Join(leaves, ", "),
		)
	}
	return nil
}

func selectorIdent(name string) string {
	if parsed, err := parseUnquotedIdentComponent(name); err == nil && parsed == strings.ToLower(parsed) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func planPartitionTableSelection(tables []db.Table, policy *SelectionPolicy, ignored []IgnoredFileLine) ([]db.Table, TableSelectionProvenance, error) {
	if err := rejectIncludedPartitionParents(tables, policy); err != nil {
		return nil, TableSelectionProvenance{}, err
	}
	expanded := expandExcludedPartitionParents(tables, policy)
	filtered, prov, err := PlanTableSelection(db.WithoutPartitionParents(tables), expanded, ignored)
	if err != nil {
		return nil, prov, err
	}
	prov.RequestedExcludes = SelectionPolicyResumeFingerprint(policy).RequestedExcludes
	return filtered, prov, nil
}

func expandExcludedPartitionParents(tables []db.Table, policy *SelectionPolicy) *SelectionPolicy {
	if policy == nil || len(policy.Excludes) == 0 {
		return policy
	}
	byKey := make(map[string]db.Table, len(tables))
	for _, table := range tables {
		byKey[tableKey(table.Schema, table.Name)] = table
	}
	included := make(map[string]struct{}, len(policy.Includes))
	for _, inc := range policy.Includes {
		included[inc.Table.key()] = struct{}{}
	}
	seen := make(map[string]struct{}, len(policy.Excludes))
	excludes := make([]SelectorEntry, 0, len(policy.Excludes))
	for _, exc := range policy.Excludes {
		key := exc.Table.key()
		if _, dup := seen[key]; dup {
			continue
		}
		table, ok := byKey[key]
		if ok && table.RelKind == "p" {
			seen[key] = struct{}{}
			parent := qualifiedName(table.Schema, table.Name)
			for _, leaf := range nestedPartitionLeaves(parent, tables) {
				leafKey := tableKey(leaf.Schema, leaf.Name)
				if _, dup := seen[leafKey]; dup {
					continue
				}
				if _, keep := included[leafKey]; keep {
					continue
				}
				seen[leafKey] = struct{}{}
				excludes = append(excludes, SelectorEntry{
					Table:  QualifiedTable{Schema: leaf.Schema, Name: leaf.Name},
					Raw:    qualifiedName(leaf.Schema, leaf.Name),
					Source: exc.Source,
				})
			}
			continue
		}
		seen[key] = struct{}{}
		excludes = append(excludes, exc)
	}
	return &SelectionPolicy{Includes: policy.Includes, Excludes: excludes}
}

func nestedPartitionLeaves(parentKey string, tables []db.Table) []db.Table {
	var leaves []db.Table
	for _, table := range tables {
		if table.PartitionOf != parentKey {
			continue
		}
		if table.RelKind == "p" {
			leaves = append(leaves, nestedPartitionLeaves(qualifiedName(table.Schema, table.Name), tables)...)
			continue
		}
		leaves = append(leaves, table)
	}
	sort.Slice(leaves, func(i, j int) bool {
		return qualifiedName(leaves[i].Schema, leaves[i].Name) < qualifiedName(leaves[j].Schema, leaves[j].Name)
	})
	return leaves
}
