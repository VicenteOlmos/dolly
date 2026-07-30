package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

const cloneFormFieldCount = 4

const (
	cloneSectionForm = iota
	cloneSectionPicker
	cloneSectionLog
	cloneSectionCount
)

type cloneScreen struct {
	draft           *CloneDraft
	cloneStatus     *CloneStatus
	cloneLog        *[]string
	cloneError      *string
	cloneProgress   **CloneProgressEvent
	hasSession      func() bool
	nav             SectionNav
	formField       int
	fieldCursors    [cloneFormFieldCount]int
	logTailOffset   int
	spinnerFrame    *int
	store           connections.ConnectionStore
	saveConnections bool
	getCfg          func() *config.Config
	getConnDSN      func() string
}

func newCloneScreen(
	draft *CloneDraft,
	hasSession func() bool,
	cloneStatus *CloneStatus,
	cloneLog *[]string,
	cloneError *string,
	spinnerFrame *int,
	store connections.ConnectionStore,
	saveConnections bool,
	getCfg func() *config.Config,
	getConnDSN func() string,
	cloneProgress **CloneProgressEvent,
) ScreenModel {
	return &cloneScreen{
		draft:           draft,
		hasSession:      hasSession,
		cloneStatus:     cloneStatus,
		cloneLog:        cloneLog,
		cloneError:      cloneError,
		cloneProgress:   cloneProgress,
		spinnerFrame:    spinnerFrame,
		store:           store,
		saveConnections: saveConnections,
		getCfg:          getCfg,
		getConnDSN:      getConnDSN,
		nav:             NewSectionNav(cloneSectionCount),
	}
}

func (c *cloneScreen) running() bool {
	return *c.cloneStatus == CloneStatusRunning
}

func (c *cloneScreen) complete() bool {
	return *c.cloneStatus == CloneStatusComplete
}

func (c *cloneScreen) resetLogScroll() {
	c.logTailOffset = 0
}

func (c *cloneScreen) scrollLog(delta int) {
	c.logTailOffset += delta
	n := len(*c.cloneLog)
	if c.logTailOffset < 0 {
		c.logTailOffset = 0
	}
	if c.logTailOffset > n {
		c.logTailOffset = n
	}
}

func (c *cloneScreen) activeField() *string {
	switch c.formField {
	case 0:
		return &c.draft.CloneName
	case 1:
		if c.draft.TargetSource == TargetSourceManual {
			return &c.draft.TargetDSN
		}
		return nil
	case 2:
		// Strategy is a cycler; don't return editable field.
		return nil
	default:
		return nil
	}
}

// cycleStrategy cycles through cloneStrategyChoices with ←/→.
func (c *cloneScreen) cycleStrategy(delta int) {
	current := -1
	for i, s := range cloneStrategyChoices {
		if s == c.draft.Strategy {
			current = i
			break
		}
	}
	next := current + delta
	if next < 0 {
		next = len(cloneStrategyChoices) - 1
	}
	if next >= len(cloneStrategyChoices) {
		next = 0
	}
	c.draft.Strategy = cloneStrategyChoices[next]
	c.resolveTargetDSN()
}

// cycleTargetSource cycles through target source: Current → Saved → Manual.
func (c *cloneScreen) cycleTargetSource(delta int) {
	next := int(c.draft.TargetSource) + delta
	if next < 0 {
		next = int(TargetSourceManual)
	}
	if next > int(TargetSourceManual) {
		next = int(TargetSourceCurrent)
	}
	c.draft.TargetSource = TargetSource(next)
	// Resolve target DSN from the new source.
	c.resolveTargetDSN()
}

// resolveTargetDSN sets TargetDSN based on the current TargetSource.
func (c *cloneScreen) resolveTargetDSN() {
	resolveCloneDraftTargetDSN(c.draft, c.getConnDSN, c.store)
}

// cycleSavedProfile advances TargetProfileName through saved profiles.
func (c *cloneScreen) cycleSavedProfile(delta int) {
	if c.store == nil {
		return
	}
	profiles, err := c.store.List()
	if err != nil || len(profiles) == 0 {
		return
	}
	idx := 0
	for i, p := range profiles {
		if p.Name == c.draft.TargetProfileName {
			idx = i
			break
		}
	}
	next := idx + delta
	if next < 0 {
		next = len(profiles) - 1
	}
	if next >= len(profiles) {
		next = 0
	}
	c.draft.TargetProfileName = profiles[next].Name
	c.draft.TargetDSN = profileDSN(profiles[next])
}

// profileDSN builds a DSN string from a saved connection profile.
func profileDSN(p connections.Connection) string {
	return p.DSN()
}

func (c *cloneScreen) moveFormField(delta int) {
	c.formField += delta
	if c.formField < 0 {
		c.formField = cloneFormFieldCount - 1
	}
	if c.formField >= cloneFormFieldCount {
		c.formField = 0
	}
	field := c.activeField()
	if field != nil {
		c.fieldCursors[c.formField] = len(*field)
	}
}

func (c *cloneScreen) applySectionEntry(entry SectionEntryMode) {
	if entry == SectionEntryInside {
		c.nav.EnterInside(cloneSectionForm)
		c.onEnterSection()
		return
	}
	c.nav.Level = SectionNavOverview
	c.nav.Section = cloneSectionForm
}

func (c *cloneScreen) onEnterSection() {
	switch c.nav.Section {
	case cloneSectionForm:
		field := c.activeField()
		if field != nil {
			c.fieldCursors[c.formField] = len(*field)
		}
	case cloneSectionLog:
		c.resetLogScroll()
	}
}

func (c *cloneScreen) sectionActive(section int) bool {
	return c.nav.InInside() && c.nav.Section == section
}

func (c *cloneScreen) shouldDeferEsc() bool {
	return c.nav.InInside()
}

func (c *cloneScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	k := key.Key()
	if c.running() || c.complete() {
		return nil
	}

	// Analyze cancel is handled at the App level (c/Esc).
	if c.draft.AnalyzeState.Loading {
		return nil
	}

	if c.nav.InInside() && k.Code == tea.KeyEscape {
		c.nav.Exit()
		return nil
	}

	if c.nav.InOverview() {
		switch k.String() {
		case "j":
			c.nav.MoveSection(1)
			return nil
		case "k":
			c.nav.MoveSection(-1)
			return nil
		}
		switch k.Code {
		case tea.KeyDown:
			c.nav.MoveSection(1)
			return nil
		case tea.KeyUp:
			c.nav.MoveSection(-1)
			return nil
		case tea.KeyEnter:
			c.nav.Enter()
			c.onEnterSection()
			return nil
		}
		return nil
	}

	// Global clone-specific keys (when inside a section).
	if c.sectionActive(cloneSectionForm) {
		switch k.String() {
		case "a":
			if c.formField == 3 {
				c.toggleAnalyze()
				return nil
			}
		case "t":
			c.cycleTargetSource(1)
			return nil
		case "j":
			if c.formField == 1 && c.draft.TargetSource == TargetSourceSaved {
				c.cycleSavedProfile(1)
				return nil
			}
		case "k":
			if c.formField == 1 && c.draft.TargetSource == TargetSourceSaved {
				c.cycleSavedProfile(-1)
				return nil
			}
		}
	}

	switch k.Code {
	case tea.KeyTab:
		if c.sectionActive(cloneSectionForm) {
			if k.Mod&tea.ModShift != 0 {
				c.moveFormField(-1)
			} else {
				c.moveFormField(1)
			}
		}
		return nil
	case tea.KeyDown:
		switch c.nav.Section {
		case cloneSectionForm:
			c.moveFormField(1)
		case cloneSectionPicker:
			c.draft.SchemaPicker.MoveCursor(1)
		case cloneSectionLog:
			c.scrollLog(-1)
		}
		return nil
	case tea.KeyUp:
		switch c.nav.Section {
		case cloneSectionForm:
			c.moveFormField(-1)
		case cloneSectionPicker:
			c.draft.SchemaPicker.MoveCursor(-1)
		case cloneSectionLog:
			c.scrollLog(1)
		}
		return nil
	case tea.KeyLeft:
		if c.sectionActive(cloneSectionForm) && c.formField == 2 {
			c.cycleStrategy(-1)
			return nil
		}
	case tea.KeyRight:
		if c.sectionActive(cloneSectionForm) && c.formField == 2 {
			c.cycleStrategy(1)
			return nil
		}
	case tea.KeyEnter, tea.KeySpace:
		if c.sectionActive(cloneSectionForm) && c.formField == 3 {
			c.toggleAnalyze()
			return nil
		}
	}
	if c.sectionActive(cloneSectionPicker) {
		if c.draft.SchemaPicker.HandleActionKey(k) {
			return nil
		}
	}
	if c.sectionActive(cloneSectionForm) {
		// Suppress printable input at strategy index (it's a cycler).
		if c.formField == 2 && len(k.Text) == 1 && k.Text[0] >= 32 {
			return nil
		}
		field := c.activeField()
		cur := &c.fieldCursors[c.formField]
		if field != nil && handleFieldCursorKey(k, field, cur) {
			return nil
		}
	}
	return nil
}

func (c *cloneScreen) onFieldCursorNavigation() bool {
	return c.sectionActive(cloneSectionForm)
}

func (c *cloneScreen) View(width, height int) string {
	if c.complete() {
		return c.viewComplete(width, height)
	}
	return c.viewIdle(width, height)
}

func (c *cloneScreen) viewIdle(width, height int) string {
	var lines []string
	lines = append(lines, StyleHeader.Render("Clone"))
	lines = append(lines, "")

	if !c.hasSession() {
		lines = append(lines, StyleMuted.Render("Connect first (screen 1)"))
	} else if c.nav.InOverview() {
		lines = append(lines, c.cloneOverviewRows()...)
	} else {
		lines = append(lines, c.cloneInsideSection(width, height)...)
	}

	if c.hasSession() {
		if c.running() {
			frame := 0
			if c.spinnerFrame != nil {
				frame = *c.spinnerFrame
			}
			if c.cloneProgress != nil && *c.cloneProgress != nil {
				ev := *c.cloneProgress
				lines = append(lines, "")
				lines = append(lines, renderProgressBar(40, int64(ev.Current), int64(ev.Total), int64(ev.Elapsed), ""))
			}
			lines = append(lines, "")
			lines = append(lines, formatCloneSpinnerLines("Cloning…", frame, width)...)
		} else if *c.cloneError != "" && !c.complete() {
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render(*c.cloneError))
		}
	}

	content := strings.Join(lines, "\n")
	return StyleBorder.Width(max(0, width-2)).Height(max(0, height-2)).Render(content)
}

func (c *cloneScreen) cloneOverviewRows() []string {
	formSummary := c.draft.CloneName
	if formSummary == "" {
		formSummary = "(incomplete)"
	}
	schemaSummary := renderSchemaPickerSummary(&c.draft.SchemaPicker)
	logSummary := "(no messages yet)"
	if n := len(*c.cloneLog); n > 0 {
		logSummary = fmt.Sprintf("%d lines", n)
	}
	rows := []string{
		overviewSectionRow(c.nav, cloneSectionForm, "Target & strategy", formSummary),
		overviewSectionRow(c.nav, cloneSectionPicker, "Schemas", schemaSummary),
		overviewSectionRow(c.nav, cloneSectionLog, "Log", logSummary),
		"",
		StyleMuted.Render("↑/↓ section · Enter open"),
	}
	return rows
}

func (c *cloneScreen) cloneInsideSection(width, height int) []string {
	headerUsed := 4
	switch c.nav.Section {
	case cloneSectionForm:
		hint := "  " + StyleMuted.Render("↑/↓/Tab field · ←/→ edit · Space toggle analyze · Esc back")
		return c.formSection(hint, width)
	case cloneSectionPicker:
		maxLines := schemaPickerMaxLines(height, headerUsed, 4)
		return c.schemaSection(maxLines)
	case cloneSectionLog:
		return c.logSectionLines(height, headerUsed)
	default:
		return nil
	}
}

func (c *cloneScreen) logSectionLines(height, headerUsed int) []string {
	var lines []string
	logLabel := StyleAccent.Render("Log:") + " " + StyleMuted.Render("(↑/↓ scroll · Esc back)")
	lines = append(lines, logLabel)
	logLines := *c.cloneLog
	if len(logLines) == 0 {
		lines = append(lines, StyleMuted.Render("  (no messages yet)"))
		return lines
	}
	maxLog := height - headerUsed - 2
	if maxLog < 1 {
		maxLog = 1
	}
	end := len(logLines) - c.logTailOffset
	if end < 0 {
		end = 0
	}
	start := end - maxLog
	if start < 0 {
		start = 0
	}
	for _, line := range logLines[start:end] {
		lines = append(lines, StyleBase.Render("  "+line))
	}
	return lines
}

func (c *cloneScreen) formSection(hint string, width int) []string {
	label := StyleAccent.Render("Target & strategy:")
	lines := []string{label + hint}
	lines = append(lines, c.fieldLine("Clone name:", c.draft.CloneName, 0, width))
	lines = append(lines, c.renderTargetField(width))
	lines = append(lines, c.renderStrategyField(width))
	lines = append(lines, c.renderAnalyzeLine())
	return lines
}

// renderTargetField renders the Target DSN field with a source badge.
func (c *cloneScreen) renderTargetField(width int) string {
	focused := c.sectionActive(cloneSectionForm) && c.formField == 1
	badge := StyleMuted.Render(fmt.Sprintf("[%s]", c.draft.TargetSource))
	displayDSN := c.draft.TargetDSN
	displayDSN = connections.RedactMessage(displayDSN)
	var value string
	switch {
	case focused:
		value = StyleAccent.Render(displayDSN)
	case displayDSN == "":
		value = StyleMuted.Render("(empty)")
	default:
		value = StyleBase.Render(displayDSN)
	}
	return StyleMuted.Render("Target DSN:") + " " + badge + " " + value
}

// renderStrategyField renders the Strategy as a cycler.
func (c *cloneScreen) renderStrategyField(width int) string {
	focused := c.sectionActive(cloneSectionForm) && c.formField == 2
	strategy := c.draft.Strategy
	if strategy == "" {
		strategy = "schema-replay"
	}
	var value string
	if focused {
		value = StyleAccent.Render(fmt.Sprintf("← %s →", strategy))
	} else {
		value = StyleBase.Render(strategy)
	}
	return StyleMuted.Render("Strategy:") + " " + value
}

func (c *cloneScreen) toggleAnalyze() {
	c.draft.AnalyzeEnabled = !c.draft.AnalyzeEnabled
}

// renderAnalyzeLine renders the analyze toggle and result.
func (c *cloneScreen) renderAnalyzeLine() string {
	focused := c.sectionActive(cloneSectionForm) && c.formField == 3
	toggle := "[ ]"
	if c.draft.AnalyzeEnabled {
		toggle = "[✓]"
	}
	if focused {
		toggle = StyleAccent.Render(toggle)
	} else {
		toggle = StyleBase.Render(toggle)
	}
	line := StyleMuted.Render("Analyze:") + " " + toggle

	if c.draft.AnalyzeState.Loading {
		frame := 0
		if c.spinnerFrame != nil {
			frame = *c.spinnerFrame
		}
		line += " " + formatSpinnerCompact("analyzing…", frame)
	} else if c.draft.AnalyzeState.Err != "" {
		line += " " + StyleWarning.Render(c.draft.AnalyzeState.Err)
	}
	return line
}

// formatBytes formats a byte count into a human-readable string.
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (c *cloneScreen) schemaSection(maxLines int) []string {
	var lines []string
	label := StyleAccent.Render("Schemas:")
	hint := "(↑/↓ move · Space toggle · a all · Esc back)"
	lines = append(lines, label+" "+StyleMuted.Render(hint))
	lines = append(lines, renderSchemaPickerSummary(&c.draft.SchemaPicker))
	if c.sectionActive(cloneSectionPicker) {
		lines = append(lines, renderSelectAllLine(&c.draft.SchemaPicker))
	}
	lines = append(lines, renderSchemaPickerLines(&c.draft.SchemaPicker, maxLines)...)
	return lines
}

func (c *cloneScreen) fieldLine(label, value string, index, width int) string {
	focused := c.sectionActive(cloneSectionForm) && c.formField == index
	rendered := renderEditableField(value, c.fieldCursors[index], false, focused)
	if value == "" && !focused {
		value = StyleMuted.Render("(empty)")
	} else if focused {
		value = StyleAccent.Render(rendered)
	} else {
		value = StyleBase.Render(rendered)
	}
	return StyleMuted.Render(label) + " " + value
}

func (c *cloneScreen) viewComplete(width, height int) string {
	var lines []string
	lines = append(lines, StyleHeader.Render("Clone"))
	lines = append(lines, "")
	if *c.cloneError != "" {
		lines = append(lines, StyleWarning.Render("✗ Clone failed"))
		errLine := truncateRunes(*c.cloneError, max(0, width-10))
		lines = append(lines, StyleWarning.Render("Error: "+errLine))
	} else {
		lines = append(lines, StyleAccent.Render("✓ Clone complete"))
	}
	lines = append(lines, "")
	lines = append(lines, StyleMuted.Render("Enter run again · Esc dismiss"))
	content := strings.Join(lines, "\n")
	return StyleBorder.Width(max(0, width-2)).Height(max(0, height-2)).Render(content)
}
