package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const dumpLogMaxLines = 50

const (
	dumpSectionPath = iota
	dumpSectionPicker
	dumpSectionHistory
	dumpSectionLog
	dumpSectionCount
)

type dumpScreen struct {
	draft            *DumpDraft
	dumpStatus       *DumpStatus
	dumpLog          *[]string
	dumpError        *string
	dumpResult       **DumpResultSummary
	dumpProgress     **DumpProgressEvent
	restoreProgress  **RestoreProgressEvent
	restoreRunning   *bool
	hasSession       func() bool
	nav              SectionNav
	pathCursor       int
	logTailOffset    int
	fileListOffset   int
	spinnerFrame     *int
	trustedSchemaSQL bool
}

func newDumpScreen(draft *DumpDraft, hasSession func() bool, dumpStatus *DumpStatus, dumpLog *[]string, dumpError *string, dumpResult **DumpResultSummary, spinnerFrame *int, dumpProgress **DumpProgressEvent, restoreProgress **RestoreProgressEvent, restoreRunning *bool) ScreenModel {
	return &dumpScreen{
		draft:           draft,
		hasSession:      hasSession,
		dumpStatus:      dumpStatus,
		dumpLog:         dumpLog,
		dumpError:       dumpError,
		dumpResult:      dumpResult,
		dumpProgress:    dumpProgress,
		restoreProgress: restoreProgress,
		restoreRunning:  restoreRunning,
		spinnerFrame:    spinnerFrame,
		nav:             NewSectionNav(dumpSectionCount),
	}
}

func (d *dumpScreen) running() bool {
	if d.restoreRunning != nil && *d.restoreRunning {
		return true
	}
	return *d.dumpStatus == DumpStatusRunning
}

func (d *dumpScreen) complete() bool {
	return *d.dumpStatus == DumpStatusComplete
}

func (d *dumpScreen) result() *DumpResultSummary {
	if d.dumpResult == nil || *d.dumpResult == nil {
		return nil
	}
	return *d.dumpResult
}

func (d *dumpScreen) transactionLabel() string {
	if d.draft.NoTransaction {
		return "Transaction: off"
	}
	return "Transaction: on"
}

func (d *dumpScreen) resetLogScroll() {
	d.logTailOffset = 0
}

func (d *dumpScreen) resetFileListScroll() {
	d.fileListOffset = 0
}

func (d *dumpScreen) scrollLog(delta int) {
	d.logTailOffset += delta
	n := len(*d.dumpLog)
	if d.logTailOffset < 0 {
		d.logTailOffset = 0
	}
	if d.logTailOffset > n {
		d.logTailOffset = n
	}
}

func (d *dumpScreen) scrollFileList(delta int) {
	d.fileListOffset += delta
	if d.fileListOffset < 0 {
		d.fileListOffset = 0
	}
	res := d.result()
	if res == nil {
		return
	}
	maxOffset := len(res.Files)
	if d.fileListOffset > maxOffset {
		d.fileListOffset = maxOffset
	}
}

func (d *dumpScreen) applySectionEntry(entry SectionEntryMode) {
	if entry == SectionEntryInside {
		d.nav.EnterInside(dumpSectionPath)
		d.onEnterSection()
		return
	}
	d.nav.Level = SectionNavOverview
	d.nav.Section = dumpSectionPath
}

func (d *dumpScreen) onEnterSection() {
	switch d.nav.Section {
	case dumpSectionPath:
		d.pathCursor = len(d.draft.OutputDir)
	case dumpSectionHistory:
		if d.draft.History.Cursor >= len(d.draft.History.Entries) {
			d.draft.History.Cursor = 0
		}
	case dumpSectionLog:
		d.resetLogScroll()
	}
}

func (d *dumpScreen) sectionActive(section int) bool {
	return d.nav.InInside() && d.nav.Section == section
}

func (d *dumpScreen) shouldDeferEsc() bool {
	return d.nav.InInside()
}

func (d *dumpScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	k := key.Key()
	if d.running() || d.complete() {
		return nil
	}

	if d.nav.InInside() && k.Code == tea.KeyEscape {
		d.nav.Exit()
		return nil
	}

	if d.nav.InInside() && d.nav.Section == dumpSectionHistory {
		if k.Code == tea.KeySpace {
			d.trustedSchemaSQL = !d.trustedSchemaSQL
			return nil
		}
		switch k.String() {
		case "r":
			return d.requestRestore()
		}
		switch k.Code {
		case tea.KeyEnter:
			return d.requestRestore()
		}
	}

	if d.nav.InOverview() {
		switch k.String() {
		case "j":
			d.nav.MoveSection(1)
			return nil
		case "k":
			d.nav.MoveSection(-1)
			return nil
		}
		switch k.Code {
		case tea.KeyDown:
			d.nav.MoveSection(1)
			return nil
		case tea.KeyUp:
			d.nav.MoveSection(-1)
			return nil
		case tea.KeyEnter:
			d.nav.Enter()
			d.onEnterSection()
			return nil
		}
		return nil
	}

	switch k.String() {
	case "t":
		if d.sectionActive(dumpSectionPath) {
			d.draft.NoTransaction = !d.draft.NoTransaction
		}
		return nil
	}
	switch k.Code {
	case tea.KeyDown:
		switch d.nav.Section {
		case dumpSectionPicker:
			d.draft.SchemaPicker.MoveCursor(1)
		case dumpSectionHistory:
			d.draft.History.MoveCursor(1)
		case dumpSectionLog:
			d.scrollLog(-1)
		}
		return nil
	case tea.KeyUp:
		switch d.nav.Section {
		case dumpSectionPicker:
			d.draft.SchemaPicker.MoveCursor(-1)
		case dumpSectionHistory:
			d.draft.History.MoveCursor(-1)
		case dumpSectionLog:
			d.scrollLog(1)
		}
		return nil
	}
	if d.sectionActive(dumpSectionPicker) {
		if d.draft.SchemaPicker.HandleActionKey(k) {
			return nil
		}
	}
	if d.sectionActive(dumpSectionPath) {
		if handleFieldCursorKey(k, &d.draft.OutputDir, &d.pathCursor) {
			return nil
		}
	}
	return nil
}

func (d *dumpScreen) requestRestore() tea.Cmd {
	if sel := d.draft.History.Selected(); sel != nil {
		dir := sel.Path
		trusted := d.trustedSchemaSQL
		d.trustedSchemaSQL = false
		return func() tea.Msg { return restoreConfirmRequestedMsg{inputDir: dir, trustedSchemaSQL: trusted} }
	}
	return nil
}

func (d *dumpScreen) onFieldCursorNavigation() bool {
	return d.sectionActive(dumpSectionPath)
}

func (d *dumpScreen) View(width, height int) string {
	if d.complete() {
		return d.viewResult(width, height)
	}
	return d.viewIdle(width, height)
}

func (d *dumpScreen) viewIdle(width, height int) string {
	var lines []string
	lines = append(lines, StyleHeader.Render("Dump"))
	lines = append(lines, "")

	if !d.hasSession() {
		lines = append(lines, StyleMuted.Render("Connect first (screen 1)"))
	} else if d.nav.InOverview() {
		lines = append(lines, d.dumpOverviewRows()...)
	} else {
		lines = append(lines, d.dumpInsideSection(width, height)...)
	}

	if d.hasSession() {
		if d.running() {
			frame := 0
			if d.spinnerFrame != nil {
				frame = *d.spinnerFrame
			}
			isRestore := d.restoreRunning != nil && *d.restoreRunning
			if isRestore && d.restoreProgress != nil && *d.restoreProgress != nil {
				ev := *d.restoreProgress
				lines = append(lines, "")
				lines = append(lines, renderProgressBar(40, int64(ev.Current), int64(ev.Total), int64(ev.Elapsed), ""))
			} else if !isRestore && d.dumpProgress != nil && *d.dumpProgress != nil {
				ev := *d.dumpProgress
				lines = append(lines, "")
				lines = append(lines, renderProgressBar(40, int64(ev.Current), int64(ev.Total), int64(ev.Elapsed), ""))
			}
			lines = append(lines, "")
			if isRestore {
				lines = append(lines, formatWalkSpinnerLines("Restoring…", frame)...)
			} else {
				lines = append(lines, formatWalkSpinnerLines("Dump in progress…", frame)...)
			}
		} else if *d.dumpError != "" && !d.complete() {
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render(*d.dumpError))
		}
	}

	content := strings.Join(lines, "\n")
	return StyleBorder.Width(max(0, width-2)).Height(max(0, height-2)).Render(content)
}

func (d *dumpScreen) dumpOverviewRows() []string {
	pathSummary := d.draft.OutputDir
	if pathSummary == "" {
		pathSummary = "(empty)"
	}
	schemaSummary := renderSchemaPickerSummary(&d.draft.SchemaPicker)
	historySummary := "(no dumps yet)"
	if n := len(d.draft.History.Entries); n > 0 {
		historySummary = fmt.Sprintf("%d dumps", n)
	}
	logSummary := "(no messages yet)"
	if n := len(*d.dumpLog); n > 0 {
		logSummary = fmt.Sprintf("%d lines", n)
	}
	return []string{
		overviewSectionRow(d.nav, dumpSectionPath, "Base directory", pathSummary),
		overviewSectionRow(d.nav, dumpSectionPicker, "Schemas", schemaSummary),
		overviewSectionRow(d.nav, dumpSectionHistory, "History", historySummary),
		overviewSectionRow(d.nav, dumpSectionLog, "Log", logSummary),
		"",
		StyleMuted.Render("↑/↓ section · Enter open"),
	}
}

func (d *dumpScreen) dumpInsideSection(width, height int) []string {
	headerUsed := 4
	switch d.nav.Section {
	case dumpSectionPath:
		return d.pathSectionLines()
	case dumpSectionPicker:
		maxLines := schemaPickerMaxLines(height, headerUsed, 4)
		return d.schemaSection(maxLines)
	case dumpSectionHistory:
		maxLines := height - headerUsed - 3
		if maxLines < 3 {
			maxLines = 3
		}
		return d.historySection(maxLines)
	case dumpSectionLog:
		return d.logSectionLines(height, headerUsed)
	default:
		return nil
	}
}

func (d *dumpScreen) pathSectionLines() []string {
	var lines []string
	pathLabel := StyleAccent.Render("Base directory:")
	pathVal := d.draft.OutputDir
	if pathVal == "" {
		pathVal = StyleMuted.Render("(empty — set base path before run)")
	} else {
		rendered := renderEditableField(d.draft.OutputDir, d.pathCursor, false, true)
		pathVal = StyleAccent.Render(rendered)
	}
	lines = append(lines, pathLabel+" "+pathVal)
	hint := StyleMuted.Render("Each dump writes to {base}/{n}") + "  " +
		StyleMuted.Render(d.transactionLabel()) + "  (t toggle)  " +
		StyleMuted.Render("←/→ edit · Esc back")
	lines = append(lines, hint)
	return lines
}

func (d *dumpScreen) historySection(maxLines int) []string {
	var lines []string
	label := StyleAccent.Render("History:")
	hint := "(↑/↓ move · Space trust schema.sql · Enter/r restore · Esc back)"
	lines = append(lines, label+" "+StyleMuted.Render(hint))
	trusted := "[ ]"
	if d.trustedSchemaSQL {
		trusted = "[x]"
	}
	lines = append(lines, StyleMuted.Render(trusted+" Trust schema.sql for this restore"))
	lines = append(lines, renderDumpHistoryLines(&d.draft.History, maxLines-1)...)
	return lines
}

func (d *dumpScreen) logSectionLines(height, headerUsed int) []string {
	var lines []string
	logLabel := StyleAccent.Render("Log:") + " " + StyleMuted.Render("(↑/↓ scroll · Esc back)")
	lines = append(lines, logLabel)
	logLines := *d.dumpLog
	if len(logLines) == 0 {
		lines = append(lines, StyleMuted.Render("  (no messages yet)"))
		return lines
	}
	maxLog := height - headerUsed - 2
	if maxLog < 1 {
		maxLog = 1
	}
	end := len(logLines) - d.logTailOffset
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

func (d *dumpScreen) schemaSection(maxLines int) []string {
	var lines []string
	label := StyleAccent.Render("Schemas:")
	hint := "(↑/↓ move · Space toggle · a all · Esc back)"
	lines = append(lines, label+" "+StyleMuted.Render(hint))
	lines = append(lines, renderSchemaPickerSummary(&d.draft.SchemaPicker))
	if d.sectionActive(dumpSectionPicker) {
		lines = append(lines, renderSelectAllLine(&d.draft.SchemaPicker))
	}
	lines = append(lines, renderSchemaPickerLines(&d.draft.SchemaPicker, maxLines)...)
	return lines
}

func (d *dumpScreen) viewResult(width, height int) string {
	res := d.result()
	var lines []string
	lines = append(lines, StyleHeader.Render("Dump"))
	lines = append(lines, "")

	if res == nil {
		lines = append(lines, StyleMuted.Render("(no result data)"))
		content := strings.Join(lines, "\n")
		return StyleBorder.Width(max(0, width-2)).Height(max(0, height-2)).Render(content)
	}

	if res.Outcome == DumpOutcomeSuccess {
		lines = append(lines, StyleAccent.Render("✓ Dump complete"))
	} else {
		lines = append(lines, StyleWarning.Render("✗ Dump failed"))
	}

	path := truncateRunes(res.OutputDir, max(0, width-12))
	lines = append(lines, StyleMuted.Render("Output:")+" "+StyleBase.Render(path))

	if res.TableCount > 0 || res.TotalRowEstimate != nil {
		stats := fmt.Sprintf("Tables: %d", res.TableCount)
		if res.TotalRowEstimate != nil {
			stats += " · Rows (est): " + formatIntComma(*res.TotalRowEstimate)
		}
		lines = append(lines, StyleBase.Render(stats))
	}

	if res.Error != "" {
		errLine := truncateRunes(res.Error, max(0, width-10))
		lines = append(lines, StyleWarning.Render("Error: "+errLine))
	}

	if res.HasIncomplete {
		lines = append(lines, StyleMuted.Render("(incomplete .tmp artifacts present)"))
	}

	fixedLines := len(lines) + 1
	maxFileLines := height - fixedLines - 2
	if maxFileLines < 1 {
		maxFileLines = 1
	}

	lines = append(lines, StyleMuted.Render("Files:"))
	totalFiles := len(res.Files)
	if totalFiles == 0 {
		lines = append(lines, StyleMuted.Render("  (none)"))
	} else {
		end := totalFiles - d.fileListOffset
		if end < 0 {
			end = 0
		}
		start := end - maxFileLines
		if start < 0 {
			start = 0
		}
		hiddenAbove := start
		hiddenBelow := end - start - maxFileLines
		if hiddenBelow < 0 {
			hiddenBelow = 0
		}
		if hiddenAbove > 0 && len(lines) < height-2 {
			lines = append(lines, StyleMuted.Render(fmt.Sprintf("  … +%d above", hiddenAbove)))
		}
		for _, name := range res.Files[start:end] {
			if len(lines) >= height-2 {
				break
			}
			lines = append(lines, StyleBase.Render("  "+name))
		}
		remaining := totalFiles - end
		if remaining > 0 {
			lines = append(lines, StyleMuted.Render(fmt.Sprintf("  … +%d more", remaining)))
		} else if hiddenBelow > 0 {
			lines = append(lines, StyleMuted.Render(fmt.Sprintf("  … +%d more", hiddenBelow)))
		}
	}

	content := strings.Join(lines, "\n")
	return StyleBorder.Width(max(0, width-2)).Height(max(0, height-2)).Render(content)
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return "…"
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func formatIntComma(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	if s != "" {
		parts = append([]string{s}, parts...)
	}
	return strings.Join(parts, ",")
}

func appendDumpLog(log *[]string, line string) {
	*log = append(*log, line)
	if len(*log) > dumpLogMaxLines {
		*log = (*log)[len(*log)-dumpLogMaxLines:]
	}
}
