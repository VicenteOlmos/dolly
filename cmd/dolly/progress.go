package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// isStderrTerminal reports whether fd refers to a terminal.
// Overridable in tests.
var isStderrTerminal = func(fd uintptr) bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// progressEvent is the subset of fields common to dump, restore, and clone
// ProgressEvent types. Render accepts any of the three concrete types via
// a type switch and projects them onto this shape.
type progressEvent struct {
	Phase   string
	Table   string
	Current int
	Total   int
	Elapsed time.Duration
}

// Render writes a progress bar line to w.
//
// TTY mode (isTTY=true): redraws the current line using \r, showing
// a filled/unfilled bar, percentage, ETA (when computable), and table name.
//
// Non-TTY mode (isTTY=false): writes a plain line per event, suitable
// for piped or redirected stderr.
func Render(w io.Writer, ev any, isTTY bool) error {
	var pe progressEvent
	switch e := ev.(type) {
	case dump.ProgressEvent:
		pe = progressEvent{Phase: e.Phase, Table: e.Table, Current: e.Current, Total: e.Total, Elapsed: e.Elapsed}
	case restore.ProgressEvent:
		pe = progressEvent{Phase: e.Phase, Table: e.Table, Current: e.Current, Total: e.Total, Elapsed: e.Elapsed}
	case clone.ProgressEvent:
		pe = progressEvent{Phase: e.Phase, Table: e.Table, Current: e.Current, Total: e.Total, Elapsed: e.Elapsed}
	default:
		return nil
	}

	line := formatProgressLine(pe)
	if isTTY {
		_, err := fmt.Fprintf(w, "\r%s", line)
		return err
	}
	_, err := fmt.Fprintf(w, "%s\n", line)
	return err
}

// formatProgressLine produces a single progress line from an event.
// TTY path uses \r rewrite; non-TTY path uses a plain newline (added by caller).
func formatProgressLine(pe progressEvent) string {
	if pe.Total <= 0 {
		return ""
	}

	ratio := float64(pe.Current) / float64(pe.Total)
	if ratio > 1.0 {
		ratio = 1.0
	}

	const barWidth = 40
	filled := int(ratio * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("▰", filled) + strings.Repeat("▱", barWidth-filled)
	pct := int(ratio * 100)

	etaStr := "—"
	if eta, ok := computeCLIETA(pe.Elapsed, pe.Current, pe.Total); ok {
		etaStr = formatCLIDuration(eta)
	}

	label := pe.Table
	if label == "" {
		label = pe.Phase
	}

	return fmt.Sprintf("  %3d%% %s ETA %s  %s", pct, bar, etaStr, label)
}

// computeCLIETA returns the estimated time remaining.
// Mirrors the TUI computeETA logic: guards against current < 2.
func computeCLIETA(elapsed time.Duration, current, total int) (time.Duration, bool) {
	if current < 2 || total <= 0 || current >= total {
		return 0, false
	}
	eta := time.Duration(int64(elapsed) * int64(total-current) / int64(current))
	if eta < 0 {
		return 0, false
	}
	return eta, true
}

// formatCLIDuration formats a duration into a compact human-readable string.
func formatCLIDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	totalSec := int(d.Seconds())
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
