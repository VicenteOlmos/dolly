package tui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/dumphistory"
)

const capabilitiesStripLines = 1

func redactUserError(err error) string {
	if err == nil {
		return ""
	}
	return connections.RedactMessage(err.Error())
}

type App struct {
	screen            Screen
	showContextHelp   bool
	showKeysHelp      bool
	helpPage          int
	statusMsg         string
	width             int
	height            int
	conn              ConnectionDraft
	schema            SchemaDraft
	dump              DumpDraft
	clone             CloneDraft
	screens           [5]ScreenModel
	cfg               *config.Config
	cfgPath           string
	db                *sql.DB
	loader            SchemaLoader
	dumpRunner        DumpRunner
	restoreRunner     RestoreRunner
	dumpHistoryStore  dumphistory.Store
	dumpRunOutputDir  string
	dumpRunSeq        int
	dumpRunSchemas    []string
	restoreRunning    bool
	restoreCh         <-chan tea.Msg
	restoreCancel     context.CancelFunc
	cloneRunner       CloneRunner
	folderOpener      FolderOpener
	connStatus        ConnectionStatus
	connectError      string
	dumpStatus        DumpStatus
	dumpError         string
	dumpLog           []string
	dumpResult        *DumpResultSummary
	dumpCancel        context.CancelFunc
	dumpCh            <-chan tea.Msg
	cloneStatus       CloneStatus
	cloneError        string
	cloneLog          []string
	cloneCancel       context.CancelFunc
	cloneCh           <-chan tea.Msg
	dumpProgress      *DumpProgressEvent
	restoreProgress   *RestoreProgressEvent
	cloneProgress     *CloneProgressEvent
	connStore         connections.ConnectionStore
	saveConnections   bool
	sectionEntry      SectionEntryMode
	activeProfile     *connections.Connection
	sourceSchemaNames []string
	spinnerFrame      int
	modal             *modalState
}

func NewApp() *App {
	return NewAppWithOptions(postgresSchemaLoader{}, productionDumpRunner{}, productionRestoreRunner{}, productionCloneRunner{}, nil, nil, false)
}

func NewAppWithLoader(loader SchemaLoader) *App {
	return NewAppWithOptions(loader, productionDumpRunner{}, productionRestoreRunner{}, productionCloneRunner{}, nil, nil, false)
}

// NewAppFromConfig builds the TUI with a loaded config and saved-connection store.
func NewAppFromConfig(store connections.ConnectionStore, saveConnections bool, cfg *config.Config, cfgPath string) *App {
	app := NewAppWithOptions(postgresSchemaLoader{}, productionDumpRunner{}, productionRestoreRunner{}, productionCloneRunner{}, nil, store, saveConnections)
	app.cfg = cfg
	app.cfgPath = cfgPath
	if cfg != nil {
		app.dump.OutputDir = cfg.Dump.OutputDir
		if cfg.TUI.SectionEntry != "" {
			app.sectionEntry = ParseSectionEntry(cfg.TUI.SectionEntry)
		}
		InitStyles(cfg.TUI.Theme)
	}
	app.applyAllSectionEntries()
	app.applyDefaultConnection()
	app.refreshDumpHistoryList()
	return app
}

func NewAppWithOptions(loader SchemaLoader, runner DumpRunner, restoreRunner RestoreRunner, cloneRunner CloneRunner, folderOpener FolderOpener, store connections.ConnectionStore, saveConnections bool) *App {
	if folderOpener == nil {
		folderOpener = defaultFolderOpener{}
	}
	app := &App{
		screen:          ScreenConnection,
		loader:          loader,
		dumpRunner:      runner,
		restoreRunner:   restoreRunner,
		cloneRunner:     cloneRunner,
		folderOpener:    folderOpener,
		connStore:       store,
		saveConnections: saveConnections,
		sectionEntry:    SectionEntryInside,
		width:           80,
		height:          24,
	}
	app.screens[ScreenConnection] = newConnectionScreen(
		&app.conn, &app.connStatus, &app.connectError, store, saveConnections,
		app.defaultConnectionName, app.setDefaultConnectionProfile, &app.spinnerFrame, app.sectionEntry,
	)
	app.screens[ScreenSchema] = newSchemaScreen(&app.schema, app.hasSession)
	app.screens[ScreenDump] = newDumpScreen(&app.dump, app.hasSession, &app.dumpStatus, &app.dumpLog, &app.dumpError, &app.dumpResult, &app.spinnerFrame, &app.dumpProgress, &app.restoreProgress, &app.restoreRunning)
	app.screens[ScreenClone] = newCloneScreen(&app.clone, app.hasSession, &app.cloneStatus, &app.cloneLog, &app.cloneError, &app.spinnerFrame, store, saveConnections, func() *config.Config { return app.cfg }, func() string { return app.conn.DSN() }, &app.cloneProgress)
	app.screens[ScreenConfig] = newConfigScreen(func() *config.Config { return app.cfg }, func() string { return app.cfgPath })
	return app
}

func (a *App) hasSession() bool {
	return a.db != nil
}

// ConnectionDraft returns the current connection form values.
func (a *App) ConnectionDraft() ConnectionDraft {
	return a.conn
}

// PrefillConnection sets non-empty fields from d onto the app's connection
// draft. It never initiates a DB connection.
func (a *App) PrefillConnection(d ConnectionDraft) {
	if d.Host != "" {
		a.conn.Host = d.Host
	}
	if d.Port != "" {
		a.conn.Port = d.Port
	}
	if d.Database != "" {
		a.conn.Database = d.Database
	}
	if d.User != "" {
		a.conn.User = d.User
	}
	if d.Password != "" {
		a.conn.Password = d.Password
	}
	if d.SSLMODE != "" {
		a.conn.SSLMODE = d.SSLMODE
	}
}

func (a *App) Init() tea.Cmd {
	return scheduleSpinnerTick()
}

func (a *App) spinnerActive() bool {
	return a.connStatus == ConnStatusConnecting ||
		a.dumpStatus == DumpStatusRunning ||
		a.restoreRunning ||
		a.cloneStatus == CloneStatusRunning ||
		a.clone.AnalyzeState.Loading
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateSizeWarning()
		return a, nil
	case connectRequestedMsg:
		return a.handleConnectRequested(msg)
	case connectResultMsg:
		return a.handleConnectResult(msg)
	case testConnectionRequestedMsg:
		return a.handleTestConnectionRequested(msg)
	case testConnectionResultMsg:
		return a.handleTestConnectionResult(msg)
	case dumpRequestedMsg:
		return a.handleDumpRequested()
	case dumpProgressMsg:
		return a.handleDumpProgress(msg)
	case dumpResultMsg:
		return a.handleDumpResult(msg)
	case restoreConfirmRequestedMsg:
		return a.handleRestoreConfirmRequested(msg)
	case restoreRequestedMsg:
		return a.handleRestoreRequested(msg)
	case restoreResultMsg:
		return a.handleRestoreResult(msg)
	case restoreProgressMsg:
		return a.handleRestoreProgress(msg)
	case cloneRequestedMsg:
		return a.handleCloneRequested()
	case cloneProceedMsg:
		return a.handleCloneProceed()
	case cloneProgressMsg:
		return a.handleCloneProgress(msg)
	case cloneResultMsg:
		return a.handleCloneResult(msg)
	case analyzeResultMsg:
		return a.handleAnalyzeResult(msg)
	case spinnerTickMsg:
		if a.spinnerActive() {
			a.spinnerFrame = (a.spinnerFrame + 1) % spinnerFrameCount
		}
		return a, scheduleSpinnerTick()
	case saveConfigRequestedMsg:
		return a.handleSaveConfig()
	case requestDeleteProfileMsg:
		a.mountDeleteProfileModal(msg.name)
		return a, nil
	case profileDeleteConfirmedMsg:
		if a.connStore != nil {
			if err := a.connStore.Delete(msg.name); err != nil {
				if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
					cs.listErr = err.Error()
				}
			} else {
				if a.cfg != nil && a.cfg.Connections.Default == msg.name {
					_ = a.setDefaultConnectionProfile("")
				}
				if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
					cs.listErr = ""
					cs.refreshProfiles()
				}
			}
		}
		return a, nil
	}

	if _, ok := msg.(tea.WindowSizeMsg); !ok && a.modalOpen() {
		next, cmd, _ := a.updateModal(msg)
		return next.(*App), cmd
	}

	if handled, cmd := a.handleDumpResultKeys(msg); handled {
		return a, cmd
	}

	if handled, cmd := a.handleCloneResultKeys(msg); handled {
		return a, cmd
	}

	if a.handleDumpAbortKeys(msg) {
		return a, nil
	}

	if a.handleRestoreAbortKeys(msg) {
		return a, nil
	}

	if a.handleCloneAbortKeys(msg) {
		return a, nil
	}

	if handled, cmd := a.handleRunKeys(msg); handled {
		return a, cmd
	}

	if a.shouldDeferFieldCursorKeys(msg) {
		if cmd := a.screens[a.screen].Update(msg); cmd != nil {
			return a, cmd
		}
		return a, nil
	}

	if a.handleGlobalKeys(msg) {
		return a, a.globalCmd(msg)
	}

	if a.screen == ScreenDump && a.dumpStatus == DumpStatusComplete {
		return a, nil
	}

	if a.screen == ScreenClone && a.cloneStatus == CloneStatusComplete {
		return a, nil
	}

	if cmd := a.screens[a.screen].Update(msg); cmd != nil {
		return a, cmd
	}
	return a, nil
}

func (a *App) handleConnectRequested(msg connectRequestedMsg) (tea.Model, tea.Cmd) {
	if a.connStatus == ConnStatusConnecting {
		return a, nil
	}
	a.connStatus = ConnStatusConnecting
	a.connectError = ""
	a.statusMsg = "Connecting…"
	return a, ConnectCmd(a.loader, msg.dsn, msg.schemas)
}

func (a *App) handleTestConnectionRequested(msg testConnectionRequestedMsg) (tea.Model, tea.Cmd) {
	if a.connStatus == ConnStatusConnecting {
		return a, nil
	}
	a.connStatus = ConnStatusConnecting
	a.connectError = ""
	a.statusMsg = "Connecting…"
	return a, TestConnectionCmd(a.loader, msg.dsn)
}

func (a *App) handleTestConnectionResult(msg testConnectionResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.connStatus = ConnStatusError
		safeErr := redactUserError(msg.err)
		a.connectError = safeErr
		a.statusMsg = truncateStatus(StyleWarning.Render("Connection failed: "+safeErr), a.width)
		a.screen = ScreenConnection
		return a, nil
	}
	a.connStatus = ConnStatusIdle
	a.connectError = ""
	a.statusMsg = truncateStatus(StyleBase.Render("Connection OK"), a.width)
	a.screen = ScreenConnection
	return a, nil
}

func (a *App) handleConnectResult(msg connectResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if msg.db != nil {
			_ = msg.db.Close()
		}
		a.connStatus = ConnStatusError
		safeErr := redactUserError(msg.err)
		a.connectError = safeErr
		a.statusMsg = truncateStatus(StyleWarning.Render("Connection failed: "+safeErr), a.width)
		a.screen = ScreenConnection
		return a, nil
	}
	a.closeDB()
	a.db = msg.db
	a.connStatus = ConnStatusConnected
	a.connectError = ""
	a.statusMsg = ""

	if a.saveConnections && a.connStore != nil {
		prof := connectionFromDraft(a.conn, "", nil)
		if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
			if picked := cs.PickedProfile(); picked != nil {
				prof.Schemas = append([]string(nil), picked.Schemas...)
				prof.Name = picked.Name
			}
			cs.ClearPickedProfile()
		}
		updated, err := a.connStore.UpsertBySignature(prof)
		if err != nil {
			a.statusMsg = truncateStatus(StyleWarning.Render("Save profile: "+err.Error()), a.width)
		} else {
			a.activeProfile = &updated
		}
	}

	a.sourceSchemaNames = append([]string(nil), msg.sourceSchemaNames...)
	profileSchemas := a.profileSchemaDefaults()
	SeedSchemaPicker(&a.dump.SchemaPicker, msg.sourceSchemaNames, profileSchemas)
	SeedSchemaPicker(&a.clone.SchemaPicker, msg.sourceSchemaNames, profileSchemas)
	a.schema = schemaDraftFromTables(msg.tables)

	// Auto-prefill clone name from config template if empty.
	if a.clone.CloneName == "" && a.cfg != nil {
		a.clone.CloneName = cloneName(a.conn.Database, a.cfg.Clone.NameTemplate)
	}
	a.resolveCloneTargetDSN()
	a.refreshDumpHistoryList()

	a.screen = ScreenSchema
	return a, nil
}

func (a *App) profileSchemaDefaults() []string {
	if a.activeProfile == nil || len(a.activeProfile.Schemas) == 0 {
		return nil
	}
	return append([]string(nil), a.activeProfile.Schemas...)
}

func (a *App) persistProfileSchemas(schemas []string) {
	if !a.saveConnections || a.connStore == nil || a.activeProfile == nil {
		return
	}
	prof := *a.activeProfile
	prof.Schemas = append([]string(nil), schemas...)
	updated, err := a.connStore.UpsertBySignature(prof)
	if err != nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Save schemas: "+err.Error()), a.width)
		return
	}
	a.activeProfile = &updated
}

func (a *App) handleDumpRequested() (tea.Model, tea.Cmd) {
	if a.db == nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Connect first (screen 1)"), a.width)
		return a, nil
	}
	if a.dump.OutputDir == "" {
		a.statusMsg = truncateStatus(StyleWarning.Render("Set output directory"), a.width)
		return a, nil
	}
	schemas := a.dump.SchemaPicker.SelectedNames()
	if len(schemas) == 0 {
		a.statusMsg = truncateStatus(StyleWarning.Render("Select schemas on dump screen"), a.width)
		return a, nil
	}
	if a.dumpStatus == DumpStatusRunning {
		return a, nil
	}
	if a.restoreRunning {
		return a, nil
	}
	store, err := a.openDumpHistoryStore()
	if err != nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Dump history: "+err.Error()), a.width)
		return a, nil
	}
	outputDir, seq, err := dumphistory.AllocateDir(a.dump.OutputDir, store)
	if err != nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Allocate dump dir: "+err.Error()), a.width)
		return a, nil
	}
	a.dumpRunOutputDir = outputDir
	a.dumpRunSeq = seq
	a.dumpRunSchemas = append([]string(nil), schemas...)
	a.clearDumpResult()
	a.dumpStatus = DumpStatusRunning
	a.dumpError = ""
	if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
		ds.resetLogScroll()
	}
	a.statusMsg = fmt.Sprintf("Dumping… (#%d)", seq)
	a.persistProfileSchemas(schemas)
	ctx := context.Background()
	cmd, ch, cancel := startDumpCmd(a.dumpRunner, ctx, a.db, outputDir, a.dump, schemas, a.conn.Database, a.conn.DSN())
	a.dumpCh = ch
	a.dumpCancel = cancel
	return a, cmd
}

func (a *App) handleDumpProgress(msg dumpProgressMsg) (tea.Model, tea.Cmd) {
	appendDumpLog(&a.dumpLog, msg.line)
	if msg.ev != nil {
		a.dumpProgress = msg.ev
	}
	if a.dumpCh == nil {
		return a, nil
	}
	return a, waitDumpCmd(a.dumpCh)
}

func (a *App) handleDumpResult(msg dumpResultMsg) (tea.Model, tea.Cmd) {
	a.dumpCancel = nil
	a.dumpCh = nil
	a.dumpProgress = nil
	outDir := a.dumpRunOutputDir
	if outDir == "" {
		outDir = a.dump.OutputDir
	}
	seq := a.dumpRunSeq
	schemas := append([]string(nil), a.dumpRunSchemas...)
	a.dumpRunOutputDir = ""
	a.dumpRunSeq = 0
	a.dumpRunSchemas = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			a.dumpStatus = DumpStatusIdle
			a.dumpResult = nil
			a.statusMsg = truncateStatus(StyleMuted.Render("Dump cancelled"), a.width)
			appendDumpLog(&a.dumpLog, "cancelled")
			return a, nil
		}
		summary := collectDumpResultSummary(outDir, msg.err)
		a.dumpResult = &summary
		a.dumpStatus = DumpStatusComplete
		safeErr := redactUserError(msg.err)
		a.dumpError = safeErr
		a.statusMsg = truncateStatus(StyleWarning.Render("Dump failed: "+safeErr), a.width)
		appendDumpLog(&a.dumpLog, "error: "+safeErr)
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			ds.resetFileListScroll()
		}
		return a, nil
	}
	summary := collectDumpResultSummary(outDir, nil)
	a.dumpResult = &summary
	a.dumpStatus = DumpStatusComplete
	a.dumpError = ""
	a.statusMsg = truncateStatus(StyleBase.Render("Dump complete"), a.width)
	appendDumpLog(&a.dumpLog, "dump complete")
	a.registerDumpHistory(outDir, seq, schemas)
	if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
		ds.resetFileListScroll()
	}
	return a, nil
}

func (a *App) restoreNeedsConfirm() (bool, string) {
	if a.cfg == nil {
		return false, ""
	}
	var parts []string
	if a.cfg.Clone.Replace {
		parts = append(parts, "truncate existing tables before restore")
	}
	if a.cfg.Clone.RestoreOnConflict == "upsert" {
		parts = append(parts, "overwrite conflicting rows (upsert)")
	}
	if len(parts) == 0 {
		return false, ""
	}
	return true, strings.Join(parts, "; ")
}

func (a *App) handleRestoreConfirmRequested(msg restoreConfirmRequestedMsg) (tea.Model, tea.Cmd) {
	needs, policy := a.restoreNeedsConfirm()
	if !needs {
		return a.handleRestoreRequested(restoreRequestedMsg{inputDir: msg.inputDir})
	}
	body := fmt.Sprintf("Path: %s\nTarget: %s\n\nThis will %s.", msg.inputDir, connections.RedactMessage(a.conn.DSN()), policy)
	a.mountRestoreConfirmModal("Restore dump?", body, msg.inputDir, nil)
	return a, nil
}

func (a *App) handleRestoreRequested(msg restoreRequestedMsg) (tea.Model, tea.Cmd) {
	if a.db == nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Connect first (screen 1)"), a.width)
		return a, nil
	}
	if a.restoreRunning || a.dumpStatus == DumpStatusRunning {
		a.statusMsg = truncateStatus(StyleWarning.Render("Wait for current operation"), a.width)
		return a, nil
	}
	if msg.inputDir == "" {
		a.statusMsg = truncateStatus(StyleWarning.Render("No dump selected"), a.width)
		return a, nil
	}
	if _, err := os.Stat(msg.inputDir); err != nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Dump not found: "+msg.inputDir), a.width)
		return a, nil
	}
	runner := a.restoreRunner
	if runner == nil {
		runner = productionRestoreRunner{}
	}
	schemas := restoreSchemasFromHistory(&a.dump.History, msg.inputDir)
	if len(schemas) == 0 {
		schemas = a.dump.SchemaPicker.SelectedNames()
	}
	a.restoreRunning = true
	a.restoreProgress = nil
	a.statusMsg = "Restoring…"
	appendDumpLog(&a.dumpLog, "restore started: "+msg.inputDir)
	cmd, ch, cancel := startRestoreCmd(runner, context.Background(), a.db, msg.inputDir, schemas, a.conn.DSN())
	a.restoreCh = ch
	a.restoreCancel = cancel
	return a, cmd
}

func (a *App) handleRestoreResult(msg restoreResultMsg) (tea.Model, tea.Cmd) {
	a.restoreRunning = false
	a.restoreProgress = nil
	a.restoreCh = nil
	a.restoreCancel = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			appendDumpLog(&a.dumpLog, "restore cancelled")
			a.statusMsg = truncateStatus(StyleMuted.Render("Restore cancelled"), a.width)
			return a, nil
		}
		safeErr := redactUserError(msg.err)
		appendDumpLog(&a.dumpLog, "restore error: "+safeErr)
		a.statusMsg = truncateStatus(StyleWarning.Render("Restore failed: "+safeErr), a.width)
		return a, nil
	}
	appendDumpLog(&a.dumpLog, "restore complete")
	a.statusMsg = truncateStatus(StyleBase.Render("Restore complete"), a.width)
	return a, nil
}

func (a *App) handleRestoreProgress(msg restoreProgressMsg) (tea.Model, tea.Cmd) {
	appendDumpLog(&a.dumpLog, msg.line)
	a.restoreProgress = &msg.ev
	if a.restoreCh == nil {
		return a, nil
	}
	return a, waitRestoreCmd(a.restoreCh)
}

func (a *App) clearDumpResult() {
	a.dumpResult = nil
	if a.dumpStatus == DumpStatusComplete {
		a.dumpStatus = DumpStatusIdle
	}
}

func (a *App) handleDumpResultKeys(msg tea.Msg) (bool, tea.Cmd) {
	if a.dumpStatus != DumpStatusComplete || a.screen != ScreenDump {
		return false, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, nil
	}
	if a.helpOverlayOpen() {
		return false, nil
	}
	k := key.Key()
	switch k.String() {
	case "o":
		if a.dumpResult != nil {
			_ = a.folderOpener.Open(a.dumpResult.OutputDir)
		}
		return true, nil
	case "esc", "escape":
		a.clearDumpResult()
		a.statusMsg = ""
		return true, nil
	case "enter":
		a.clearDumpResult()
		a.statusMsg = ""
		return true, func() tea.Msg { return dumpRequestedMsg{} }
	case "j", "down":
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			ds.scrollFileList(-1)
		}
		return true, nil
	case "k", "up":
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			ds.scrollFileList(1)
		}
		return true, nil
	}
	switch key.Code {
	case tea.KeyDown:
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			ds.scrollFileList(-1)
		}
		return true, nil
	case tea.KeyUp:
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			ds.scrollFileList(1)
		}
		return true, nil
	}
	return false, nil
}

func (a *App) handleDumpCancel() {
	if a.dumpCancel != nil {
		a.statusMsg = truncateStatus(StyleMuted.Render("Cancelling…"), a.width)
		a.dumpCancel()
	}
}

func (a *App) handleRestoreCancel() {
	if a.restoreCancel != nil {
		a.statusMsg = truncateStatus(StyleMuted.Render("Cancelling…"), a.width)
		a.restoreCancel()
	}
}

func (a *App) handleRestoreAbortKeys(msg tea.Msg) bool {
	if !a.restoreRunning || a.screen != ScreenDump {
		return false
	}
	if a.helpOverlayOpen() {
		return false
	}
	if _, ok := isCancelKey(msg); !ok {
		return false
	}
	a.mountCancelRunModal(modalCancelRun, "Cancel restore?", "Stop the running restore. The default restore transaction will roll back data changes.", func() tea.Cmd {
		a.handleRestoreCancel()
		return nil
	})
	return true
}

func (a *App) handleDumpAbortKeys(msg tea.Msg) bool {
	if a.dumpStatus != DumpStatusRunning {
		return false
	}
	if a.helpOverlayOpen() {
		return false
	}
	if _, ok := isCancelKey(msg); !ok {
		return false
	}
	a.mountCancelRunModal(modalCancelRun, "Cancel dump?", "Stop the running dump. Output files may be incomplete.", func() tea.Cmd {
		a.handleDumpCancel()
		return nil
	})
	return true
}

func (a *App) handleSaveConfig() (tea.Model, tea.Cmd) {
	if a.persistConfig(true) {
		return a, nil
	}
	return a, nil
}

// persistConfig commits pending edits and writes config.jsonc when dirty.
// showSavedStatus controls the success status bar message.
func (a *App) persistConfig(showSavedStatus bool) bool {
	if cs, ok := a.screens[ScreenConfig].(*configScreen); ok {
		if err := cs.commitPendingEdit(); err != nil {
			a.statusMsg = truncateStatus(StyleWarning.Render("Fix invalid field before save"), a.width)
			return false
		}
		if !cs.dirty && !showSavedStatus {
			return true
		}
	}
	if a.cfg == nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("No config loaded"), a.width)
		return false
	}
	if err := config.SaveConfig(a.cfg, a.cfgPath); err != nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Save failed: "+err.Error()), a.width)
		return false
	}
	InitStyles(a.cfg.TUI.Theme)
	if cs, ok := a.screens[ScreenConfig].(*configScreen); ok {
		cs.clearDirty()
	}
	if showSavedStatus {
		a.statusMsg = truncateStatus(StyleBase.Render("Config saved"), a.width)
	}
	return true
}

func (a *App) autoSaveConfigOnLeave() bool {
	return a.persistConfig(false)
}

func (a *App) switchToScreen(next Screen) bool {
	if a.screen == ScreenConfig && next != ScreenConfig {
		if !a.autoSaveConfigOnLeave() {
			return false
		}
	}
	a.screen = next
	a.resetScreenSectionOverview(next)
	if next == ScreenDump {
		a.refreshDumpHistoryList()
	}
	return true
}

func (a *App) closeDB() {
	if a.db != nil {
		_ = a.db.Close()
		a.db = nil
	}
}

func (a *App) helpOverlayOpen() bool {
	return a.showContextHelp || a.showKeysHelp
}

func (a *App) closeHelpOverlays() bool {
	if !a.helpOverlayOpen() {
		return false
	}
	a.showContextHelp = false
	a.showKeysHelp = false
	a.helpPage = 0
	return true
}

func (a *App) toggleKeysHelp() {
	a.showKeysHelp = !a.showKeysHelp
	if a.showKeysHelp {
		a.showContextHelp = false
		a.helpPage = 0
		return
	}
	a.helpPage = 0
}

func truncateStatus(s string, width int) string {
	if width <= 0 {
		return s
	}
	plain := stripANSI(s)
	if lipgloss.Width(plain) <= width {
		return s
	}
	truncated := truncateRunes(plain, width)
	if plain == s {
		return truncated
	}
	// Re-wrap with the same semantic style (status bar is faint; message keeps emphasis).
	if strings.Contains(plain, "failed") || strings.Contains(plain, "Failed") {
		return StyleWarning.Render(truncated)
	}
	return StyleBase.Render(truncated)
}

func (a *App) handleGlobalKeys(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}
	k := key.Key()

	switch k.String() {
	case "ctrl+c":
		a.mountQuitModal()
		return true
	case "?":
		a.showContextHelp = !a.showContextHelp
		if a.showContextHelp {
			a.showKeysHelp = false
			a.helpPage = 0
		}
		return true
	case "f1":
		a.toggleKeysHelp()
		return true
	case "n":
		if a.showKeysHelp {
			a.helpPage = (a.helpPage + 1) % HelpPageCount()
			return true
		}
	case "p":
		if a.showKeysHelp {
			total := HelpPageCount()
			a.helpPage = (a.helpPage + total - 1) % total
			return true
		}
	case "esc", "escape":
		if a.closeHelpOverlays() {
			return true
		}
		if a.shouldDeferEscToScreen() {
			return false
		}
		if a.dumpStatus == DumpStatusRunning && a.screen == ScreenDump {
			return false
		}
		if a.cloneStatus == CloneStatusRunning && a.screen == ScreenClone {
			return false
		}
		if a.restoreRunning && a.screen == ScreenDump {
			return false
		}
		a.mountQuitModal()
		return true
	}
	switch k.Code {
	case tea.KeyF1:
		a.toggleKeysHelp()
		return true
	}

	if len(k.Text) == 1 && k.Text[0] >= '1' && k.Text[0] <= '5' && k.Mod == 0 {
		if !a.shouldDeferGlobalDigitKeys() {
			next := Screen(k.Text[0] - '1')
			if next != a.screen {
				a.switchToScreen(next)
			}
			return true
		}
	}

	if k.Code == tea.KeyTab {
		if a.dumpStatus == DumpStatusRunning && a.screen == ScreenDump {
			return true
		}
		if a.cloneStatus == CloneStatusRunning && a.screen == ScreenClone {
			return true
		}
		if k.Mod&tea.ModCtrl != 0 {
			a.navigateScreenTab(k)
			return true
		}
		return false
	}

	return false
}

func (a *App) handleRunKeys(msg tea.Msg) (bool, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, nil
	}
	if a.helpOverlayOpen() {
		return false, nil
	}
	trigger, isLetterG := runKeyTrigger(key)
	if !trigger {
		return false, nil
	}
	if isLetterG && a.shouldDeferRunKey() {
		return false, nil
	}
	if a.screen == ScreenDump && a.dumpStatus == DumpStatusIdle && !a.restoreRunning {
		_, cmd := a.handleDumpRequested()
		return true, cmd
	}
	if a.screen == ScreenClone && a.cloneStatus == CloneStatusIdle {
		_, cmd := a.handleCloneRequested()
		return true, cmd
	}
	return false, nil
}

func (a *App) shouldDeferRunKey() bool {
	switch a.screen {
	case ScreenDump:
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			return ds.sectionActive(dumpSectionPath)
		}
	case ScreenClone:
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			if cs.sectionActive(cloneSectionForm) {
				return cs.activeField() != nil
			}
		}
	}
	return false
}

func (a *App) shouldDeferGlobalDigitKeys() bool {
	switch a.screen {
	case ScreenConnection:
		if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
			return cs.onFieldCursorNavigation() || cs.panel == connPanelSaveAs || cs.panel == connPanelRename
		}
	case ScreenDump:
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			return ds.onFieldCursorNavigation()
		}
	case ScreenClone:
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			return cs.onFieldCursorNavigation()
		}
	case ScreenConfig:
		if cs, ok := a.screens[ScreenConfig].(*configScreen); ok {
			return cs.editing
		}
	}
	return false
}

func (a *App) globalCmd(msg tea.Msg) tea.Cmd {
	return nil
}

func (a *App) navigateScreenTab(k tea.Key) {
	var next Screen
	if k.Mod&tea.ModShift != 0 {
		next = prevScreen(a.screen)
	} else {
		next = nextScreen(a.screen)
	}
	a.switchToScreen(next)
}

func restoreSchemasFromHistory(h *DumpHistoryState, inputDir string) []string {
	if h == nil {
		return nil
	}
	if sel := h.Selected(); sel != nil && sel.Path == inputDir && len(sel.Schemas) > 0 {
		return append([]string(nil), sel.Schemas...)
	}
	for _, e := range h.Entries {
		if e.Path == inputDir && len(e.Schemas) > 0 {
			return append([]string(nil), e.Schemas...)
		}
	}
	return nil
}

func (a *App) applyAllSectionEntries() {
	a.applyScreenSectionEntry(ScreenConnection)
	a.applyScreenSectionEntry(ScreenDump)
	a.applyScreenSectionEntry(ScreenClone)
}

func (a *App) defaultConnectionName() string {
	if a.cfg == nil {
		return ""
	}
	return strings.TrimSpace(a.cfg.Connections.Default)
}

func (a *App) setDefaultConnectionProfile(name string) error {
	if a.cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if a.cfgPath == "" {
		return fmt.Errorf("config path not set")
	}
	a.cfg.Connections.Default = strings.TrimSpace(name)
	return config.SaveConfig(a.cfg, a.cfgPath)
}

func (a *App) applyDefaultConnection() {
	if a.connStore == nil || !a.saveConnections {
		return
	}
	name := a.defaultConnectionName()
	if name == "" {
		return
	}
	prof, err := a.connStore.Get(name)
	if err != nil {
		return
	}
	a.PrefillConnection(draftFromConnection(prof))
	if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
		cs.positionOnDefaultOrFirst()
		if cs.panel == connPanelList {
			cs.previewListProfile()
		}
	}
}

func (a *App) resetScreenSectionOverview(screen Screen) {
	a.applyScreenSectionNav(screen, SectionEntryOverview)
}

func (a *App) applyScreenSectionEntry(screen Screen) {
	a.applyScreenSectionNav(screen, a.sectionEntry)
}

func (a *App) applyScreenSectionNav(screen Screen, entry SectionEntryMode) {
	switch screen {
	case ScreenConnection:
		if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
			cs.applySectionEntry(entry)
		}
	case ScreenDump:
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			if !ds.running() && !ds.complete() {
				ds.applySectionEntry(entry)
			}
		}
	case ScreenClone:
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			if !cs.running() && !cs.complete() {
				cs.applySectionEntry(entry)
			}
		}
	}
}

func (a *App) shouldDeferFieldCursorKeys(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}
	if !isFieldCursorNavigationKey(key.Key()) {
		return false
	}
	switch a.screen {
	case ScreenConnection:
		if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
			return cs.onFieldCursorNavigation()
		}
	case ScreenDump:
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			return ds.onFieldCursorNavigation()
		}
	case ScreenClone:
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			return cs.onFieldCursorNavigation()
		}
	case ScreenConfig:
		if cs, ok := a.screens[ScreenConfig].(*configScreen); ok {
			return cs.onFieldCursorNavigation()
		}
	}
	return false
}

func (a *App) shouldDeferEscToScreen() bool {
	if a.dumpStatus == DumpStatusComplete && a.screen == ScreenDump {
		return true
	}
	if a.cloneStatus == CloneStatusComplete && a.screen == ScreenClone {
		return true
	}
	if a.clone.AnalyzeState.Loading && a.screen == ScreenClone {
		return true
	}
	if a.screen == ScreenConnection {
		if cs, ok := a.screens[ScreenConnection].(*connectionScreen); ok {
			if cs.shouldDeferEsc() {
				return true
			}
		}
	}
	if a.screen == ScreenDump {
		if ds, ok := a.screens[ScreenDump].(*dumpScreen); ok {
			return ds.shouldDeferEsc()
		}
	}
	if a.screen == ScreenClone {
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			return cs.shouldDeferEsc()
		}
	}
	if a.screen == ScreenConfig {
		if cs, ok := a.screens[ScreenConfig].(*configScreen); ok && cs.editing {
			return true
		}
	}
	return false
}

func (a *App) updateSizeWarning() {
	if a.dumpStatus == DumpStatusRunning {
		a.statusMsg = "Dumping…"
		return
	}
	if a.cloneStatus == CloneStatusRunning {
		a.statusMsg = "Cloning…"
		return
	}
	if a.dumpStatus == DumpStatusComplete {
		return
	}
	if a.cloneStatus == CloneStatusComplete {
		return
	}
	if a.connStatus == ConnStatusConnecting {
		return
	}
	if a.connStatus == ConnStatusError && a.connectError != "" {
		a.statusMsg = truncateStatus(StyleWarning.Render("Connection failed: "+a.connectError), a.width)
		return
	}
	if a.dumpStatus == DumpStatusError && a.dumpError != "" {
		a.statusMsg = truncateStatus(StyleWarning.Render("Dump failed: "+a.dumpError), a.width)
		return
	}
	if a.width < minTerminalWidth || a.height < minTerminalHeight {
		a.statusMsg = StyleWarning.Render(fmt.Sprintf(
			"Terminal small (%dx%d); recommended %dx%d",
			a.width, a.height, minTerminalWidth, minTerminalHeight,
		))
		return
	}
	a.statusMsg = ""
}

func nextScreen(s Screen) Screen {
	return Screen((int(s) + 1) % 5)
}

func prevScreen(s Screen) Screen {
	return Screen((int(s) + 4) % 5)
}

func (a *App) contentSize() (int, int) {
	stripLines := capabilitiesStripLines
	statusLines := 1
	contentHeight := a.height - stripLines - statusLines
	if contentHeight < 1 {
		contentHeight = 1
	}
	contentWidth := a.width - ScreenNavWidth - 1
	if contentWidth < 1 {
		contentWidth = 1
	}
	return contentWidth, contentHeight
}

func (a *App) View() tea.View {
	contentW, contentH := a.contentSize()
	nav := RenderScreenNav(contentH, a.screen)
	var body string
	if a.showContextHelp || a.showKeysHelp {
		helpW := contentW / 2
		if helpW < 28 {
			helpW = 28
		}
		screenW := contentW - helpW - 1
		if screenW < 20 {
			screenW = 20
			helpW = max(20, contentW-screenW-1)
		}
		screenBody := a.screens[a.screen].View(screenW, contentH)
		var helpBody string
		if a.showKeysHelp {
			helpBody = RenderHelpSplit(a.screen, a.dumpStatus, a.cloneStatus, a.helpPage, helpW, contentH, a.saveConnections)
		} else {
			helpBody = RenderContextHelpSplit(a, helpW, contentH)
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, screenBody, helpBody)
	} else {
		body = a.screens[a.screen].View(contentW, contentH)
	}
	main := lipgloss.JoinHorizontal(lipgloss.Top, nav, body)
	strip := RenderCapabilitiesStrip(a.width)
	status := RenderStatusBar(a.width, a.screen, a.statusMsg, a.dumpStatus, a.cloneStatus, a.saveConnections)

	layout := main + "\n" + strip + "\n" + status
	if a.modalOpen() {
		layout = overlayModal(layout, a.renderModalBox(a.width), a.width, a.height)
	}

	v := tea.NewView(layout)
	// Do not set View foreground/background — Ghostty and other terminals keep
	// their palette on transparent cells (see gentle-ai TUI pattern).
	v.AltScreen = true
	return v
}
