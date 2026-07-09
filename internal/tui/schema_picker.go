package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// SchemaPickerState holds multi-select schema picker UI state for dump/clone screens.
type SchemaPickerState struct {
	AvailableSchemas   []string
	SelectedSchemas    map[string]bool
	PickerCursor       int
	PickerScrollOffset int
}

func (p *SchemaPickerState) SelectedNames() []string {
	if p == nil || len(p.SelectedSchemas) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.SelectedSchemas))
	for name, on := range p.SelectedSchemas {
		if on {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func SeedSchemaPicker(p *SchemaPickerState, catalog, profileSchemas []string) {
	if p == nil {
		return
	}
	p.AvailableSchemas = append([]string(nil), catalog...)
	p.SelectedSchemas = make(map[string]bool)
	p.PickerCursor = 0
	p.PickerScrollOffset = 0
	if len(profileSchemas) == 0 {
		return
	}
	catalogSet := make(map[string]bool, len(catalog))
	for _, name := range catalog {
		catalogSet[name] = true
	}
	for _, name := range profileSchemas {
		if catalogSet[name] {
			p.SelectedSchemas[name] = true
		}
	}
}

func (p *SchemaPickerState) ToggleSelection() {
	if p == nil || len(p.AvailableSchemas) == 0 {
		return
	}
	name := p.AvailableSchemas[p.PickerCursor]
	if p.SelectedSchemas == nil {
		p.SelectedSchemas = make(map[string]bool)
	}
	p.SelectedSchemas[name] = !p.SelectedSchemas[name]
}

func (p *SchemaPickerState) AllSelected() bool {
	if p == nil || len(p.AvailableSchemas) == 0 {
		return false
	}
	for _, name := range p.AvailableSchemas {
		if p.SelectedSchemas == nil || !p.SelectedSchemas[name] {
			return false
		}
	}
	return true
}

func (p *SchemaPickerState) SelectAll() {
	if p == nil || len(p.AvailableSchemas) == 0 {
		return
	}
	if p.SelectedSchemas == nil {
		p.SelectedSchemas = make(map[string]bool)
	}
	for _, name := range p.AvailableSchemas {
		p.SelectedSchemas[name] = true
	}
}

func (p *SchemaPickerState) ClearAll() {
	if p == nil {
		return
	}
	p.SelectedSchemas = make(map[string]bool)
}

func (p *SchemaPickerState) ToggleSelectAll() {
	if p.AllSelected() {
		p.ClearAll()
	} else {
		p.SelectAll()
	}
}

func (p *SchemaPickerState) MoveCursor(delta int) {
	if p == nil || len(p.AvailableSchemas) == 0 {
		return
	}
	p.PickerCursor += delta
	if p.PickerCursor < 0 {
		p.PickerCursor = len(p.AvailableSchemas) - 1
	}
	if p.PickerCursor >= len(p.AvailableSchemas) {
		p.PickerCursor = 0
	}
}

func (p *SchemaPickerState) ensureCursorVisible(maxLines int) {
	if p == nil || len(p.AvailableSchemas) == 0 {
		return
	}
	if maxLines < 1 {
		maxLines = 1
	}
	if p.PickerCursor < 0 {
		p.PickerCursor = 0
	}
	if p.PickerCursor >= len(p.AvailableSchemas) {
		p.PickerCursor = len(p.AvailableSchemas) - 1
	}
	if p.PickerScrollOffset < 0 {
		p.PickerScrollOffset = 0
	}
	if p.PickerCursor < p.PickerScrollOffset {
		p.PickerScrollOffset = p.PickerCursor
	}
	if p.PickerCursor >= p.PickerScrollOffset+maxLines {
		p.PickerScrollOffset = p.PickerCursor - maxLines + 1
	}
	maxOffset := len(p.AvailableSchemas) - maxLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if p.PickerScrollOffset > maxOffset {
		p.PickerScrollOffset = maxOffset
	}
}

func (p *SchemaPickerState) HandleActionKey(k tea.Key) bool {
	if p == nil || len(p.AvailableSchemas) == 0 {
		return false
	}
	switch k.String() {
	case " ", "space":
		p.ToggleSelection()
		return true
	case "a":
		p.ToggleSelectAll()
		return true
	}
	switch k.Code {
	case tea.KeySpace:
		p.ToggleSelection()
		return true
	}
	return false
}

// HandleKey applies picker actions (toggle, select all). Arrow navigation is handled by the screen pane.
func (p *SchemaPickerState) HandleKey(k tea.Key) bool {
	return p.HandleActionKey(k)
}

func schemaPickerMaxLines(viewHeight, linesUsedBefore, minLogLines int) int {
	const minPicker = 10
	const maxPicker = 22
	remain := viewHeight - linesUsedBefore - minLogLines - 4
	if remain < minPicker {
		return minPicker
	}
	if remain > maxPicker {
		return maxPicker
	}
	return remain
}

func renderSelectAllLine(p *SchemaPickerState) string {
	mark := "[ ]"
	if p.AllSelected() {
		mark = "[x]"
	}
	return StyleBase.Render("  "+mark+" Select all") + " " + StyleMuted.Render("(a)")
}

func renderSchemaPickerLines(p *SchemaPickerState, maxLines int) []string {
	if p == nil {
		return nil
	}
	if len(p.AvailableSchemas) == 0 {
		return []string{StyleWarning.Render("No schemas found")}
	}
	if maxLines < 1 {
		maxLines = 1
	}
	p.ensureCursorVisible(maxLines)
	total := len(p.AvailableSchemas)
	start := p.PickerScrollOffset
	if start < 0 {
		start = 0
	}
	if start >= total {
		start = 0
	}
	end := start + maxLines
	if end > total {
		end = total
	}
	var lines []string
	if start > 0 {
		lines = append(lines, StyleMuted.Render(fmt.Sprintf("  … +%d above", start)))
	}
	for i := start; i < end; i++ {
		name := p.AvailableSchemas[i]
		mark := "[ ]"
		if p.SelectedSchemas != nil && p.SelectedSchemas[name] {
			mark = "[x]"
		}
		line := mark + " " + name
		if i == p.PickerCursor {
			line = StyleAccent.Render("> " + line)
		} else {
			line = StyleBase.Render("  " + line)
		}
		lines = append(lines, line)
	}
	remaining := total - end
	if remaining > 0 {
		lines = append(lines, StyleMuted.Render(fmt.Sprintf("  … +%d more", remaining)))
	}
	return lines
}

func renderSchemaPickerSummary(p *SchemaPickerState) string {
	selected := p.SelectedNames()
	if len(selected) == 0 {
		return StyleWarning.Render("No schemas selected")
	}
	return StyleMuted.Render("Selected:") + " " + StyleBase.Render(strings.Join(selected, ", "))
}
