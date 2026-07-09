package dump

import (
	"sort"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func qualifiedName(schema, name string) string {
	return schema + "." + name
}

// SortTables orders tables in foreign-key dependency order (parents before children).
func SortTables(tables []db.Table) []db.Table {
	nameToIdx := make(map[string]int, len(tables))
	for i, t := range tables {
		nameToIdx[qualifiedName(t.Schema, t.Name)] = i
	}

	inDegree := make(map[string]int, len(tables))
	adj := make(map[string][]string, len(tables))
	for _, t := range tables {
		q := qualifiedName(t.Schema, t.Name)
		inDegree[q] = 0
		adj[q] = nil
	}

	for _, t := range tables {
		parents := inSchemaParentEdges(t, nameToIdx)
		q := qualifiedName(t.Schema, t.Name)
		for parent := range parents {
			adj[parent] = append(adj[parent], q)
			inDegree[q]++
		}
	}

	var queue []string
	for _, t := range tables {
		q := qualifiedName(t.Schema, t.Name)
		if inDegree[q] == 0 {
			queue = append(queue, q)
		}
	}
	sort.Strings(queue)

	visited := make(map[string]bool, len(tables))
	var result []db.Table
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		result = append(result, tables[nameToIdx[name]])
		visited[name] = true

		for _, child := range adj[name] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
		sort.Strings(queue)
	}

	if len(result) < len(tables) {
		var remaining []string
		for _, t := range tables {
			q := qualifiedName(t.Schema, t.Name)
			if !visited[q] {
				remaining = append(remaining, q)
			}
		}
		sort.Strings(remaining)
		for _, name := range remaining {
			result = append(result, tables[nameToIdx[name]])
		}
	}

	return result
}
