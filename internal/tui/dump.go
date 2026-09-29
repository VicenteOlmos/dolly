package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const dumpLogMaxLines = 50

const (
	historyFocusList = iota
	historyFocusPath
	historyFocusConflict
	historyFocusReplace
	historyFocusTrust
	historyFocusCount
)

const (
	dumpSectionPath = iota
	dumpSectionMode
	dumpSectionPicker
	dumpSectionHistory
	dumpSectionLog
	dumpSectionCount
)

const (
	modeFieldSlow = iota
	modeFieldSafe
	modeFieldWorkers
	modeFieldPercent
	modeFieldSeed
	modeFieldChunk
	modeFieldMaxDepth
	modeFieldMaxTables
	modeFieldMaxRows
	modeFieldMaxRowsPerTable
	modeFieldInclude
	modeFieldExclude
	modeFieldChunkSize
	modeFieldRetryMax
	modeFieldRetryBase
	modeFieldIncludeTableFile
	modeFieldCount
)

type dumpScreen struct {
	draft                  *DumpDraft
	dumpStatus             *DumpStatus
	dumpLog                *[]string
	dumpError              *string
	dumpResult             **DumpResultSummary
	dumpProgress           **DumpProgressEvent
	restoreProgress        **RestoreProgressEvent
	restoreRunning         *bool
	hasSession             func() bool
	nav                    SectionNav
	pathCursor             int
	modeField              int
	percentCursor          int
	seedCursor             int
	chunkCursor            int
	depthCursor            int
	tablesCursor           int
	rowsCursor             int
	rowsPerCursor          int
	includeCursor          int
	excludeCursor          int
	chunkSizeCursor        int
	retryMaxCursor         int
	retryBaseCursor        int
	includeTableFileCursor int
	restoreDir             string
	restoreDirCursor       int
	restoreDirFocus        bool
	historyFocus           int
	logTailOffset          int
	fileListOffset         int
	spinnerFrame           *int
	trustedSchemaSQL       bool
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
	case dumpSectionMode:
		d.syncModeCursors()
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
		if d.nav.Section == dumpSectionHistory && d.restoreDirFocus {
			d.restoreDirFocus = false
			return nil
		}
		d.nav.Exit()
		return nil
	}

	if d.nav.InInside() && d.nav.Section == dumpSectionHistory {
		if k.Code == tea.KeyTab {
			if d.restoreDirFocus {
				d.restoreDirFocus = false
			}
			d.historyFocus = (d.historyFocus + 1) % historyFocusCount
			if d.historyFocus == historyFocusPath {
				d.restoreDirFocus = true
				d.restoreDirCursor = len(d.restoreDir)
			}
			return nil
		}
		if !d.restoreDirFocus && k.String() == "p" {
			d.historyFocus = historyFocusPath
			d.restoreDirFocus = true
			return nil
		}
		if d.restoreDirFocus && handleFieldCursorKey(k, &d.restoreDir, &d.restoreDirCursor) {
			return nil
		}
		if d.restoreDirFocus && k.Code == tea.KeyEscape {
			d.restoreDirFocus = false
			return nil
		}
		if k.Code == tea.KeySpace && !d.restoreDirFocus {
			switch d.historyFocus {
			case historyFocusConflict:
				d.draft.RestoreOnConflict = cycleRestoreOnConflict(d.draft.RestoreOnConflict)
				return nil
			case historyFocusReplace:
				d.toggleRestoreReplace()
				return nil
			case historyFocusTrust:
				d.trustedSchemaSQL = !d.trustedSchemaSQL
				return nil
			case historyFocusList:
				d.trustedSchemaSQL = !d.trustedSchemaSQL
				return nil
			}
		}
		if k.Code == tea.KeyEnter && !d.restoreDirFocus {
			if d.historyFocus == historyFocusConflict {
				d.draft.RestoreOnConflict = cycleRestoreOnConflict(d.draft.RestoreOnConflict)
				return nil
			}
		}
		switch k.String() {
		case "r":
			if !d.restoreDirFocus {
				return d.requestRestore()
			}
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

	if d.sectionActive(dumpSectionMode) {
		if d.handleModeKey(k) {
			return nil
		}
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
		case dumpSectionMode:
			d.moveModeField(1)
		case dumpSectionPicker:
			d.draft.SchemaPicker.MoveCursor(1)
		case dumpSectionHistory:
			if d.historyFocus == historyFocusList && !d.restoreDirFocus {
				d.draft.History.MoveCursor(1)
			} else {
				d.moveHistoryFocus(1)
			}
		case dumpSectionLog:
			d.scrollLog(-1)
		}
		return nil
	case tea.KeyUp:
		switch d.nav.Section {
		case dumpSectionMode:
			d.moveModeField(-1)
		case dumpSectionPicker:
			d.draft.SchemaPicker.MoveCursor(-1)
		case dumpSectionHistory:
			if d.historyFocus == historyFocusList && !d.restoreDirFocus {
				d.draft.History.MoveCursor(-1)
			} else {
				d.moveHistoryFocus(-1)
			}
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
	dir := strings.TrimSpace(d.restoreDir)
	if dir == "" {
		if sel := d.draft.History.Selected(); sel != nil {
			dir = sel.Path
		}
	}
	if dir == "" {
		return nil
	}
	trusted := d.trustedSchemaSQL
	d.trustedSchemaSQL = false
	return func() tea.Msg { return restoreConfirmRequestedMsg{inputDir: dir, trustedSchemaSQL: trusted} }
}

func (d *dumpScreen) onFieldCursorNavigation() bool {
	return d.sectionActive(dumpSectionPath) || d.modeTextFocused()
}

func (d *dumpScreen) modeTextFocused() bool {
	if !d.sectionActive(dumpSectionMode) {
		return false
	}
	switch d.modeField {
	case modeFieldPercent, modeFieldSeed, modeFieldChunk,
		modeFieldMaxDepth, modeFieldMaxTables, modeFieldMaxRows, modeFieldMaxRowsPerTable,
		modeFieldInclude, modeFieldExclude, modeFieldChunkSize, modeFieldRetryMax, modeFieldRetryBase,
		modeFieldIncludeTableFile:
		return true
	default:
		return false
	}
}

func (d *dumpScreen) syncModeCursors() {
	d.percentCursor = len(d.draft.PercentText)
	d.seedCursor = len(d.draft.SeedFile)
	d.chunkCursor = len(d.draft.ChunkTables)
	d.depthCursor = len(d.draft.MaxDepthText)
	d.tablesCursor = len(d.draft.MaxTablesText)
	d.rowsCursor = len(d.draft.MaxRowsText)
	d.rowsPerCursor = len(d.draft.MaxRowsPerTableText)
	d.includeCursor = len(d.draft.IncludeTables)
	d.excludeCursor = len(d.draft.ExcludeTables)
	d.chunkSizeCursor = len(d.draft.ChunkSizeText)
	d.retryMaxCursor = len(d.draft.RetryMaxText)
	d.retryBaseCursor = len(d.draft.RetryBaseText)
	d.includeTableFileCursor = len(d.draft.IncludeTableFile)
}

func (d *dumpScreen) moveModeField(delta int) {
	d.modeField += delta
	if d.modeField < modeFieldSlow {
		d.modeField = modeFieldSlow
	}
	if d.modeField >= modeFieldCount {
		d.modeField = modeFieldCount - 1
	}
	d.syncModeCursors()
}

func (d *dumpScreen) handleModeKey(k tea.Key) bool {
	if d.modeTextFocused() {
		switch d.modeField {
		case modeFieldPercent:
			return handleFieldCursorKey(k, &d.draft.PercentText, &d.percentCursor)
		case modeFieldSeed:
			return handleFieldCursorKey(k, &d.draft.SeedFile, &d.seedCursor)
		case modeFieldChunk:
			return handleFieldCursorKey(k, &d.draft.ChunkTables, &d.chunkCursor)
		case modeFieldMaxDepth:
			return handleFieldCursorKey(k, &d.draft.MaxDepthText, &d.depthCursor)
		case modeFieldMaxTables:
			return handleFieldCursorKey(k, &d.draft.MaxTablesText, &d.tablesCursor)
		case modeFieldMaxRows:
			return handleFieldCursorKey(k, &d.draft.MaxRowsText, &d.rowsCursor)
		case modeFieldMaxRowsPerTable:
			return handleFieldCursorKey(k, &d.draft.MaxRowsPerTableText, &d.rowsPerCursor)
		case modeFieldInclude:
			return handleFieldCursorKey(k, &d.draft.IncludeTables, &d.includeCursor)
		case modeFieldExclude:
			return handleFieldCursorKey(k, &d.draft.ExcludeTables, &d.excludeCursor)
		case modeFieldChunkSize:
			return handleFieldCursorKey(k, &d.draft.ChunkSizeText, &d.chunkSizeCursor)
		case modeFieldRetryMax:
			return handleFieldCursorKey(k, &d.draft.RetryMaxText, &d.retryMaxCursor)
		case modeFieldRetryBase:
			return handleFieldCursorKey(k, &d.draft.RetryBaseText, &d.retryBaseCursor)
		case modeFieldIncludeTableFile:
			return handleFieldCursorKey(k, &d.draft.IncludeTableFile, &d.includeTableFileCursor)
		}
	}
	switch d.modeField {
	case modeFieldSlow:
		if k.String() == "s" || k.Code == tea.KeySpace {
			d.draft.SlowConnection = !d.draft.SlowConnection
			return true
		}
	case modeFieldSafe:
		if k.String() == "k" || k.Code == tea.KeySpace {
			d.draft.RequireSafeKey = !d.draft.RequireSafeKey
			return true
		}
	case modeFieldWorkers:
		switch k.Code {
		case tea.KeyLeft:
			d.adjustWorkers(-1)
			return true
		case tea.KeyRight:
			d.adjustWorkers(1)
			return true
		}
	}
	return false
}

func (d *dumpScreen) adjustWorkers(delta int) {
	maxWorkers := maxDumpWorkers()
	if !d.draft.WorkersSet {
		if delta > 0 {
			d.draft.Workers = 1
			d.draft.WorkersSet = true
		}
		return
	}
	next := d.draft.Workers + delta
	if next < 1 {
		d.draft.Workers = 0
		d.draft.WorkersSet = false
		return
	}
	if next > maxWorkers {
		next = maxWorkers
	}
	d.draft.Workers = next
}

func (d *dumpScreen) modeSummary() string {
	parts := []string{"full"}
	if d.draft.SlowConnection {
		parts = []string{"slow"}
	}
	if d.draft.RequireSafeKey {
		parts = append(parts, "safe-key")
	}
	if strings.TrimSpace(d.draft.PercentText) != "" {
		parts = append(parts, strings.TrimSpace(d.draft.PercentText)+"%")
	}
	if strings.TrimSpace(d.draft.SeedFile) != "" {
		parts = append(parts, "seed")
	}
	if strings.TrimSpace(d.draft.ChunkTables) != "" {
		parts = append(parts, "chunk")
	}
	if strings.TrimSpace(d.draft.IncludeTables) != "" {
		parts = append(parts, "include")
	}
	if strings.TrimSpace(d.draft.ExcludeTables) != "" {
		parts = append(parts, "exclude")
	}
	if strings.TrimSpace(d.draft.IncludeTableFile) != "" {
		parts = append(parts, "include-file")
	}
	if d.draft.WorkersSet {
		parts = append(parts, fmt.Sprintf("workers %d", d.draft.Workers))
	}
	return strings.Join(parts, " · ")
}

func (d *dumpScreen) workersLabel() string {
	if !d.draft.WorkersSet {
		return "config"
	}
	return strconv.Itoa(d.draft.Workers)
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
		overviewSectionRow(d.nav, dumpSectionMode, "Mode", d.modeSummary()),
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
	case dumpSectionMode:
		return d.modeSectionLines()
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

func (d *dumpScreen) modeSectionLines() []string {
	onOff := func(v bool) string {
		if v {
			return "on"
		}
		return "off"
	}
	rows := []struct {
		field int
		label string
		value string
	}{
		{modeFieldSlow, "Slow connection", onOff(d.draft.SlowConnection)},
		{modeFieldSafe, "Require safe key", onOff(d.draft.RequireSafeKey)},
		{modeFieldWorkers, "Workers", d.workersLabel()},
		{modeFieldPercent, "Percent", d.modeFieldValue(d.draft.PercentText, d.percentCursor, modeFieldPercent)},
		{modeFieldSeed, "Seed file", d.modeFieldValue(d.draft.SeedFile, d.seedCursor, modeFieldSeed)},
		{modeFieldChunk, "Chunk tables", d.modeFieldValue(d.draft.ChunkTables, d.chunkCursor, modeFieldChunk)},
		{modeFieldMaxDepth, "Max depth", d.modeFieldValue(d.draft.MaxDepthText, d.depthCursor, modeFieldMaxDepth)},
		{modeFieldMaxTables, "Max tables", d.modeFieldValue(d.draft.MaxTablesText, d.tablesCursor, modeFieldMaxTables)},
		{modeFieldMaxRows, "Max rows", d.modeFieldValue(d.draft.MaxRowsText, d.rowsCursor, modeFieldMaxRows)},
		{modeFieldMaxRowsPerTable, "Max rows/table", d.modeFieldValue(d.draft.MaxRowsPerTableText, d.rowsPerCursor, modeFieldMaxRowsPerTable)},
		{modeFieldInclude, "Include tables", d.modeFieldValue(d.draft.IncludeTables, d.includeCursor, modeFieldInclude)},
		{modeFieldExclude, "Exclude tables", d.modeFieldValue(d.draft.ExcludeTables, d.excludeCursor, modeFieldExclude)},
		{modeFieldChunkSize, "Chunk size", d.modeFieldValue(d.draft.ChunkSizeText, d.chunkSizeCursor, modeFieldChunkSize)},
		{modeFieldRetryMax, "Retry max", d.modeFieldValue(d.draft.RetryMaxText, d.retryMaxCursor, modeFieldRetryMax)},
		{modeFieldRetryBase, "Retry base", d.modeFieldValue(d.draft.RetryBaseText, d.retryBaseCursor, modeFieldRetryBase)},
		{modeFieldIncludeTableFile, "Include table file", d.modeFieldValue(d.draft.IncludeTableFile, d.includeTableFileCursor, modeFieldIncludeTableFile)},
	}
	var lines []string
	lines = append(lines, StyleAccent.Render("Mode"))
	for _, row := range rows {
		prefix := "  "
		label := row.label
		if d.modeField == row.field {
			prefix = "> "
			label = StyleAccent.Render(label)
		}
		val := row.value
		if val == "" {
			val = StyleMuted.Render("(empty)")
		}
		lines = append(lines, prefix+label+"  "+val)
	}
	lines = append(lines, StyleMuted.Render("↑/↓ field · s slow · k safe key · ←/→ workers · type limits and tables · Esc back"))
	return lines
}

func (d *dumpScreen) modeFieldValue(value string, cursor, field int) string {
	if d.modeField != field {
		return value
	}
	return renderEditableField(value, cursor, false, true)
}

func cycleRestoreOnConflict(current string) string {
	switch current {
	case "", "error":
		return "skip"
	case "skip":
		return "upsert"
	default:
		return "error"
	}
}

func (d *dumpScreen) toggleRestoreReplace() {
	if !d.draft.RestoreReplaceSet {
		d.draft.RestoreReplace = !d.draft.RestoreReplace
		d.draft.RestoreReplaceSet = true
		return
	}
	d.draft.RestoreReplace = !d.draft.RestoreReplace
}

func (d *dumpScreen) moveHistoryFocus(delta int) {
	if d.restoreDirFocus {
		return
	}
	d.historyFocus += delta
	if d.historyFocus < historyFocusList {
		d.historyFocus = historyFocusList
	}
	if d.historyFocus >= historyFocusCount {
		d.historyFocus = historyFocusCount - 1
	}
}

func (d *dumpScreen) restoreConflictLabel() string {
	if d.draft.RestoreOnConflict == "" {
		return "config"
	}
	return d.draft.RestoreOnConflict
}

func (d *dumpScreen) restoreReplaceLabel() string {
	if !d.draft.RestoreReplaceSet {
		return "config"
	}
	if d.draft.RestoreReplace {
		return "on"
	}
	return "off"
}

func (d *dumpScreen) historySection(maxLines int) []string {
	var lines []string
	label := StyleAccent.Render("History:")
	hint := "(Tab field · p path · ↑/↓ · Space edit · Enter restore · Esc back)"
	lines = append(lines, label+" "+StyleMuted.Render(hint))
	pathLabel := "Restore directory:"
	pathVal := renderEditableField(d.restoreDir, d.restoreDirCursor, false, d.restoreDirFocus)
	if pathVal == "" {
		pathVal = StyleMuted.Render("(empty — use the selected history dump)")
	}
	lines = append(lines, d.historyControlLine(historyFocusPath, pathLabel, pathVal))
	conflictVal := d.restoreConflictLabel()
	lines = append(lines, d.historyControlLine(historyFocusConflict, "On conflict:", conflictVal))
	replaceVal := d.restoreReplaceLabel()
	lines = append(lines, d.historyControlLine(historyFocusReplace, "Replace:", replaceVal))
	trusted := "[ ]"
	if d.trustedSchemaSQL {
		trusted = "[x]"
	}
	lines = append(lines, d.historyControlLine(historyFocusTrust, "", trusted+" Trust schema.sql for this restore"))
	lines = append(lines, renderDumpHistoryLines(&d.draft.History, maxLines-5)...)
	return lines
}

func (d *dumpScreen) historyControlLine(focus int, label, value string) string {
	prefix := "  "
	if d.historyFocus == focus && (focus != historyFocusPath || d.restoreDirFocus) {
		prefix = "> "
	}
	if focus == historyFocusPath && d.restoreDirFocus {
		prefix = "> "
	}
	line := prefix
	if label != "" {
		line += StyleAccent.Render(label) + " "
	}
	if value == "" {
		value = StyleMuted.Render("(empty)")
	}
	return line + value
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
