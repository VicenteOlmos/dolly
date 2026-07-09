package tui

import (
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

const passwordMaskLen = 8

const spinnerInterval = 140 * time.Millisecond

type spinnerTickMsg time.Time

func scheduleSpinnerTick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func maskedPasswordDisplay(value string) string {
	if value == "" {
		return ""
	}
	return strings.Repeat("*", passwordMaskLen)
}

// maskDSNPassword redacts the password in a postgres:// DSN for display.
func maskDSNPassword(dsn string) string {
	if dsn == "" {
		return ""
	}
	return strings.ReplaceAll(connections.RedactDSN(dsn), "%2A%2A%2A", strings.Repeat("*", passwordMaskLen))
}

func clampCursor(cursor, length int) int {
	if cursor < 0 {
		return 0
	}
	if cursor > length {
		return length
	}
	return cursor
}

func fieldCursorLeft(cursor *int) {
	if *cursor > 0 {
		*cursor--
	}
}

func fieldCursorRight(cursor *int, length int) {
	if *cursor < length {
		*cursor++
	}
}

func fieldCursorHome(cursor *int) {
	*cursor = 0
}

func fieldCursorEnd(cursor *int, length int) {
	*cursor = length
}

func fieldCursorInsert(value *string, cursor *int, text string) {
	if text == "" {
		return
	}
	pos := clampCursor(*cursor, len(*value))
	*value = (*value)[:pos] + text + (*value)[pos:]
	*cursor = pos + len(text)
}

func fieldCursorBackspace(value *string, cursor *int) {
	pos := clampCursor(*cursor, len(*value))
	if pos == 0 {
		return
	}
	*value = (*value)[:pos-1] + (*value)[pos:]
	*cursor = pos - 1
}

func fieldCursorDelete(value *string, cursor *int) {
	pos := clampCursor(*cursor, len(*value))
	if pos >= len(*value) {
		return
	}
	*value = (*value)[:pos] + (*value)[pos+1:]
}

func handleFieldCursorKey(k tea.Key, value *string, cursor *int) bool {
	switch k.Code {
	case tea.KeyLeft:
		fieldCursorLeft(cursor)
		return true
	case tea.KeyRight:
		fieldCursorRight(cursor, len(*value))
		return true
	case tea.KeyHome:
		fieldCursorHome(cursor)
		return true
	case tea.KeyEnd:
		fieldCursorEnd(cursor, len(*value))
		return true
	case tea.KeyBackspace:
		fieldCursorBackspace(value, cursor)
		return true
	case tea.KeyDelete:
		fieldCursorDelete(value, cursor)
		return true
	}
	if k.Text != "" && unicode.IsPrint([]rune(k.Text)[0]) {
		fieldCursorInsert(value, cursor, k.Text)
		return true
	}
	return false
}

func isFieldCursorNavigationKey(k tea.Key) bool {
	switch k.Code {
	case tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd, tea.KeyDelete:
		return true
	default:
		return false
	}
}

func renderEditableField(value string, cursor int, masked, focused bool) string {
	display := value
	if masked {
		display = maskedPasswordDisplay(value)
	}
	if !focused {
		if display == "" {
			return ""
		}
		return display
	}
	if masked {
		if display == "" {
			return "[*]"
		}
		return display + "[*]"
	}
	cursor = clampCursor(cursor, len(value))
	if len(value) == 0 {
		return "[*]"
	}
	return value[:cursor] + "▌" + value[cursor:]
}
