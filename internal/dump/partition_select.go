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
		leaves := directPartitionChildren(qualifiedName(inc.Table.Schema, inc.Table.Name), tables)
		return fmt.Errorf(
			"%w: include partitioned table %q is not supported; include leaf partitions: %s",
			ErrTableSelection,
			inc.Table.Normalized(),
			strings.Join(leaves, ", "),
		)
	}
	return nil
}

func directPartitionChildren(parentKey string, tables []db.Table) []string {
	var names []string
	for _, table := range tables {
		if table.PartitionOf == parentKey {
			names = append(names, qualifiedName(table.Schema, table.Name))
		}
	}
	sort.Strings(names)
	return names
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
			for _, leafName := range nestedPartitionLeafKeys(parent, tables) {
				leafKey := tableKeyFromQualified(leafName)
				if _, dup := seen[leafKey]; dup {
					continue
				}
				if _, keep := included[leafKey]; keep {
					continue
				}
				seen[leafKey] = struct{}{}
				leaf := byKey[leafKey]
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

func nestedPartitionLeafKeys(parentKey string, tables []db.Table) []string {
	var keys []string
	for _, table := range tables {
		if table.PartitionOf != parentKey {
			continue
		}
		name := qualifiedName(table.Schema, table.Name)
		if partitionHasChildren(name, tables) {
			keys = append(keys, nestedPartitionLeafKeys(name, tables)...)
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

func tableKeyFromQualified(qualified string) string {
	schema, name, ok := strings.Cut(qualified, ".")
	if !ok {
		return qualified
	}
	return tableKey(schema, name)
}

func partitionHasChildren(parentKey string, tables []db.Table) bool {
	for _, table := range tables {
		if table.PartitionOf == parentKey {
			return true
		}
	}
	return false
}
