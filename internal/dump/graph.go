package dump

import (
	"fmt"

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
		nameSet[t.Name] = struct{}{}
	}

	g := &fkGraph{
		childToParents:   make(map[string][]fkEdge),
		parentToChildren: make(map[string][]fkEdge),
	}

	for _, t := range tables {
		for _, fk := range t.ForeignKeys {
			if fk.ReferencedTableSchema != "public" {
				return nil, fmt.Errorf(
					"foreign key %q on %q references schema %q (only public schema supported)",
					fk.ConstraintName, t.Name, fk.ReferencedTableSchema,
				)
			}
			if _, ok := nameSet[fk.ReferencedTableName]; !ok {
				return nil, fmt.Errorf(
					"foreign key %q on %q references external table %q",
					fk.ConstraintName, t.Name, fk.ReferencedTableName,
				)
			}
			edge := fkEdge{
				childTable:   t.Name,
				childColumn:  fk.ColumnName,
				parentTable:  fk.ReferencedTableName,
				parentColumn: fk.ReferencedColumnName,
			}
			g.childToParents[t.Name] = append(g.childToParents[t.Name], edge)
			g.parentToChildren[fk.ReferencedTableName] = append(g.parentToChildren[fk.ReferencedTableName], edge)
		}
	}
	return g, nil
}

func inSchemaParentEdges(t db.Table, nameToIdx map[string]int) map[string]struct{} {
	parents := make(map[string]struct{})
	for _, fk := range t.ForeignKeys {
		q := qualifiedName(fk.ReferencedTableSchema, fk.ReferencedTableName)
		if _, ok := nameToIdx[q]; !ok {
			continue
		}
		parents[q] = struct{}{}
	}
	return parents
}
