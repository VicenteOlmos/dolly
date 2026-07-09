package restore

import (
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func validateSchema(metaTables, targetTables []db.Table) error {
	targetByName := make(map[string]db.Table, len(targetTables))
	for _, t := range targetTables {
		targetByName[t.Name] = t
	}

	for _, meta := range metaTables {
		target, ok := targetByName[meta.Name]
		if !ok {
			return fmt.Errorf("table %q in metadata not found in target schema", meta.Name)
		}
		if err := validateTableColumns(meta, target); err != nil {
			return err
		}
	}

	return nil
}

func validateTableColumns(meta, target db.Table) error {
	if len(target.Columns) < len(meta.Columns) {
		return fmt.Errorf(
			"table %q: target has fewer columns than metadata (metadata %d, target %d)",
			meta.Name, len(meta.Columns), len(target.Columns),
		)
	}

	targetByName := make(map[string]db.Column, len(target.Columns))
	for _, c := range target.Columns {
		targetByName[c.Name] = c
	}

	for _, mc := range meta.Columns {
		tc, ok := targetByName[mc.Name]
		if !ok {
			return fmt.Errorf("table %q: column %q in metadata not found in target", meta.Name, mc.Name)
		}
		if mc.OrdinalPosition != tc.OrdinalPosition {
			return fmt.Errorf(
				"table %q column %q: ordinal mismatch (metadata %d, target %d)",
				meta.Name, mc.Name, mc.OrdinalPosition, tc.OrdinalPosition,
			)
		}
		if !strings.EqualFold(mc.DataType, tc.DataType) {
			return fmt.Errorf(
				"table %q column %q: type mismatch (metadata %q, target %q)",
				meta.Name, mc.Name, mc.DataType, tc.DataType,
			)
		}
		if mc.IsNullable != tc.IsNullable {
			return fmt.Errorf(
				"table %q column %q: nullability mismatch (metadata %v, target %v)",
				meta.Name, mc.Name, mc.IsNullable, tc.IsNullable,
			)
		}
		if mc.PrimaryKey != tc.PrimaryKey {
			return fmt.Errorf(
				"table %q column %q: primary key flag mismatch (metadata %v, target %v)",
				meta.Name, mc.Name, mc.PrimaryKey, tc.PrimaryKey,
			)
		}
	}

	return nil
}
