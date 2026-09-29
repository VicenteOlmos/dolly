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
