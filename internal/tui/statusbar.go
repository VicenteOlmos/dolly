package tui

import (
	"strings"
)

func RenderStatusBar(width int, screen Screen, statusMsg string, dumpStatus DumpStatus, cloneStatus CloneStatus, saveConnections bool) string {
	text := statusMsg
	if text == "" {
		text = defaultStatusHint(screen, dumpStatus, cloneStatus, saveConnections)
	}
	if width > 0 && len(text) > width {
		text = text[:width]
	}
	return StyleStatusBar.Width(width).Render(text)
}

func defaultStatusHint(screen Screen, dumpStatus DumpStatus, cloneStatus CloneStatus, saveConnections bool) string {
	if screen == ScreenConnection {
		if saveConnections {
			return "↑↓ section · Enter open · e/s/r/d/t · ?/F1"
		}
		return "Enter connect · Esc back · Tab field · ?/F1"
	}
	if screen == ScreenDump {
		switch dumpStatus {
		case DumpStatusRunning:
			return "Dumping… · c/Esc cancel · Ctrl+C quit"
		case DumpStatusComplete:
			return "o open · Enter again · Esc dismiss · Ctrl+C quit"
		case DumpStatusError:
			return "Failed · g/F5 retry · 1-5 · ?/F1"
		}
		return "↑↓ section · Enter open · g/F5 dump · ?/F1"
	}
	if screen == ScreenClone {
		switch cloneStatus {
		case CloneStatusRunning:
			return "Cloning… · c/Esc cancel · Ctrl+C quit"
		case CloneStatusComplete:
			return "Enter again · Esc dismiss · Ctrl+C quit"
		}
		return "↑↓ section · Enter open · g/F5 clone · ?/F1"
	}
	if screen == ScreenConfig {
		return "↑↓/j/k scroll · Enter/Space edit · auto-save on leave · ?/F1"
	}
	return "1-5 screen · ? info · F1 keys · Esc quit"
}

func statusHintSegmentCount(hint string) int {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return 0
	}
	return strings.Count(hint, "·") + 1
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	skip := false
	for _, r := range s {
		if r == '\x1b' {
			skip = true
			continue
		}
		if skip {
			if r == 'm' {
				skip = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
