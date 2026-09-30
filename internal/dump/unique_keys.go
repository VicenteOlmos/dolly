package dump

import (
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func persistUniqueKeys(tables []db.Table) {
	for i := range tables {
		tables[i].UniqueKeys = uniqueKeysForMetadata(tables[i])
	}
}

func uniqueKeysForMetadata(table db.Table) [][]string {
	var keys [][]string
	for _, index := range table.UniqueIndexes {
		descriptor, ok := uniqueKeyDescriptor(table, index)
		if !ok {
			continue
		}
		keys = append(keys, descriptor.ColumnNames())
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.Join(keys[i], ",") < strings.Join(keys[j], ",")
	})
	return keys
}
