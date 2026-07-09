package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type schemaScreen struct {
	draft      *SchemaDraft
	hasSession func() bool
}

func newSchemaScreen(draft *SchemaDraft, hasSession func() bool) ScreenModel {
	return &schemaScreen{draft: draft, hasSession: hasSession}
}

func (s *schemaScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || !s.hasSession() {
		return nil
	}
	switch key.Key().String() {
	case "j", "down":
		s.scrollTables(1)
		return nil
	case "k", "up":
		s.scrollTables(-1)
		return nil
	}
	switch key.Key().Code {
	case tea.KeyDown:
		s.scrollTables(1)
	case tea.KeyUp:
		s.scrollTables(-1)
	}
	return nil
}

func (s *schemaScreen) View(width, height int) string {
	var lines []string
	lines = append(lines, StyleHeader.Render("Schema"))
	lines = append(lines, "")
	if !s.hasSession() {
		lines = append(lines, StyleMuted.Render("Connect first (screen 1)"))
	} else {
		summary := fmt.Sprintf(
			"%d tables · %d columns · %d foreign keys",
			s.draft.TableCount,
			s.draft.ColumnCount,
			s.draft.FKCount,
		)
		lines = append(lines, StyleMuted.Render(summary))
		lines = append(lines, StyleMuted.Render("Browse only · select schemas on dump (3) or clone (4)"))
		lines = append(lines, "")
		lines = append(lines, s.renderTableLines(height)...)
	}
	content := strings.Join(lines, "\n")
	return StyleBorder.Width(max(0, width-2)).Height(max(0, height-2)).Render(content)
}

func (s *schemaScreen) scrollTables(delta int) {
	if s == nil || s.draft == nil || len(s.draft.Tables) == 0 {
		return
	}
	s.draft.TableScrollOffset += delta
	if s.draft.TableScrollOffset < 0 {
		s.draft.TableScrollOffset = 0
	}
	if s.draft.TableScrollOffset >= len(s.draft.Tables) {
		s.draft.TableScrollOffset = len(s.draft.Tables) - 1
	}
}

func (s *schemaScreen) renderTableLines(height int) []string {
	if s == nil || s.draft == nil || len(s.draft.Tables) == 0 {
		return nil
	}
	maxLines := height - 8
	if maxLines < 1 {
		maxLines = 1
	}
	total := len(s.draft.Tables)
	start := s.draft.TableScrollOffset
	if start < 0 {
		start = 0
	}
	maxOffset := total - maxLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if start > maxOffset {
		start = maxOffset
		s.draft.TableScrollOffset = start
	}
	end := start + maxLines
	if end > total {
		end = total
	}

	out := make([]string, 0, maxLines+2)
	if start > 0 {
		out = append(out, StyleMuted.Render(fmt.Sprintf("  … +%d above", start)))
	}
	for _, table := range s.draft.Tables[start:end] {
		out = append(out, StyleBase.Render("> "+table))
	}
	if end < total {
		out = append(out, StyleMuted.Render(fmt.Sprintf("  … +%d more", total-end)))
	}
	return out
}
