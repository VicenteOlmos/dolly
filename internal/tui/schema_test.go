package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSchemaLoadedShowsNextStepHint(t *testing.T) {
	draft := &SchemaDraft{
		Tables:      []string{"users", "orders"},
		TableCount:  2,
		ColumnCount: 5,
		FKCount:     1,
	}
	screen := newSchemaScreen(draft, func() bool { return true })
	view := stripANSIForGolden(screen.View(80, 24))
	if !strings.Contains(view, "dump (3)") || !strings.Contains(view, "clone (4)") {
		t.Fatalf("view missing dump/clone next-step hint: %s", view)
	}
}

func TestSchemaScreenScrollsTableList(t *testing.T) {
	tables := make([]string, 12)
	for i := range tables {
		tables[i] = fmt.Sprintf("schema_%02d.table", i)
	}
	draft := &SchemaDraft{
		Tables:      tables,
		TableCount:  len(tables),
		ColumnCount: len(tables),
	}
	screen := newSchemaScreen(draft, func() bool { return true })

	before := stripANSIForGolden(screen.View(80, 12))
	if !strings.Contains(before, "schema_00.table") || !strings.Contains(before, "more") {
		t.Fatalf("initial view = %q, want first table and more indicator", before)
	}

	for range 6 {
		screen.Update(keyPress("", tea.KeyDown, 0))
	}
	after := stripANSIForGolden(screen.View(80, 12))
	if strings.Contains(after, "schema_00.table") {
		t.Fatalf("scrolled view = %q, should not keep first table pinned", after)
	}
	if !strings.Contains(after, "schema_06.table") || !strings.Contains(after, "above") {
		t.Fatalf("scrolled view = %q, want later table and above indicator", after)
	}
}
