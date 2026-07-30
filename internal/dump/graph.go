package dump

import (
	"fmt"
	"sort"

	"github.com/VicenteOlmos/dolly/internal/db"
)

type fkEdge struct {
	childTable   string
	childColumn  string
	parentTable  string
	parentColumn string
}

type fkGraph struct {
	childToParents   map[string][]fkEdge
	parentToChildren map[string][]fkEdge
}

func buildFKGraph(tables []db.Table) (*fkGraph, error) {
	nameSet := make(map[string]struct{}, len(tables))
	for _, t := range tables {
		nameSet[tableKey(t.Schema, t.Name)] = struct{}{}
	}

	g := &fkGraph{
		childToParents:   make(map[string][]fkEdge),
		parentToChildren: make(map[string][]fkEdge),
	}

	for _, t := range tables {
		for _, fk := range t.ForeignKeys {
			if _, ok := nameSet[tableKey(fk.ReferencedTableSchema, fk.ReferencedTableName)]; !ok {
				return nil, fmt.Errorf(
					"foreign key %q on %q references external table %q",
					fk.ConstraintName, t.Name, fk.ReferencedTableName,
				)
			}
			edge := fkEdge{
				childTable:   tableKey(t.Schema, t.Name),
				childColumn:  fk.ColumnName,
				parentTable:  tableKey(fk.ReferencedTableSchema, fk.ReferencedTableName),
				parentColumn: fk.ReferencedColumnName,
			}
			g.childToParents[edge.childTable] = append(g.childToParents[edge.childTable], edge)
			g.parentToChildren[edge.parentTable] = append(g.parentToChildren[edge.parentTable], edge)
		}
	}
	// Deterministic edge ordering: sort each edge slice by childTable,
	// then by childColumn. This guarantees that FK closure traversal
	// (which iterates graph.parentToChildren and graph.childToParents via
	// range loops) produces identical subsets across repeated runs.
	for _, edges := range g.parentToChildren {
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].childTable != edges[j].childTable {
				return edges[i].childTable < edges[j].childTable
			}
			return edges[i].childColumn < edges[j].childColumn
		})
	}
	for _, edges := range g.childToParents {
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].parentTable != edges[j].parentTable {
				return edges[i].parentTable < edges[j].parentTable
			}
			return edges[i].parentColumn < edges[j].parentColumn
		})
	}
	return g, nil
}

func inSchemaParentEdges(t db.Table, nameToIdx map[string]int) map[string]struct{} {
	parents := make(map[string]struct{})
	for _, fk := range t.ForeignKeys {
		q := tableKey(fk.ReferencedTableSchema, fk.ReferencedTableName)
		if _, ok := nameToIdx[q]; !ok {
			continue
		}
		parents[q] = struct{}{}
	}
	return parents
}
