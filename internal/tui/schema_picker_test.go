package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSchemaPickerToggleSelectAll(t *testing.T) {
	p := &SchemaPickerState{
		AvailableSchemas: []string{"app", "billing", "public"},
		SelectedSchemas:  make(map[string]bool),
	}
	p.HandleKey(tea.Key{Text: "a"})
	if !p.AllSelected() || len(p.SelectedNames()) != 3 {
		t.Fatalf("after first a: selected = %v, want all", p.SelectedNames())
	}
	p.HandleKey(tea.Key{Text: "a"})
	if len(p.SelectedNames()) != 0 {
		t.Fatalf("after second a: selected = %v, want none", p.SelectedNames())
	}
}

func TestSchemaPickerSpaceTogglesSelection(t *testing.T) {
	p := &SchemaPickerState{
		AvailableSchemas: []string{"app", "billing"},
		SelectedSchemas:  make(map[string]bool),
	}
	p.HandleKey(tea.Key{Code: tea.KeySpace})
	p.MoveCursor(1)
	p.HandleKey(tea.Key{Code: tea.KeySpace})

	if !p.SelectedSchemas["app"] || !p.SelectedSchemas["billing"] {
		t.Fatalf("SelectedSchemas = %v, want app and billing", p.SelectedSchemas)
	}
	names := p.SelectedNames()
	if len(names) != 2 || names[0] != "app" || names[1] != "billing" {
		t.Fatalf("SelectedNames = %v, want [app billing]", names)
	}
}

func TestSeedSchemaPickerFromProfile(t *testing.T) {
	var p SchemaPickerState
	SeedSchemaPicker(&p, []string{"public", "app", "billing"}, []string{"app"})
	if !p.SelectedSchemas["app"] {
		t.Fatal("expected app pre-selected")
	}
	if p.SelectedSchemas["billing"] {
		t.Fatal("billing should not be pre-selected")
	}
}

func TestSchemaPickerRenderKeepsCursorVisible(t *testing.T) {
	p := &SchemaPickerState{
		AvailableSchemas: []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6"},
		SelectedSchemas:  make(map[string]bool),
	}
	for range 5 {
		p.MoveCursor(1)
	}

	view := stripANSIForGolden(strings.Join(renderSchemaPickerLines(p, 3), "\n"))
	if !strings.Contains(view, "> [ ] s5") {
		t.Fatalf("view = %q, want cursor row s5 visible", view)
	}
	if strings.Contains(view, "s0") {
		t.Fatalf("view = %q, should have scrolled past s0", view)
	}
	if !strings.Contains(view, "above") {
		t.Fatalf("view = %q, want hidden-above indicator", view)
	}
}
