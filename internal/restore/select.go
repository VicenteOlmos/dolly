package restore

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

// ErrRestoreTableSelection marks restore exclude selector planning failures.
var ErrRestoreTableSelection = errors.New("restore table selection")

// ResolveRestoreExcludeSelector maps one selector onto a table listed in dump metadata.
// Qualified schema.table selectors use the same grammar as dump include-table.
// A bare table name must match exactly one table in the dump.
func ResolveRestoreExcludeSelector(raw string, tables []db.Table) (dump.QualifiedTable, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return dump.QualifiedTable{}, fmt.Errorf("empty table selector")
	}

	qt, err := dump.ParseQualifiedTable(s)
	if err == nil {
		for _, t := range tables {
			if t.Schema == qt.Schema && t.Name == qt.Name {
				return qt, nil
			}
		}
		return dump.QualifiedTable{}, fmt.Errorf("%w: exclude table %q not found in dump", ErrRestoreTableSelection, qt.Normalized())
	}
	if !strings.Contains(err.Error(), "unqualified") {
		return dump.QualifiedTable{}, err
	}
	tbl, nameErr := findDumpTableByBareName(s, tables)
	if nameErr != nil {
		return dump.QualifiedTable{}, nameErr
	}
	return dump.QualifiedTable{Schema: tbl.Schema, Name: tbl.Name}, nil
}

func findDumpTableByBareName(name string, tables []db.Table) (db.Table, error) {
	name = strings.TrimSpace(name)
	var matches []db.Table
	for _, t := range tables {
		if t.Name == name {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return db.Table{}, fmt.Errorf("%w: exclude table %q not found in dump", ErrRestoreTableSelection, name)
	case 1:
		return matches[0], nil
	default:
		labels := make([]string, len(matches))
		for i, t := range matches {
			labels[i] = dumpQualifiedName(t.Schema, t.Name)
		}
		sort.Strings(labels)
		return db.Table{}, fmt.Errorf("%w: ambiguous table %q: matches %s", ErrRestoreTableSelection, name, strings.Join(labels, ", "))
	}
}

func dumpQualifiedName(schema, name string) string {
	return dump.QualifiedTable{Schema: schema, Name: name}.Normalized()
}

// ApplyRestoreTableExclusions removes excluded tables from dump metadata order.
func ApplyRestoreTableExclusions(tables []db.Table, excludeSelectors []string) ([]db.Table, int, map[string]bool, error) {
	if len(excludeSelectors) == 0 {
		return tables, 0, nil, nil
	}

	excludeKeys := make(map[string]struct{})
	for _, raw := range excludeSelectors {
		qt, err := ResolveRestoreExcludeSelector(raw, tables)
		if err != nil {
			return nil, 0, nil, err
		}
		excludeKeys[tableKey(qt.Schema, qt.Name)] = struct{}{}
	}

	excludedTables := make(map[string]bool, len(excludeKeys))
	var filtered []db.Table
	for _, t := range tables {
		key := tableKey(t.Schema, t.Name)
		if _, skip := excludeKeys[key]; skip {
			excludedTables[key] = true
			continue
		}
		filtered = append(filtered, t)
	}
	excluded := len(tables) - len(filtered)
	return filtered, excluded, excludedTables, nil
}
