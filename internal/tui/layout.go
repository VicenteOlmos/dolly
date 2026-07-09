package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// NavContentWidth is the usable text width inside the sidebar border and padding.
func NavContentWidth() int {
	// StyleBorder uses Width(ScreenNavWidth-2) with Padding(0,1): text gets 2 less.
	return max(0, ScreenNavWidth-4)
}

// centerBlock centers a multi-line block as one unit (lines share the same axis).
func centerBlock(lines []string, width int) []string {
	if width <= 0 || len(lines) == 0 {
		return lines
	}
	trimmed := make([]string, len(lines))
	for i, line := range lines {
		// Cowsay art includes leading spaces; strip so we center the glyph block once.
		trimmed[i] = strings.TrimLeft(line, " ")
	}
	lines = trimmed
	maxW := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxW {
			maxW = w
		}
	}
	if maxW > width {
		return lines
	}
	blockPad := (width - maxW) / 2
	out := make([]string, len(lines))
	for i, line := range lines {
		linePad := blockPad + (maxW-lipgloss.Width(line))/2
		out[i] = strings.Repeat(" ", linePad) + line
	}
	return out
}

func centerLine(line string, width int) string {
	return centerBlock([]string{line}, width)[0]
}

func centerLines(scene []string, width int) []string {
	return centerBlock(scene, width)
}

// stylePaddedLine applies style only to the trimmed text, keeping padding spaces
// outside ANSI sequences so lipgloss borders preserve visual centering.
func stylePaddedLine(line string, style lipgloss.Style) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line
	}
	start := strings.Index(line, trimmed)
	if start < 0 {
		return style.Render(line)
	}
	end := start + len(trimmed)
	return line[:start] + style.Render(trimmed) + line[end:]
}
