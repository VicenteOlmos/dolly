package restore

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5"
)

// ErrRestoreCycle marks foreign-key cycles among eligible restore tables.
var ErrRestoreCycle = errors.New("restore dependency cycle")

// RestoreCycleError reports a deterministic cycle among qualified tables.
type RestoreCycleError struct {
	Tables []string
}

func (e *RestoreCycleError) Error() string {
	return fmt.Sprintf(
		"foreign key cycle among tables %s: parallel restore cannot schedule these tables; use serial atomic restore (workers=1) instead",
		strings.Join(e.Tables, ", "),
	)
}

func (e *RestoreCycleError) Is(target error) bool {
	return target == ErrRestoreCycle
}

// RestoreLevel is one FK dependency level. Tables in a level may run concurrently;
// every parent appears in an earlier level than its children.
type RestoreLevel struct {
	Tables []string
}

func tableKey(schema, name string) string {
	return schema + "\x00" + name
}

func qualifiedLabel(schema, name string) string {
	if strings.ContainsAny(schema, ".\"") || strings.ContainsAny(name, ".\"") {
		return pgx.Identifier{schema, name}.Sanitize()
	}
	return schema + "." + name
}

func parentEdges(t db.Table, eligible map[string]struct{}) map[string]struct{} {
	parents := make(map[string]struct{})
	for _, fk := range t.ForeignKeys {
		parent := tableKey(fk.ReferencedTableSchema, fk.ReferencedTableName)
		if _, ok := eligible[parent]; !ok {
			continue
		}
		child := tableKey(t.Schema, t.Name)
		if parent == child {
			parents[parent] = struct{}{}
			continue
		}
		parents[parent] = struct{}{}
	}
	return parents
}

// BuildRestoreLevels partitions eligible tables into deterministic FK dependency
// levels using Kahn topological sorting. FK edges to tables outside tables are
// ignored. Cycles and self-references return RestoreCycleError.
func BuildRestoreLevels(tables []db.Table) ([]RestoreLevel, error) {
	if len(tables) == 0 {
		return nil, nil
	}

	sortedInput := append([]db.Table(nil), tables...)
	sort.Slice(sortedInput, func(i, j int) bool {
		ki := tableKey(sortedInput[i].Schema, sortedInput[i].Name)
		kj := tableKey(sortedInput[j].Schema, sortedInput[j].Name)
		return ki < kj
	})

	byKey := make(map[string]db.Table, len(sortedInput))
	keys := make([]string, 0, len(sortedInput))
	for _, t := range sortedInput {
		key := tableKey(t.Schema, t.Name)
		if _, exists := byKey[key]; exists {
			continue
		}
		byKey[key] = t
		keys = append(keys, key)
	}
	sort.Strings(keys)

	eligible := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		eligible[key] = struct{}{}
	}

	inDegree := make(map[string]int, len(keys))
	adj := make(map[string][]string, len(keys))
	for _, key := range keys {
		inDegree[key] = 0
		adj[key] = nil
	}

	for _, key := range keys {
		t := byKey[key]
		for parent := range parentEdges(t, eligible) {
			adj[parent] = append(adj[parent], key)
			inDegree[key]++
		}
	}

	for parent, children := range adj {
		sort.Strings(children)
		adj[parent] = children
	}

	var levels []RestoreLevel
	remaining := len(keys)
	visited := make(map[string]bool, len(keys))

	queue := make([]string, 0, len(keys))
	for _, key := range keys {
		if inDegree[key] == 0 {
			queue = append(queue, key)
		}
	}
	sort.Strings(queue)

	for len(queue) > 0 {
		level := make([]string, len(queue))
		for i, key := range queue {
			level[i] = qualifiedLabel(byKey[key].Schema, byKey[key].Name)
			visited[key] = true
			remaining--
		}
		sort.Strings(level)
		levels = append(levels, RestoreLevel{Tables: level})

		next := make([]string, 0)
		for _, key := range queue {
			for _, child := range adj[key] {
				inDegree[child]--
				if inDegree[child] == 0 {
					next = append(next, child)
				}
			}
		}
		sort.Strings(next)
		queue = next
	}

	if remaining > 0 {
		var cyclic []string
		for _, key := range keys {
			if !visited[key] {
				t := byKey[key]
				cyclic = append(cyclic, qualifiedLabel(t.Schema, t.Name))
			}
		}
		sort.Strings(cyclic)
		return nil, &RestoreCycleError{Tables: cyclic}
	}

	return levels, nil
}
