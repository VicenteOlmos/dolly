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
	draft.History = DumpHistoryState{Entries: entries}
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
	if maxLines < 1 {
		maxLines = 1
	}
	start := 0
	if len(h.Entries) > maxLines {
		start = h.Cursor - maxLines/2
		if start < 0 {
			start = 0
		}
		if start+maxLines > len(h.Entries) {
			start = len(h.Entries) - maxLines
		}
	}
	end := start + maxLines
	if end > len(h.Entries) {
		end = len(h.Entries)
	}
	var lines []string
	for i := start; i < end; i++ {
		entry := h.Entries[i]
		if i == h.Cursor {
			lines = append(lines, StyleAccent.Render("> "+entry.Label))
		} else {
			lines = append(lines, "  "+entry.Label)
		}
	}
	return lines
}
