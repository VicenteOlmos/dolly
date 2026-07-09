package tui

import (
	"fmt"
	"strings"
)

const (
	analyzeModalMaxWidth    = 76
	analyzeModalVisibleRows = 10
)

func formatAnalyzeModalBody(r analyzeResult, scroll int) string {
	var lines []string
	lines = append(lines, fmt.Sprintf(
		"Database size: %s · %d objects · ~%s rows",
		formatBytes(r.DatabaseSize),
		r.TableCount,
		formatIntComma(r.TotalRowEstimate),
	))
	lines = append(lines, fmt.Sprintf("Suggested clone name: %s", r.NextCloneName))
	lines = append(lines, "")

	if len(r.Objects) == 0 {
		lines = append(lines, "(no objects in selected schemas)")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "  OBJECT                         KIND       ROWS       SIZE")
	maxScroll := max(0, len(r.Objects)-analyzeModalVisibleRows)
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + analyzeModalVisibleRows
	if end > len(r.Objects) {
		end = len(r.Objects)
	}
	for _, obj := range r.Objects[scroll:end] {
		label := obj.Schema + "." + obj.Name
		if len(label) > 30 {
			label = label[:27] + "..."
		}
		lines = append(lines, fmt.Sprintf(
			"  %-30s %-10s %10s %10s",
			label,
			obj.Kind,
			formatIntComma(obj.RowEstimate),
			formatBytes(obj.SizeBytes),
		))
	}
	if len(r.Objects) > analyzeModalVisibleRows {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  showing %d–%d of %d · ↑/↓ scroll",
			scroll+1, end, len(r.Objects)))
	}
	return strings.Join(lines, "\n")
}

func analyzeModalMaxScroll(r analyzeResult) int {
	return max(0, len(r.Objects)-analyzeModalVisibleRows)
}
