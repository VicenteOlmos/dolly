package tui

import (
	"strings"
)

func RenderContextHelpSplit(a *App, width, height int) string {
	body := contextHelpForApp(a, width)
	panel := StyleHeader.Render("About") + "\n\n" + body + "\n\n" + StyleMuted.Render("? close · Esc close")
	box := StyleHelpPanel.Width(max(0, width-2)).Height(max(1, height-2)).Render(truncateHelpLines(panel, height))
	return box
}

func contextHelpForApp(a *App, width int) string {
	switch a.screen {
	case ScreenConnection:
		if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
			return cs.contextHelp(width)
		}
	case ScreenSchema:
		if ss, ok := a.screens[ScreenSchema].(*schemaScreen); ok {
			return ss.contextHelp(width)
		}
	case ScreenDump:
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			return ds.contextHelp(width)
		}
	case ScreenClone:
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			return cs.contextHelp(width)
		}
	case ScreenConfig:
		if cs, ok := a.screens[ScreenConfig].(*configScreen); ok {
			return cs.contextHelp(width)
		}
	}
	return wrapText("Context help for the active screen. Press F1 for keyboard shortcuts and CLI reference.", width)
}

func renderCloneStrategiesHelp(width int) string {
	var lines []string
	lines = append(lines, StyleMuted.Render("Logical (single database):"))
	for _, opt := range cloneStrategyOptions {
		lines = append(lines, "")
		lines = append(lines, StyleAccent.Render(opt.Name))
		lines = append(lines, wrapText(opt.Description, width))
	}
	return strings.Join(lines, "\n")
}

func (c *connectionScreen) contextHelp(width int) string {
	if !c.saveConnections {
		return wrapText("Connect to PostgreSQL using host, port, database, user, and password. After connecting, use screen 2 to browse schema and screens 3–4 for dump or clone.", width)
	}
	if c.nav.InOverview() {
		return wrapText("Connection workflow: enter credentials in Fields, or pick a saved profile from Saved. Saved profiles support edit (e), save-as (s), rename (r), delete (d), and test (t).", width)
	}
	switch c.nav.Section {
	case connSectionFields:
		if c.focus >= 0 && c.focus < len(c.fields) {
			return connectionFieldHelp(c.fields[c.focus].label, width)
		}
		return wrapText("PostgreSQL connection parameters for the source database.", width)
	case connSectionList:
		return wrapText("Saved connection profiles store host, credentials, and default schemas. ↑/↓ previews the highlighted profile in the fields above; Enter connects. Letter keys: edit (e), save-as (s), rename (r), delete (d), test (t), default (*). The default profile (★) is pre-selected when you open connect.", width)
	default:
		return wrapText("Connection settings for the source PostgreSQL instance.", width)
	}
}

func connectionFieldHelp(label string, width int) string {
	switch label {
	case "Host":
		return wrapText("PostgreSQL server hostname or IP address.", width)
	case "Port":
		return wrapText("PostgreSQL port (default 5432).", width)
	case "Database":
		return wrapText("Database name to connect to on the source server.", width)
	case "User":
		return wrapText("PostgreSQL role used for dump, clone, and schema introspection.", width)
	case "Password":
		return wrapText("Password for the PostgreSQL role. Masked in the UI.", width)
	default:
		return wrapText("Connection field: "+label, width)
	}
}

func (s *schemaScreen) contextHelp(width int) string {
	return wrapText("Read-only table browser for the connected database. Schema selection for dump and clone happens on screens 3 and 4 — this screen is for inspection only.", width)
}

func (d *dumpScreen) contextHelp(width int) string {
	if d.nav.InOverview() {
		return wrapText("Export selected schemas to a numbered directory under the output base. Optionally restore a previous dump from history without leaving the TUI.", width)
	}
	switch d.nav.Section {
	case dumpSectionPath:
		return wrapText("Output base directory: each run creates {base}/{n}. Transaction on wraps the dump in a read-only transaction; turn off for large subset closures.", width)
	case dumpSectionPicker:
		return wrapText("Schemas included in the dump. Space toggles a schema; a selects or clears all. At least one schema is required to start.", width)
	case dumpSectionHistory:
		return wrapText("Previously completed dumps under the output base. Enter or r restores the highlighted dump into the current connection using the same restore seam as dolly restore.", width)
	case dumpSectionLog:
		return wrapText("Progress and table-level messages from the current or last dump run.", width)
	default:
		return wrapText("Dump workflow sections: output path, schemas, history restore, and log.", width)
	}
}

func (c *cloneScreen) contextHelp(width int) string {
	if c.nav.InOverview() {
		return wrapText("Clone copies selected schemas to a target database using the chosen strategy. Configure target and strategy, pick schemas, then g or Ctrl+Enter to start.", width)
	}
	switch c.nav.Section {
	case cloneSectionForm:
		return c.cloneFormContextHelp(width)
	case cloneSectionPicker:
		return wrapText("Schemas copied to the target. Selection is persisted to the active saved profile when save_connections is enabled.", width)
	case cloneSectionLog:
		return wrapText("Step messages and progress from the current or last clone run.", width)
	default:
		return wrapText("Clone workflow: target settings, schema selection, and log.", width)
	}
}

func (c *cloneScreen) cloneFormContextHelp(width int) string {
	switch c.formField {
	case 0:
		return wrapText(cloneFormFieldHints[0]+". Auto-filled from analyze when enabled.", width)
	case 1:
		return wrapText(cloneFormFieldHints[1]+". Saved uses a stored profile DSN; Manual lets you type a full connection string.", width)
	case 2:
		return renderCloneStrategiesHelp(width)
	case 3:
		return wrapText(cloneFormFieldHints[3]+". Runs before clone when enabled and gates pass; Esc cancels analyze.", width)
	default:
		return wrapText("Target database name, destination DSN, clone strategy, and optional analyze preflight.", width)
	}
}

func (cs *configScreen) contextHelp(width int) string {
	if cs.cursor < 0 || cs.cursor >= len(cs.fields) {
		return wrapText("Edit config.jsonc values in place. Changes save automatically when you leave this screen (another screen key or Ctrl+Tab). Ctrl+S saves immediately. Array fields like clone.schemas are read-only here.", width)
	}
	f := cs.fields[cs.cursor]
	var lines []string
	lines = append(lines, StyleAccent.Render(f.Section+"."+f.Label))
	if f.Hint != "" {
		lines = append(lines, wrapText(f.Hint, width))
	} else {
		lines = append(lines, wrapText("Configuration knob from config.jsonc.", width))
	}
	if f.Kind == fieldKindChoice && f.Label == "strategy" {
		lines = append(lines, "")
		lines = append(lines, renderCloneStrategiesHelp(width))
	}
	if f.Kind == fieldKindChoice && len(f.Choices) > 0 && f.Label != "strategy" {
		lines = append(lines, "")
		lines = append(lines, StyleMuted.Render("Choices: "+strings.Join(f.Choices, ", ")))
	}
	return strings.Join(lines, "\n")
}
