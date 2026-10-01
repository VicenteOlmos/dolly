package tui

import (
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/dumphistory"
)

func refreshDumpHistory(draft *DumpDraft, store dumphistory.Store) {
	if draft == nil {
		return
	}
	if draft.OutputDir == "" {
		draft.History = DumpHistoryState{}
		return
	}
	if store == nil {
		return
	}
	recs, err := dumphistory.ListBaseMerged(draft.OutputDir, store)
	if err != nil {
		draft.History = DumpHistoryState{}
		return
	}
	prev := draft.History
	entries := make([]DumpHistoryEntry, 0, len(recs))
	for _, r := range recs {
		entries = append(entries, DumpHistoryEntry{
			Seq:         r.Seq,
			Path:        r.Path,
			Label:       formatDumpHistoryLabel(r),
			Schemas:     append([]string(nil), r.Schemas...),
			TableCount:  r.TableCount,
			RowEstimate: r.RowEstimate,
		})
	}
	draft.History = DumpHistoryState{
		Entries:       entries,
		Cursor:        prev.Cursor,
		Filter:        prev.Filter,
		FilterDraft:   prev.FilterDraft,
		FilterEditing: prev.FilterEditing,
	}
	draft.History.clampCursorToVisible()
}

func formatDumpHistoryLabel(r dumphistory.Record) string {
	schema := r.SchemaLabel
	if schema == "" {
		schema = "?"
	}
	parts := []string{
		fmt.Sprintf("#%d", r.Seq),
		schema,
		fmt.Sprintf("%d tables", r.TableCount),
	}
	if !r.CreatedAt.IsZero() {
		parts = append(parts, r.CreatedAt.Format("Jan 2"))
	}
	if r.SourceDatabase != "" {
		parts = append(parts, r.SourceDatabase)
	}
	return strings.Join(parts, " · ")
}

func renderDumpHistoryLines(h *DumpHistoryState, maxLines int) []string {
	if h == nil || len(h.Entries) == 0 {
		return []string{StyleMuted.Render("  (no dumps yet — run a dump to build history)")}
	}
	visible := h.visibleIndices()
	if len(visible) == 0 {
		return []string{StyleMuted.Render("  no matching dumps")}
	}
	if maxLines < 1 {
		maxLines = 1
	}
	cursorPos := 0
	for i, idx := range visible {
		if idx == h.Cursor {
			cursorPos = i
			break
		}
	}
	start := 0
	if len(visible) > maxLines {
		start = cursorPos - maxLines/2
		if start < 0 {
			start = 0
		}
		if start+maxLines > len(visible) {
			start = len(visible) - maxLines
		}
	}
	end := start + maxLines
	if end > len(visible) {
		end = len(visible)
	}
	var lines []string
	for _, idx := range visible[start:end] {
		entry := h.Entries[idx]
		if idx == h.Cursor {
			lines = append(lines, StyleAccent.Render("> "+entry.Label))
		} else {
			lines = append(lines, "  "+entry.Label)
		}
	}
	return lines
}
