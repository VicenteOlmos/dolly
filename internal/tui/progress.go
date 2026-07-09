package tui

import (
	"fmt"
	"strings"
	"time"
)

// DumpProgressEvent mirrors dump.ProgressEvent without importing the dump package.
type DumpProgressEvent struct {
	Phase   string
	Table   string
	Current int
	Total   int
	Elapsed time.Duration
}

// RestoreProgressEvent mirrors restore.ProgressEvent without importing the restore package.
type RestoreProgressEvent struct {
	Phase   string
	Table   string
	Current int
	Total   int
	Elapsed time.Duration
}

// computeETA returns the estimated time remaining based on linear extrapolation.
// It guards against divide-by-zero and negative values by requiring current >= 2.
func computeETA(elapsed time.Duration, current, total int) (time.Duration, bool) {
	if current < 2 || total <= 0 || current >= total {
		return 0, false
	}
	eta := time.Duration(int64(elapsed) * int64(total-current) / int64(current))
	if eta < 0 {
		return 0, false
	}
	return eta, true
}

// renderProgressBar renders a progress bar with percentage and optional ETA.
// Width defaults to 40 and is clamped to [10, 80].
func renderProgressBar(width, current, total, elapsed int64, label string) string {
	if width < 10 {
		width = 10
	}
	if width > 80 {
		width = 80
	}
	if total <= 0 {
		return ""
	}

	ratio := float64(current) / float64(total)
	if ratio > 1.0 {
		ratio = 1.0
	}

	// Reserve space: "  42% " (6) + bar (width) + " ETA 12m34s" (~14) + label
	barWidth := int(width)
	filled := int(ratio * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := StyleProgressBar.Render(strings.Repeat("▓", filled)) +
		StyleProgressTrack.Render(strings.Repeat("░", barWidth-filled))
	pct := int(ratio * 100)

	etaStr := ""
	if eta, ok := computeETA(time.Duration(elapsed), int(current), int(total)); ok {
		etaStr = " ETA " + formatDuration(eta)
	}

	return fmt.Sprintf("  %3d%% %s%s %s", pct, bar, etaStr, label)
}

// formatDuration formats a duration into a compact human-readable string.
func formatDuration(d time.Duration) string {
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
