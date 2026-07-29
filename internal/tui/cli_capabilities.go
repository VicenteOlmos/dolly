package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type ShellPolicy int

const (
	ShellPolicyInTUI ShellPolicy = iota
	ShellPolicyCLIOnly
)

type CLIFlag struct {
	Name        string
	Required    bool
	Default     string
	Description string
	SubsetOnly  bool
}

type CLICommand struct {
	Name        string
	Short       string
	ShellPolicy ShellPolicy
	Flags       []CLIFlag
	Examples    []string
	ConfigNote  string
}

func CLICatalog() []CLICommand {
	return []CLICommand{
		{
			Name:        "dump",
			Short:       "export PostgreSQL database to NDJSON",
			ShellPolicy: ShellPolicyInTUI,
			Flags: []CLIFlag{
				{Name: "dsn", Description: "PostgreSQL connection string (or use --connection when save_connections is enabled)"},
				{Name: "connection", Description: "saved connection profile name (requires save_connections in config.jsonc)"},
				{Name: "output", Description: "output directory (or config dump.output_dir)"},
				{Name: "schemas", Description: "comma-separated source schema names (overrides saved profile and dump.schemas; default public)"},
				{Name: "no-transaction", Description: "skip read-only transaction wrapper (recommended for large subset closures)"},
				{Name: "seed-file", Description: "JSON seed file for subset dump (omit for full-schema dump; conflicts with --percent/--slow-connection)"},
				{Name: "percent", Description: "percent-based subset dump (1-100). Samples recent root rows, then FK closure may exceed percent. Conflicts with --seed-file/--slow-connection"},
				{Name: "slow-connection", Description: "stream in resumable chunks; incompatible with subset modes"},
				{Name: "chunk-size", Description: "rows per slow-connection chunk"},
				{Name: "retry-max", Description: "slow-connection retry attempts"},
				{Name: "retry-base", Description: "slow-connection retry delay"},
				{Name: "max-depth", Description: "subset max FK closure depth (default 10)", SubsetOnly: true},
				{Name: "max-tables", Description: "subset max tables in closure (default 50)", SubsetOnly: true},
				{Name: "max-rows", Description: "subset max rows read during planning (default 100000)", SubsetOnly: true},
				{Name: "max-rows-per-table", Description: "subset max rows exported per table (0 = unlimited)", SubsetOnly: true},
				{Name: "max-in-list-size", Description: "subset max values per IN/ANY batch (default 500)", SubsetOnly: true},
				{Name: "include-table", Description: "exact qualified table to include (repeatable; narrows dump; no globs or CSV)"},
				{Name: "exclude-table", Description: "exact qualified table to exclude (repeatable; wins over include; no globs or CSV)"},
				{Name: "include-table-file", Description: "newline-delimited include table file (repeatable; # comments and blank lines ignored)"},
				{Name: "exclude-table-file", Description: "newline-delimited exclude table file (repeatable; # comments and blank lines ignored)"},
				{Name: "chunk-table", Description: "exact qualified table to stream with keyset chunking (repeatable; no globs or CSV)"},
				{Name: "chunk-table-file", Description: "newline-delimited chunk table file (repeatable; # comments and blank lines ignored)"},
				{Name: "workers", Default: "1", Description: "parallel table dump workers (max 16; incompatible with chunk/slow/subset/no-transaction)"},
				{Name: "json", Description: "emit machine-readable JSON result to stdout"},
			},
			Examples: []string{
				"dolly dump --dsn \"$DATABASE_URL\" --output ./out",
				"dolly dump --dsn \"$DATABASE_URL\" --output ./subset --seed-file seeds.json",
				"dolly dump --dsn \"$DATABASE_URL\" --output ./out --include-table public.users --exclude-table public.audit_log",
				"dolly dump --dsn \"$DATABASE_URL\" --output ./out --chunk-table public.orders",
				"dolly dump --dsn \"$DATABASE_URL\" --output ./out --workers 4",
				"dolly dump list",
				"dolly dump list --json --output ./dolly_dump",
			},
		},
		{
			Name:        "restore",
			Short:       "load dump artifacts into PostgreSQL",
			ShellPolicy: ShellPolicyCLIOnly,
			Flags: []CLIFlag{
				{Name: "dsn", Description: "PostgreSQL connection string (or use --connection when save_connections is enabled)"},
				{Name: "connection", Description: "saved connection profile name (requires save_connections in config.jsonc)"},
				{Name: "input", Required: true, Description: "dump input directory"},
				{Name: "on-conflict", Default: "error", Description: "row conflict policy: error, skip, upsert"},
				{Name: "replace", Description: "truncate tables before insert (destructive)"},
				{Name: "no-transaction", Description: "advanced: commit after each table (no global rollback; requires --yes; default is atomic)"},
				{Name: "trust-schema-sql", Description: "replay reviewed schema.sql when target tables are missing (requires --no-transaction --yes; default off)"},
				{Name: "yes", Description: "confirm destructive or advanced operations (required with --replace/--no-transaction)"},
				{Name: "workers", Default: "1", Description: "parallel table restore workers (max 16; requires --no-transaction --yes --ack-partial-state; TUI history restore stays serial)"},
				{Name: "ack-partial-state", Description: "acknowledge partial-state risk for parallel restore (CLI-only; never stored in config)"},
				{Name: "partial-state-file", Description: "partial-state manifest path (default: config restore.partial_state_file or input/.dolly-restore-partial-state.json)"},
				{Name: "json", Description: "emit machine-readable JSON result to stdout"},
			},
			ConfigNote: "Reads config.jsonc for restore.workers and restore.partial_state_file. Parallel acknowledgement is CLI-only.",
			Examples: []string{
				"dolly restore --dsn \"$DATABASE_URL\" --input ./out",
				"dolly restore --dsn \"$DATABASE_URL\" --input ./out --on-conflict upsert",
				"dolly restore --dsn \"$DATABASE_URL\" --input ./out --workers 4 --no-transaction --yes --ack-partial-state",
			},
		},
		{
			Name:        "tui",
			Short:       "interactive terminal cockpit",
			ShellPolicy: ShellPolicyInTUI,
		},
		{
			Name:        "clone",
			Short:       "interactive dump + restore",
			ShellPolicy: ShellPolicyInTUI,
			Flags: []CLIFlag{
				{Name: "ff", Description: "fast-forward: skip prompts and use config defaults"},
				{Name: "strategy", Description: "clone strategy: template, schema-replay, logical-stream (large single-DB), physical-backup"},
				{Name: "target-dir", Description: "target data directory for physical-backup clone (pg_basebackup -D)"},
				{Name: "connection", Description: "saved connection profile as source (requires save_connections; use with -ff)"},
				{Name: "schemas", Description: "comma-separated source schema names (overrides clone.schemas config)"},
				{Name: "yes", Description: "confirm destructive operations (required with -ff when clone.replace=true)"},
				{Name: "json", Description: "emit machine-readable JSON result to stdout"},
			},
			ConfigNote: "Reads config.jsonc for env.*, clone.target_url, clone.target_dir, clone.name_template, clone.schemas, clone.replace, clone.restore_on_conflict, and related keys.",
			Examples: []string{
				"dolly clone",
				"dolly clone -ff",
			},
		},
	}
}

func HelpPageCount() int {
	return 1 + len(CLICatalog())
}

func FlagNames(cmdName string) []string {
	for _, cmd := range CLICatalog() {
		if cmd.Name != cmdName {
			continue
		}
		names := make([]string, len(cmd.Flags))
		for i, f := range cmd.Flags {
			names[i] = f.Name
		}
		return names
	}
	return nil
}

func RenderCapabilitiesStrip(width int) string {
	var text string
	switch {
	case width >= 72:
		text = "U@U dump · restore · clone · tui — export · load · clone workflow · cockpit"
	case width >= 60:
		text = "U@U dump · restore · clone · tui · history restore"
	default:
		text = "U@U dump|restore|clone|tui · F1"
	}
	if width > 0 {
		text = truncateRunes(text, width)
	}
	return StyleCapabilitiesStrip.Width(width).Render(text)
}

func RenderCLIHelp(cmd CLICommand, width int) string {
	var lines []string
	title := cmd.Name
	switch {
	case cmd.Name == "restore":
		title += "  " + StyleMuted.Render("(CLI + TUI history restore)")
	case cmd.ShellPolicy == ShellPolicyCLIOnly:
		title += "  " + StyleMuted.Render("(shell only — not run from TUI)")
	default:
		title += "  " + StyleMuted.Render("(you are here)")
	}
	lines = append(lines, StyleHeader.Render(title))
	lines = append(lines, StyleBase.Render(cmd.Short))
	if len(cmd.Flags) > 0 {
		lines = append(lines, "")
		lines = append(lines, StyleHeader.Render("Flags"))
		for _, f := range cmd.Flags {
			lines = append(lines, renderFlagLine(f, width))
		}
	}
	if cmd.ConfigNote != "" {
		lines = append(lines, "")
		lines = append(lines, StyleHeader.Render("Config"))
		lines = append(lines, StyleBase.Render(wrapText(cmd.ConfigNote, width)))
	}
	if len(cmd.Examples) > 0 {
		lines = append(lines, "")
		lines = append(lines, StyleHeader.Render("shell: examples"))
		for _, ex := range cmd.Examples {
			lines = append(lines, StyleAccent.Render(ex))
		}
	}
	return strings.Join(lines, "\n")
}

func renderFlagLine(f CLIFlag, width int) string {
	name := "--" + f.Name
	if f.Required {
		name += "*"
	}
	meta := []string{}
	if f.Default != "" {
		meta = append(meta, "default="+f.Default)
	}
	if f.SubsetOnly {
		meta = append(meta, "CLI-only with --seed-file or --percent")
	}
	desc := f.Description
	if len(meta) > 0 {
		desc += " (" + strings.Join(meta, "; ") + ")"
	}
	line := StyleAccent.Render(name) + "  " + StyleBase.Render(desc)
	if width > 0 && len([]rune(stripANSI(line))) > width {
		return StyleBase.Render(truncateRunes(stripANSI(line), width))
	}
	return line
}

func RenderHelpPaged(screen Screen, dumpStatus DumpStatus, cloneStatus CloneStatus, page, width, height int, saveConnections bool) string {
	return RenderHelpSplit(screen, dumpStatus, cloneStatus, page, width, height, saveConnections)
}

func RenderHelpSplit(screen Screen, dumpStatus DumpStatus, cloneStatus CloneStatus, page, totalWidth, height int, saveConnections bool) string {
	total := HelpPageCount()
	if page < 0 {
		page = 0
	}
	if page >= total {
		page = total - 1
	}

	leftW := totalWidth / 2
	if leftW < 24 {
		leftW = 24
	}
	rightW := totalWidth - leftW - 1
	if rightW < 20 {
		rightW = 20
		leftW = max(20, totalWidth-rightW-1)
	}

	left := renderBindingsHelp(screen, dumpStatus, cloneStatus, saveConnections)
	var right string
	if page == 0 {
		right = StyleHeader.Render("CLI catalog") + "\n" +
			StyleMuted.Render("n/p pages · dump · restore · clone · tui") + "\n\n" +
			helpPageFooter(page, total)
	} else {
		right = RenderCLIHelp(CLICatalog()[page-1], rightW) + "\n" + helpPageFooter(page, total)
	}

	leftBox := StyleHelpPanel.Width(max(0, leftW-2)).Height(max(1, height-2)).Render(truncateHelpLines(left, height))
	rightBox := StyleHelpPanel.Width(max(0, rightW-2)).Height(max(1, height-2)).Render(truncateHelpLines(right, height))
	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
}

func truncateHelpLines(content string, maxLines int) string {
	if maxLines < 2 {
		maxLines = 2
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	lines = lines[:maxLines]
	lines[len(lines)-1] = truncateRunes(stripANSI(lines[len(lines)-1]), 40) + "…"
	return strings.Join(lines, "\n")
}

func renderBindingsHelp(screen Screen, dumpStatus DumpStatus, cloneStatus CloneStatus, saveConnections bool) string {
	var lines []string
	lines = append(lines, StyleHeader.Render("Keyboard Help"))
	switch screen {
	case ScreenConnection, ScreenDump, ScreenClone:
		lines = append(lines, StyleMuted.Render("↑/↓ section · Enter open · Esc back · Tab within · g/F5 run (dump/clone)"))
		lines = append(lines, "")
	}
	for _, b := range BindingsForScreen(screen, dumpStatus, cloneStatus, saveConnections) {
		line := StyleMuted.Render(b.Key) + "  " + StyleBase.Render("["+b.Scope+"]") + "  " + b.Action
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func helpPageFooter(page, total int) string {
	pageNum := page + 1
	kind := "Keys"
	if page > 0 {
		kind = "CLI"
	}
	return StyleMuted.Render(fmt.Sprintf("F1/Esc close · n/p pages · %d/%d %s", pageNum, total, kind))
}

func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() == 0 {
			cur.WriteString(w)
			continue
		}
		if cur.Len()+1+len(w) > width {
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(w)
			continue
		}
		cur.WriteString(" ")
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}
