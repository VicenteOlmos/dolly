package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

type saveConfigRequestedMsg struct{}

type fieldKind int

const (
	fieldKindString fieldKind = iota
	fieldKindBool
	fieldKindInt
	fieldKindDuration
	fieldKindChoice
)

type configField struct {
	Section string
	Label   string
	Hint    string
	Kind    fieldKind
	Choices []string
	Get     func(*config.Config) string
	Set     func(*config.Config, string) error
}

func buildConfigFields() []configField {
	return []configField{
		// env section
		{Section: "env", Label: "path", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Env.Path },
			Set: func(c *config.Config, v string) error { c.Env.Path = v; return nil }},
		{Section: "env", Label: "url_var", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Env.URLVar },
			Set: func(c *config.Config, v string) error { c.Env.URLVar = v; return nil }},
		{Section: "env", Label: "host_var", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Env.HostVar },
			Set: func(c *config.Config, v string) error { c.Env.HostVar = v; return nil }},
		{Section: "env", Label: "port_var", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Env.PortVar },
			Set: func(c *config.Config, v string) error { c.Env.PortVar = v; return nil }},
		{Section: "env", Label: "name_var", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Env.NameVar },
			Set: func(c *config.Config, v string) error { c.Env.NameVar = v; return nil }},
		{Section: "env", Label: "user_var", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Env.UserVar },
			Set: func(c *config.Config, v string) error { c.Env.UserVar = v; return nil }},
		{Section: "env", Label: "password_var", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Env.PasswordVar },
			Set: func(c *config.Config, v string) error { c.Env.PasswordVar = v; return nil }},

		// clone section
		{Section: "clone", Label: "name_template", Kind: fieldKindString,
			Hint: "Go template for auto-generated clone database names (CLI -ff).",
			Get:  func(c *config.Config) string { return c.Clone.NameTemplate },
			Set:  func(c *config.Config, v string) error { c.Clone.NameTemplate = v; return nil }},
		{Section: "clone", Label: "target_url", Kind: fieldKindString,
			Hint: "Default target PostgreSQL DSN for clone when not set in the TUI.",
			Get:  func(c *config.Config) string { return c.Clone.TargetURL },
			Set:  func(c *config.Config, v string) error { c.Clone.TargetURL = v; return nil }},
		{Section: "clone", Label: "dump_dir", Kind: fieldKindString,
			Hint: "Scratch directory for schema-replay intermediate dump files.",
			Get:  func(c *config.Config) string { return c.Clone.DumpDir },
			Set:  func(c *config.Config, v string) error { c.Clone.DumpDir = v; return nil }},
		{Section: "clone", Label: "restore_on_conflict", Kind: fieldKindString,
			Hint: "Row conflict policy during clone restore: error, skip, or upsert.",
			Get:  func(c *config.Config) string { return c.Clone.RestoreOnConflict },
			Set:  func(c *config.Config, v string) error { c.Clone.RestoreOnConflict = v; return nil }},
		{Section: "clone", Label: "replace", Kind: fieldKindBool,
			Hint: "Truncate target tables before insert (destructive).",
			Get:  func(c *config.Config) string { return fmt.Sprintf("%v", c.Clone.Replace) },
			Set: func(c *config.Config, v string) error {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid bool %q", v)
				}
				c.Clone.Replace = b
				return nil
			}},
		{Section: "clone", Label: "skip_create", Kind: fieldKindBool,
			Hint: "Skip CREATE DATABASE; target database must already exist.",
			Get:  func(c *config.Config) string { return fmt.Sprintf("%v", c.Clone.SkipCreate) },
			Set: func(c *config.Config, v string) error {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid bool %q", v)
				}
				c.Clone.SkipCreate = b
				return nil
			}},
		{Section: "clone", Label: "strategy", Kind: fieldKindChoice,
			Hint:    "How data is copied to the target. Press ? for strategy details.",
			Choices: cloneStrategyChoices,
			Get:     func(c *config.Config) string { return c.Clone.Strategy },
			Set: func(c *config.Config, v string) error {
				for _, opt := range cloneStrategyChoices {
					if v == opt {
						c.Clone.Strategy = v
						return nil
					}
				}
				return fmt.Errorf("invalid strategy %q", v)
			}},

		// clone.preflight section
		{Section: "clone.preflight", Label: "cache_permissions", Kind: fieldKindBool,
			Get: func(c *config.Config) string { return fmt.Sprintf("%v", c.Clone.Preflight.CachePermissions) },
			Set: func(c *config.Config, v string) error {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid bool %q", v)
				}
				c.Clone.Preflight.CachePermissions = b
				return nil
			}},
		{Section: "clone.preflight", Label: "cache_permissions_path", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Clone.Preflight.CachePermissionsPath },
			Set: func(c *config.Config, v string) error {
				c.Clone.Preflight.CachePermissionsPath = v
				return nil
			}},
		{Section: "clone.preflight", Label: "cache_permissions_ttl", Kind: fieldKindDuration,
			Get: func(c *config.Config) string { return c.Clone.Preflight.CachePermissionsTTL },
			Set: func(c *config.Config, v string) error {
				if _, err := time.ParseDuration(v); err != nil {
					return fmt.Errorf("invalid duration %q", v)
				}
				c.Clone.Preflight.CachePermissionsTTL = v
				return nil
			}},

		// sanitization section
		{Section: "sanitization", Label: "enabled", Kind: fieldKindBool,
			Hint: "Redact sensitive columns during schema-replay clone dumps only.",
			Get:  func(c *config.Config) string { return fmt.Sprintf("%v", c.Sanitization.Enabled) },
			Set: func(c *config.Config, v string) error {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid bool %q", v)
				}
				c.Sanitization.Enabled = b
				return nil
			}},

		// subset section
		{Section: "subset", Label: "percent", Kind: fieldKindInt,
			Get: func(c *config.Config) string { return fmt.Sprintf("%d", c.Subset.Percent) },
			Set: func(c *config.Config, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil {
					return fmt.Errorf("invalid integer %q", v)
				}
				c.Subset.Percent = n
				return nil
			}},
		{Section: "subset", Label: "seed_file", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Subset.SeedFile },
			Set: func(c *config.Config, v string) error { c.Subset.SeedFile = v; return nil }},

		// top-level
		{Section: "top", Label: "save_connections", Kind: fieldKindBool,
			Get: func(c *config.Config) string { return fmt.Sprintf("%v", c.SaveConnections) },
			Set: func(c *config.Config, v string) error {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid bool %q", v)
				}
				c.SaveConnections = b
				return nil
			}},

		// tui section
		{Section: "tui", Label: "theme", Kind: fieldKindChoice,
			Hint:    "TUI palette — match your Ghostty/terminal theme. Saved when you leave config.",
			Choices: ThemeNames(),
			Get:     func(c *config.Config) string { return NormalizeTheme(c.TUI.Theme) },
			Set: func(c *config.Config, v string) error {
				name := NormalizeTheme(v)
				for _, opt := range ThemeNames() {
					if name == opt {
						c.TUI.Theme = name
						InitStyles(name)
						return nil
					}
				}
				return fmt.Errorf("invalid theme %q", v)
			}},
		{Section: "tui", Label: "section_entry", Kind: fieldKindString,
			Get: func(c *config.Config) string {
				if c.TUI.SectionEntry == "" {
					return "inside"
				}
				return c.TUI.SectionEntry
			},
			Set: func(c *config.Config, v string) error {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "overview", "inside":
					c.TUI.SectionEntry = strings.ToLower(strings.TrimSpace(v))
					return nil
				default:
					return fmt.Errorf("section_entry must be overview or inside, got %q", v)
				}
			}},

		// connections section
		{Section: "connections", Label: "scope", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Connections.Scope },
			Set: func(c *config.Config, v string) error { c.Connections.Scope = v; return nil }},
		{Section: "connections", Label: "path", Kind: fieldKindString,
			Get: func(c *config.Config) string { return c.Connections.Path },
			Set: func(c *config.Config, v string) error { c.Connections.Path = v; return nil }},
		{Section: "connections", Label: "encrypt", Kind: fieldKindBool,
			Get: func(c *config.Config) string { return fmt.Sprintf("%v", c.Connections.Encrypt) },
			Set: func(c *config.Config, v string) error {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid bool %q", v)
				}
				c.Connections.Encrypt = b
				return nil
			}},
		{Section: "connections", Label: "default", Kind: fieldKindString,
			Hint: "Saved profile name pre-selected on the TUI connect screen (set with * in the list).",
			Get:  func(c *config.Config) string { return c.Connections.Default },
			Set:  func(c *config.Config, v string) error { c.Connections.Default = strings.TrimSpace(v); return nil }},

		// dump section
		{Section: "dump", Label: "output_dir", Kind: fieldKindString,
			Hint: "Default base directory for numbered dump output folders.",
			Get:  func(c *config.Config) string { return c.Dump.OutputDir },
			Set:  func(c *config.Config, v string) error { c.Dump.OutputDir = v; return nil }},
	}
}

type configScreen struct {
	getCfg     func() *config.Config
	getPath    func() string
	fields     []configField
	cursor     int
	offset     int
	editing    bool
	editValue  string
	editCursor int
	editErr    string
	dirty      bool
}

func newConfigScreen(getCfg func() *config.Config, getPath func() string) ScreenModel {
	return &configScreen{
		getCfg:  getCfg,
		getPath: getPath,
		fields:  buildConfigFields(),
	}
}

func (cs *configScreen) Init() tea.Cmd {
	return nil
}

func (cs *configScreen) onFieldCursorNavigation() bool {
	return cs.editing
}

func (cs *configScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	k := key.Key()

	if cs.editing {
		switch k.String() {
		case "esc", "escape":
			cs.editing = false
			cs.editErr = ""
			return nil
		case "enter":
			return cs.confirmEdit()
		case "ctrl+s":
			return cs.requestSave()
		}
		switch k.Code {
		case tea.KeyEscape:
			cs.editing = false
			cs.editErr = ""
			return nil
		case tea.KeyEnter:
			return cs.confirmEdit()
		}
		// Clear transient error on any other keystroke so user sees live feedback
		cs.editErr = ""
		handleFieldCursorKey(k, &cs.editValue, &cs.editCursor)
		return nil
	}

	switch k.String() {
	case "j", "down":
		if cs.cursor < len(cs.fields)-1 {
			cs.cursor++
		}
		return nil
	case "k", "up":
		if cs.cursor > 0 {
			cs.cursor--
		}
		return nil
	case "enter":
		return cs.handleEnter()
	case " ":
		return cs.handleSpace()
	case "left", "h":
		if cs.fields[cs.cursor].Kind == fieldKindChoice {
			return cs.cycleChoice(cs.fields[cs.cursor], false)
		}
		return nil
	case "right", "l":
		if cs.fields[cs.cursor].Kind == fieldKindChoice {
			return cs.cycleChoice(cs.fields[cs.cursor], true)
		}
		return nil
	case "ctrl+s":
		return cs.requestSave()
	}
	switch k.Code {
	case tea.KeyDown:
		if cs.cursor < len(cs.fields)-1 {
			cs.cursor++
		}
	case tea.KeyUp:
		if cs.cursor > 0 {
			cs.cursor--
		}
	case tea.KeyEnter:
		return cs.handleEnter()
	}
	return nil
}

func (cs *configScreen) commitPendingEdit() error {
	if !cs.editing {
		return nil
	}
	f := cs.fields[cs.cursor]
	if f.Set == nil {
		cs.editing = false
		cs.editErr = ""
		return nil
	}
	if err := f.Set(cs.getCfg(), cs.editValue); err != nil {
		cs.editErr = err.Error()
		return err
	}
	cs.editing = false
	cs.editErr = ""
	cs.dirty = true
	return nil
}

func (cs *configScreen) confirmEdit() tea.Cmd {
	_ = cs.commitPendingEdit()
	return nil
}

func (cs *configScreen) requestSave() tea.Cmd {
	if err := cs.commitPendingEdit(); err != nil {
		return nil
	}
	return func() tea.Msg { return saveConfigRequestedMsg{} }
}

func (cs *configScreen) handleEnter() tea.Cmd {
	f := cs.fields[cs.cursor]
	if f.Kind == fieldKindChoice {
		return cs.cycleChoice(f, true)
	}
	if f.Kind == fieldKindBool {
		return cs.toggleBool(f)
	}
	cfg := cs.getCfg()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	cs.editValue = f.Get(cfg)
	cs.editCursor = len(cs.editValue)
	cs.editing = true
	cs.editErr = ""
	return nil
}

func (cs *configScreen) handleSpace() tea.Cmd {
	f := cs.fields[cs.cursor]
	switch f.Kind {
	case fieldKindBool:
		return cs.toggleBool(f)
	case fieldKindChoice:
		return cs.cycleChoice(f, true)
	}
	return nil
}

func (cs *configScreen) cycleChoice(f configField, forward bool) tea.Cmd {
	cfg := cs.getCfg()
	if cfg == nil || f.Set == nil || len(f.Choices) == 0 {
		return nil
	}
	cur := f.Get(cfg)
	idx := 0
	for i, c := range f.Choices {
		if c == cur {
			idx = i
			break
		}
	}
	if forward {
		idx = (idx + 1) % len(f.Choices)
	} else {
		idx = (idx + len(f.Choices) - 1) % len(f.Choices)
	}
	_ = f.Set(cfg, f.Choices[idx])
	cs.dirty = true
	return nil
}

func (cs *configScreen) toggleBool(f configField) tea.Cmd {
	cfg := cs.getCfg()
	if cfg == nil {
		return nil
	}
	cur := f.Get(cfg)
	newVal := "false"
	if cur != "true" {
		newVal = "true"
	}
	_ = f.Set(cfg, newVal)
	cs.dirty = true
	return nil
}

func (cs *configScreen) clearDirty() {
	cs.dirty = false
}

func (cs *configScreen) View(width, height int) string {
	cfg := cs.getCfg()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	var lines []string

	title := StyleAccent.Render("Config")
	shortcut := StyleMuted.Render("1-5 screen · ? info · F1 keys")
	lines = append(lines, title)
	lines = append(lines, shortcut)
	lines = append(lines, "")

	lastSection := ""
	for i, f := range cs.fields {
		if f.Section != lastSection {
			if lastSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, StyleMuted.Render("["+f.Section+"]"))
			lastSection = f.Section
		}

		val := f.Get(cfg)
		if f.Section == "clone" && f.Label == "target_url" && !cs.editing {
			val = connections.RedactDSN(val)
		}
		if val == "" {
			val = StyleMuted.Render("(empty)")
		}

		var row string
		if i == cs.cursor {
			if cs.editing {
				editValue := cs.editValue
				editCursor := cs.editCursor
				if f.Section == "clone" && f.Label == "target_url" {
					editValue = connections.RedactDSN(editValue)
					editCursor = len(editValue)
				}
				editDisplay := renderEditableField(editValue, editCursor, false, true)
				row = StyleAccent.Render("> "+f.Label) + fmt.Sprintf("%-*s %s", 28-2, "", editDisplay)
			} else {
				row = StyleAccent.Render("> "+f.Label) + fmt.Sprintf("%-*s %s", 28-2, "", val)
			}
		} else {
			row = fmt.Sprintf("  %-28s %s", f.Label, val)
		}
		lines = append(lines, row)

		if f.Kind == fieldKindChoice && i == cs.cursor && !cs.editing {
			lines = append(lines, "  "+StyleMuted.Render(strings.Join(f.Choices, " · ")))
		}

		if i == cs.cursor && cs.editing && cs.editErr != "" {
			lines = append(lines, "  "+StyleWarning.Render("! "+cs.editErr))
		}
	}

	// scroll to keep cursor visible
	contentLines := lines[3:] // header + blank
	visibleRows := height - 3
	if visibleRows < 1 {
		visibleRows = 1
	}

	// recalculate cursor row among content lines
	cursorRow := 0
	rowIdx := 0
	for i, f := range cs.fields {
		if i > 0 {
			prevSection := cs.fields[i-1].Section
			if f.Section != prevSection {
				rowIdx++ // blank line between sections
				rowIdx++ // section header
			}
		} else {
			rowIdx++ // section header for first field
		}
		if i == cs.cursor {
			cursorRow = rowIdx
		}
		rowIdx++
		if i == cs.cursor && !cs.editing && f.Kind == fieldKindChoice {
			rowIdx++ // choice option list
		}
		if i == cs.cursor && cs.editing && cs.editErr != "" {
			rowIdx++ // error line below field
		}
	}

	if cs.offset > cursorRow {
		cs.offset = cursorRow
	}
	if cs.offset+visibleRows-1 < cursorRow {
		cs.offset = cursorRow - visibleRows + 1
	}
	if cs.offset < 0 {
		cs.offset = 0
	}

	end := cs.offset + visibleRows
	if end > len(contentLines) {
		end = len(contentLines)
	}
	visible := contentLines
	if cs.offset < len(contentLines) {
		visible = contentLines[cs.offset:end]
	}

	header := strings.Join(lines[:3], "\n")
	body := strings.Join(visible, "\n")
	return header + "\n" + body
}
