package tui

import tea "charm.land/bubbletea/v2"

type KeyBinding struct {
	Key    string
	Scope  string
	Action string
}

var globalBindings = []KeyBinding{
	{Key: "1-5", Scope: "global", Action: "jump to screen"},
	{Key: "Ctrl+Tab", Scope: "global", Action: "next screen"},
	{Key: "Ctrl+Shift+Tab", Scope: "global", Action: "previous screen"},
	{Key: "g / F5", Scope: "dump/clone", Action: "start run"},
	{Key: "Ctrl+Enter", Scope: "dump/clone", Action: "start run (terminal-dependent)"},
	{Key: "?", Scope: "global", Action: "context help (screen, field, strategies)"},
	{Key: "F1", Scope: "global", Action: "keyboard shortcuts and CLI catalog"},
	{Key: "Esc", Scope: "global", Action: "close help / quit prompt"},
	{Key: "Ctrl+C", Scope: "global", Action: "quit prompt"},
	{Key: "Y/N", Scope: "modal", Action: "confirm or cancel dialog"},
}

var screenBindings = map[Screen][]KeyBinding{
	ScreenConnection: {
		{Key: "↑/↓", Scope: "connection", Action: "overview: next section · inside: field or profile"},
		{Key: "Enter", Scope: "connection", Action: "overview: open section · fields: connect · list: pick"},
		{Key: "Tab", Scope: "connection", Action: "inside fields: next field"},
		{Key: "Esc", Scope: "connection", Action: "inside: back to overview"},
		{Key: "Ctrl+T", Scope: "connection", Action: "test connection (fields or edit panel)"},
	},
	ScreenSchema: {
		{Key: "j/k", Scope: "schema", Action: "scroll table list"},
		{Key: "↑/↓", Scope: "schema", Action: "scroll table list"},
	},
	ScreenDump: {
		{Key: "↑/↓", Scope: "dump", Action: "overview: next section · inside: move in section"},
		{Key: "Enter", Scope: "dump", Action: "overview: open section"},
		{Key: "Esc", Scope: "dump", Action: "inside: back to overview"},
		{Key: "←/→", Scope: "dump", Action: "edit base directory (inside path section)"},
		{Key: "Space", Scope: "dump", Action: "toggle schema (inside schemas section)"},
		{Key: "a", Scope: "dump", Action: "select/deselect all schemas (inside schemas section)"},
		{Key: "Enter/r", Scope: "dump", Action: "restore selected dump (inside history section)"},
		{Key: "g / F5", Scope: "dump", Action: "start dump"},
		{Key: "Ctrl+Enter", Scope: "dump", Action: "start dump (terminal-dependent)"},
		{Key: "t", Scope: "dump", Action: "toggle transaction (inside path section)"},
		{Key: "c", Scope: "dump", Action: "cancel dump (while running)"},
		{Key: "Esc", Scope: "dump", Action: "cancel dump (while running)"},
	},
	ScreenClone: {
		{Key: "↑/↓", Scope: "clone", Action: "overview: next section · inside: move in section"},
		{Key: "Tab", Scope: "clone", Action: "inside fields: next field"},
		{Key: "Enter", Scope: "clone", Action: "overview: open section"},
		{Key: "Esc", Scope: "clone", Action: "inside: back to overview · cancel analyze"},
		{Key: "←/→", Scope: "clone", Action: "cycle strategy (inside fields, strategy field)"},
		{Key: "Space", Scope: "clone", Action: "toggle schema (inside schemas section) · toggle analyze (inside fields, analyze field)"},
		{Key: "a", Scope: "clone", Action: "toggle analyze preflight (inside fields, analyze field)"},
		{Key: "t", Scope: "clone", Action: "cycle target source (inside fields)"},
		{Key: "g / F5", Scope: "clone", Action: "start clone"},
		{Key: "Ctrl+Enter", Scope: "clone", Action: "start clone (terminal-dependent)"},
		{Key: "c", Scope: "clone", Action: "cancel clone (while running)"},
		{Key: "Esc", Scope: "clone", Action: "cancel clone (while running)"},
	},
	ScreenConfig: {
		{Key: "j/k", Scope: "config", Action: "scroll fields"},
		{Key: "↑/↓", Scope: "config", Action: "scroll fields"},
		{Key: "Enter", Scope: "config", Action: "edit field / toggle bool / cycle choice"},
		{Key: "Space", Scope: "config", Action: "toggle bool / cycle choice"},
		{Key: "←/→", Scope: "config", Action: "cycle choice field"},
		{Key: "Ctrl+S", Scope: "config", Action: "save config now (also auto-saves on leave)"},
		{Key: "Esc", Scope: "config", Action: "cancel edit"},
	},
}

var cloneRunningBindings = []KeyBinding{
	{Key: "j/k", Scope: "clone", Action: "scroll log"},
	{Key: "↑/↓", Scope: "clone", Action: "scroll log"},
	{Key: "c", Scope: "clone", Action: "cancel clone"},
	{Key: "Esc", Scope: "clone", Action: "cancel clone"},
	{Key: "Ctrl+C", Scope: "global", Action: "cancel clone and quit"},
}

var cloneResultBindings = []KeyBinding{
	{Key: "Enter", Scope: "clone", Action: "run again"},
	{Key: "Esc", Scope: "clone", Action: "dismiss result"},
	{Key: "j/k", Scope: "clone", Action: "scroll log"},
	{Key: "↑/↓", Scope: "clone", Action: "scroll log"},
}

var dumpRunningBindings = []KeyBinding{
	{Key: "j/k", Scope: "dump", Action: "scroll log"},
	{Key: "↑/↓", Scope: "dump", Action: "scroll log"},
	{Key: "c", Scope: "dump", Action: "cancel dump"},
	{Key: "Esc", Scope: "dump", Action: "cancel dump"},
	{Key: "Ctrl+C", Scope: "global", Action: "cancel dump and quit"},
}

var dumpResultBindings = []KeyBinding{
	{Key: "o", Scope: "dump", Action: "open output folder"},
	{Key: "Enter", Scope: "dump", Action: "run again"},
	{Key: "Esc", Scope: "dump", Action: "dismiss result"},
	{Key: "j/k", Scope: "dump", Action: "scroll file list"},
	{Key: "↑/↓", Scope: "dump", Action: "scroll file list"},
}

var connectionSavedBindings = []KeyBinding{
	{Key: "e", Scope: "connection", Action: "edit profile fields (saved list section)"},
	{Key: "s", Scope: "connection", Action: "save-as profile (saved list section)"},
	{Key: "r", Scope: "connection", Action: "rename profile (saved list section)"},
	{Key: "d", Scope: "connection", Action: "delete profile (saved list section)"},
	{Key: "t", Scope: "connection", Action: "test connection (saved list section)"},
	{Key: "*", Scope: "connection", Action: "set default profile (saved list section)"},
}

func BindingsForScreen(screen Screen, dumpStatus DumpStatus, cloneStatus CloneStatus, saveConnections bool) []KeyBinding {
	out := make([]KeyBinding, len(globalBindings))
	copy(out, globalBindings)
	if screen == ScreenClone && cloneStatus == CloneStatusComplete {
		out = append(out, cloneResultBindings...)
		return out
	}
	if screen == ScreenClone && cloneStatus == CloneStatusRunning {
		out = append(out, cloneRunningBindings...)
		return out
	}
	if screen == ScreenDump && dumpStatus == DumpStatusComplete {
		out = append(out, dumpResultBindings...)
		return out
	}
	if screen == ScreenDump && dumpStatus == DumpStatusRunning {
		out = append(out, dumpRunningBindings...)
		return out
	}
	if extra, ok := screenBindings[screen]; ok {
		out = append(out, extra...)
	}
	if screen == ScreenConnection && saveConnections {
		out = append(out, connectionSavedBindings...)
	}
	return out
}

// isCancelKey reports whether key should trigger cancel/abort for a running
// dump, clone, or analyze preflight. Ctrl+C is handled separately (quit modal).
func isCancelKey(msg tea.Msg) (tea.KeyPressMsg, bool) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return key, false
	}
	k := key.Key()
	if k.Mod != 0 {
		return key, false
	}
	switch {
	case k.String() == "c":
		return key, true
	case k.String() == "esc", k.String() == "escape":
		return key, true
	case k.Code == tea.KeyEscape:
		return key, true
	default:
		return key, false
	}
}

// runKeyTrigger reports whether key starts dump/clone. isLetterG is true for the
// "g" shortcut, which must not fire while typing in an editable text field.
func runKeyTrigger(key tea.KeyPressMsg) (trigger bool, isLetterG bool) {
	k := key.Key()
	switch k.String() {
	case "ctrl+enter":
		return true, false
	case "g":
		if k.Mod == 0 {
			return true, true
		}
	case "f5":
		return true, false
	}
	if k.Code == tea.KeyF5 {
		return true, false
	}
	return false, false
}

func RenderHelp(screen Screen, dumpStatus DumpStatus, cloneStatus CloneStatus, page, width, height int, saveConnections bool) string {
	return RenderHelpPaged(screen, dumpStatus, cloneStatus, page, width, height, saveConnections)
}
