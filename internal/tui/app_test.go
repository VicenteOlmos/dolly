package tui

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func keyPress(text string, code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: text, Code: code, Mod: mod}
}

func ctrlEnter() tea.KeyPressMsg {
	return keyPress("", tea.KeyEnter, tea.ModCtrl)
}

func enterConnectionFields(cs *connectionScreen) {
	cs.panel = connPanelFields
	cs.nav.EnterInside(connSectionFields)
}

func enterConnectionList(cs *connectionScreen) {
	cs.panel = connPanelList
	cs.nav.EnterInside(connSectionList)
	cs.refreshProfiles()
}

func enterDumpSection(ds *dumpScreen, section int) {
	ds.nav.EnterInside(section)
}

func updateApp(t *testing.T, keys ...tea.KeyPressMsg) *App {
	t.Helper()
	app := NewApp()
	app.width = 80
	app.height = 24
	for _, k := range keys {
		next, _ := app.Update(k)
		app = next.(*App)
	}
	return app
}

func TestNewAppStartsOnConnection(t *testing.T) {
	app := NewApp()
	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection", app.screen)
	}
}

func TestViewDoesNotOverrideTerminalTheme(t *testing.T) {
	app := NewApp()
	app.width = 80
	app.height = 24

	v := app.View()
	if !v.AltScreen {
		t.Fatal("expected alt screen view")
	}
	if v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Fatal("view must not set terminal fg/bg so Ghostty theme shows through")
	}
}

func TestAppScreenCount(t *testing.T) {
	app := NewApp()
	if len(app.screens) != 5 {
		t.Fatalf("screens len = %d, want 5", len(app.screens))
	}
	for i, s := range app.screens {
		if s == nil {
			t.Fatalf("screens[%d] is nil", i)
		}
	}
}

func TestAppDigitNavigatesAllScreens(t *testing.T) {
	app := NewApp()
	app.width = 80
	app.height = 24
	app.screen = ScreenSchema

	for _, pair := range []struct {
		key  rune
		want Screen
	}{
		{'3', ScreenDump},
		{'4', ScreenClone},
		{'5', ScreenConfig},
		{'1', ScreenConnection},
	} {
		app = drainUpdate(app, keyPress(string(pair.key), pair.key, 0))
		if app.screen != pair.want {
			t.Fatalf("screen = %v, want %v after %c", app.screen, pair.want, pair.key)
		}
	}
}

func TestAppDigitFromSchemaOpensDumpOverview(t *testing.T) {
	app := NewApp()
	app.width = 80
	app.height = 24
	app.screen = ScreenSchema

	app = drainUpdate(app, keyPress("3", '3', 0))
	if app.screen != ScreenDump {
		t.Fatalf("screen = %v, want dump after 3 from schema", app.screen)
	}
	ds := app.screens[ScreenDump].(*dumpScreen)
	if !ds.nav.InOverview() {
		t.Fatal("expected dump section overview after digit 3 from schema")
	}
}

func TestAppDigitFromConnectionOverview(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.sectionEntry = SectionEntryOverview
	app.applyScreenSectionEntry(ScreenConnection)
	app.width = 80
	app.height = 24
	cs := app.screens[ScreenConnection].(*connectionScreen)
	if cs.panel != connPanelOverview {
		t.Fatalf("panel = %v, want overview", cs.panel)
	}

	app = drainUpdate(app, keyPress("2", '2', 0))
	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema after 2 from overview", app.screen)
	}
}

func TestAppPlainTabStaysOnDump(t *testing.T) {
	app := NewApp()
	app.screen = ScreenDump
	app.dumpStatus = DumpStatusIdle
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", tea.KeyTab, 0))
	if app.screen != ScreenDump {
		t.Fatalf("screen = %v, want dump after plain Tab", app.screen)
	}
	ds := app.screens[ScreenDump].(*dumpScreen)
	if ds.nav.InInside() {
		t.Fatal("expected overview after plain Tab on dump")
	}
}

func TestAppCtrlTabNavigatesToConfig(t *testing.T) {
	app := updateApp(t, keyPress("", tea.KeyTab, tea.ModCtrl))
	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema after Ctrl+Tab from connection", app.screen)
	}
}

func TestAppNextScreenFromConfigWraps(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24
	next, _ := app.Update(keyPress("", tea.KeyTab, tea.ModCtrl))
	got := next.(*App).screen
	if got != ScreenConnection {
		t.Fatalf("nextScreen from config = %v, want connection (wrap)", got)
	}
}

func TestAppDumpOverviewDrillSection(t *testing.T) {
	app := NewApp()
	app.screen = ScreenDump
	app.dumpStatus = DumpStatusIdle
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	ds := app.screens[ScreenDump].(*dumpScreen)
	if ds.nav.Section != dumpSectionPicker {
		t.Fatalf("section = %d, want schema after ↓ in overview", ds.nav.Section)
	}

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	ds = app.screens[ScreenDump].(*dumpScreen)
	if !ds.nav.InInside() || ds.nav.Section != dumpSectionPicker {
		t.Fatalf("nav = %+v, want inside schemas", ds.nav)
	}
}

func TestAppCloneInsideArrowMovesFormField(t *testing.T) {
	app := NewApp()
	app.screen = ScreenClone
	app.cloneStatus = CloneStatusIdle
	app.width = 80
	app.height = 24
	cs := app.screens[ScreenClone].(*cloneScreen)
	cs.nav.EnterInside(cloneSectionForm)
	cs.formField = 0

	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	cs = app.screens[ScreenClone].(*cloneScreen)
	if cs.formField != 1 {
		t.Fatalf("formField = %d, want 1 after ↓ in fields section", cs.formField)
	}
}

func TestAppCtrlTabFromSchemaPicker(t *testing.T) {
	app := NewApp()
	app.screen = ScreenClone
	app.cloneStatus = CloneStatusIdle
	app.width = 80
	app.height = 24
	if cs, ok := app.screens[ScreenClone].(*cloneScreen); ok {
		cs.nav.EnterInside(cloneSectionPicker)
	}

	next, _ := app.Update(keyPress("", tea.KeyTab, tea.ModCtrl|tea.ModShift))
	got := next.(*App)
	if got.screen != ScreenDump {
		t.Fatalf("screen = %v, want dump from Ctrl+Shift+Tab on schema picker", got.screen)
	}
}

func TestAppNavigation(t *testing.T) {
	tests := []struct {
		name       string
		keys       []tea.KeyPressMsg
		wantScreen Screen
	}{
		{
			name:       "ctrl tab next screen",
			keys:       []tea.KeyPressMsg{keyPress("", tea.KeyTab, tea.ModCtrl)},
			wantScreen: ScreenSchema,
		},
		{
			name:       "ctrl shift tab previous screen",
			keys:       []tea.KeyPressMsg{keyPress("", tea.KeyTab, tea.ModCtrl), keyPress("", tea.KeyTab, tea.ModCtrl|tea.ModShift)},
			wantScreen: ScreenConnection,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := updateApp(t, tt.keys...)
			if app.screen != tt.wantScreen {
				t.Fatalf("screen = %v, want %v", app.screen, tt.wantScreen)
			}
		})
	}
}

func TestAppHelpOverlay(t *testing.T) {
	app := NewApp()
	app.width = 80
	app.height = 24

	var next tea.Model
	next, _ = app.Update(keyPress("?", '?', 0))
	app = next.(*App)
	if !app.showContextHelp {
		t.Fatal("expected context help visible after ?")
	}
	if app.showKeysHelp {
		t.Fatal("context help should not open keys help")
	}

	next, _ = app.Update(keyPress("f1", 0, 0))
	app = next.(*App)
	if !app.showKeysHelp {
		t.Fatal("expected keys help visible after F1")
	}
	if app.showContextHelp {
		t.Fatal("keys help should replace context help")
	}
	if app.helpPage != 0 {
		t.Fatalf("helpPage = %d, want 0 on open", app.helpPage)
	}
	next, _ = app.Update(keyPress("n", 'n', 0))
	app = next.(*App)
	if app.helpPage != 1 {
		t.Fatalf("helpPage = %d, want 1 after n", app.helpPage)
	}
	next, _ = app.Update(keyPress("p", 'p', 0))
	app = next.(*App)
	if app.helpPage != 0 {
		t.Fatalf("helpPage = %d, want 0 after p", app.helpPage)
	}

	clonePage := -1
	for i, cmd := range CLICatalog() {
		if cmd.Name == "clone" {
			clonePage = i + 1
			break
		}
	}
	if clonePage < 0 {
		t.Fatal("clone command missing from catalog")
	}
	for app.helpPage != clonePage {
		next, _ = app.Update(keyPress("n", 'n', 0))
		app = next.(*App)
	}
	if app.helpPage != clonePage {
		t.Fatalf("helpPage = %d, want clone CLI page %d", app.helpPage, clonePage)
	}
	var cloneCmd CLICommand
	for _, cmd := range CLICatalog() {
		if cmd.Name == "clone" {
			cloneCmd = cmd
			break
		}
	}
	cloneHelp := stripANSI(RenderCLIHelp(cloneCmd, 60))
	if !strings.Contains(cloneHelp, "interactive dump + restore") {
		t.Fatalf("clone CLI help missing summary: %s", cloneHelp)
	}

	next, _ = app.Update(keyPress("f1", 0, 0))
	app = next.(*App)
	if app.showKeysHelp {
		t.Fatal("expected keys help hidden after second F1")
	}
	if app.helpPage != 0 {
		t.Fatalf("helpPage = %d, want 0 after close", app.helpPage)
	}
	next, _ = app.Update(keyPress("?", '?', 0))
	app = next.(*App)
	next, _ = app.Update(keyPress("", tea.KeyEscape, 0))
	app = next.(*App)
	if app.helpOverlayOpen() {
		t.Fatal("expected help hidden after esc")
	}
	if app.helpPage != 0 {
		t.Fatalf("helpPage = %d, want 0 after esc close", app.helpPage)
	}
}

func TestAppResizeWarning(t *testing.T) {
	app := NewApp()
	next, _ := app.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	got := next.(*App)
	if got.statusMsg == "" {
		t.Fatal("expected size warning in statusMsg")
	}
	next, _ = got.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got = next.(*App)
	if got.statusMsg != "" {
		t.Fatalf("expected empty statusMsg, got %q", got.statusMsg)
	}
}

func TestAppQuitRequiresConfirmation(t *testing.T) {
	app := NewApp()
	next, cmd := app.Update(keyPress("", tea.KeyEscape, 0))
	app = next.(*App)
	if cmd != nil {
		t.Fatal("expected no immediate quit before confirmation")
	}
	if app.modalOpen() {
		t.Fatal("expected first Esc to leave inside fields, not quit")
	}
	conn := app.screens[ScreenConnection].(*connectionScreen)
	if !conn.nav.InOverview() {
		t.Fatal("expected overview after first Esc from fields")
	}
	next, cmd = app.Update(keyPress("", tea.KeyEscape, 0))
	app = next.(*App)
	if cmd != nil {
		t.Fatal("expected no immediate quit before confirmation")
	}
	if !app.modalOpen() {
		t.Fatal("expected quit modal after Esc on overview")
	}
	next, cmd = app.Update(keyPress("y", 'y', 0))
	if cmd == nil {
		t.Fatal("expected quit command after Y")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", cmd())
	}
	_ = next
}

func TestConfigEditStringUpdatesValue(t *testing.T) {
	app := NewApp()
	cfg := config.DefaultConfig()
	app.cfg = cfg
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	// cursor=0 is env.path; press Enter to enter edit mode
	next, _ := app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	cs := app.screens[ScreenConfig].(*configScreen)
	if !cs.editing {
		t.Fatal("expected editing=true after Enter on string field")
	}

	// Clear the current value and type a new one
	// Backspace enough to clear ".env"
	for i := 0; i < 10; i++ {
		next, _ = app.Update(keyPress("", tea.KeyBackspace, 0))
		app = next.(*App)
	}
	// Type new value
	for _, ch := range "newpath" {
		next, _ = app.Update(keyPress(string(ch), rune(ch), 0))
		app = next.(*App)
	}
	// Confirm with Enter
	next, _ = app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	cs = app.screens[ScreenConfig].(*configScreen)
	if cs.editing {
		t.Fatal("expected editing=false after confirm Enter")
	}
	if app.cfg.Env.Path != "newpath" {
		t.Fatalf("Env.Path = %q, want %q", app.cfg.Env.Path, "newpath")
	}
}

func TestConfigEditInvalidIntRejected(t *testing.T) {
	app := NewApp()
	cfg := config.DefaultConfig()
	cfg.Subset.Percent = 42
	app.cfg = cfg
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	// Navigate cursor to subset.percent (index 19)
	cs := app.screens[ScreenConfig].(*configScreen)
	cs.cursor = 18

	// Enter edit mode
	next, _ := app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	cs = app.screens[ScreenConfig].(*configScreen)
	if !cs.editing {
		t.Fatal("expected editing=true")
	}

	// Clear and type invalid value
	for i := 0; i < 5; i++ {
		next, _ = app.Update(keyPress("", tea.KeyBackspace, 0))
		app = next.(*App)
	}
	for _, ch := range "notanint" {
		next, _ = app.Update(keyPress(string(ch), rune(ch), 0))
		app = next.(*App)
	}
	// Confirm
	next, _ = app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	cs = app.screens[ScreenConfig].(*configScreen)

	if cs.editErr == "" {
		t.Fatal("expected editErr to be set for invalid int")
	}
	if app.cfg.Subset.Percent != 42 {
		t.Fatalf("Subset.Percent = %d, want 42 (unchanged)", app.cfg.Subset.Percent)
	}
	if !cs.editing {
		t.Fatal("expected editing=true after rejection")
	}
}

func TestConfigCtrlSSavesConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.jsonc"
	if err := os.WriteFile(path, config.DefaultTemplate(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Env.Path = ".env.saved"

	app := NewAppFromConfig(nil, false, cfg, path)
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", 's', tea.ModCtrl))

	if app.statusMsg == "" {
		t.Fatal("expected statusMsg after Ctrl+S save")
	}
	if !strings.Contains(stripANSI(app.statusMsg), "saved") {
		t.Fatalf("statusMsg = %q, want 'Config saved'", app.statusMsg)
	}
	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got.Env.Path != ".env.saved" {
		t.Fatalf("Env.Path on disk = %q, want %q", got.Env.Path, ".env.saved")
	}
}

func TestConfigCtrlSSavesPendingEdit(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.jsonc"
	if err := os.WriteFile(path, config.DefaultTemplate(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	app := NewAppFromConfig(nil, false, cfg, path)
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	for i := 0; i < 10; i++ {
		app = drainUpdate(app, keyPress("", tea.KeyBackspace, 0))
	}
	for _, ch := range "savedpath" {
		app = drainUpdate(app, keyPress(string(ch), ch, 0))
	}
	app = drainUpdate(app, keyPress("", 's', tea.ModCtrl))

	cs := app.screens[ScreenConfig].(*configScreen)
	if cs.editing {
		t.Fatal("expected pending edit to be committed before save")
	}
	if app.cfg.Env.Path != "savedpath" {
		t.Fatalf("Env.Path in memory = %q, want savedpath", app.cfg.Env.Path)
	}
	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got.Env.Path != "savedpath" {
		t.Fatalf("Env.Path on disk = %q, want savedpath", got.Env.Path)
	}
}

func TestConfigAutoSaveOnLeaveScreen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.jsonc"
	if err := os.WriteFile(path, config.DefaultTemplate(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	app := NewAppFromConfig(nil, false, cfg, path)
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	cs := app.screens[ScreenConfig].(*configScreen)
	for i, f := range cs.fields {
		if f.Section == "env" && f.Label == "path" {
			cs.cursor = i
			break
		}
	}
	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	for i := 0; i < 4; i++ {
		app = drainUpdate(app, keyPress("", tea.KeyBackspace, 0))
	}
	for _, ch := range "autosave" {
		app = drainUpdate(app, keyPress(string(ch), ch, 0))
	}
	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	app = drainUpdate(app, keyPress("1", '1', 0))

	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection", app.screen)
	}
	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Env.Path != "autosave" {
		t.Fatalf("Env.Path on disk = %q, want autosave", got.Env.Path)
	}
}

func TestConfigCtrlSBlocksOnInvalidEdit(t *testing.T) {
	app := NewApp()
	cfg := config.DefaultConfig()
	app.cfg = cfg
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	cs := app.screens[ScreenConfig].(*configScreen)
	for i, f := range cs.fields {
		if f.Section == "subset" && f.Label == "percent" {
			cs.cursor = i
			break
		}
	}
	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	app = drainUpdate(app, keyPress("x", 'x', 0))
	app = drainUpdate(app, keyPress("", 's', tea.ModCtrl))

	if cs.editErr == "" || !containsPlain(cs.editErr, "invalid") {
		t.Fatalf("editErr = %q, want invalid field warning", cs.editErr)
	}
	if !cs.editing {
		t.Fatal("expected to stay in edit mode after invalid save")
	}
}

func TestConfigStrategyCycles(t *testing.T) {
	app := NewApp()
	cfg := config.DefaultConfig()
	app.cfg = cfg
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	cs := app.screens[ScreenConfig].(*configScreen)
	for i, f := range cs.fields {
		if f.Label == "strategy" {
			cs.cursor = i
			break
		}
	}

	next, _ := app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	if cfg.Clone.Strategy != "template" {
		t.Fatalf("Strategy = %q, want template after Enter", cfg.Clone.Strategy)
	}

	next, _ = app.Update(keyPress("", 'h', 0))
	app = next.(*App)
	if cfg.Clone.Strategy != "schema-replay" {
		t.Fatalf("Strategy = %q, want schema-replay after Left", cfg.Clone.Strategy)
	}
}

func TestConfigEscCancelsEdit(t *testing.T) {
	app := NewApp()
	cfg := config.DefaultConfig()
	app.cfg = cfg
	app.screen = ScreenConfig
	app.width = 80
	app.height = 24

	// Enter edit mode on env.path
	next, _ := app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	cs := app.screens[ScreenConfig].(*configScreen)
	if !cs.editing {
		t.Fatal("expected editing=true")
	}

	// Esc should cancel edit, not open quit modal
	next, _ = app.Update(keyPress("", tea.KeyEscape, 0))
	app = next.(*App)
	cs = app.screens[ScreenConfig].(*configScreen)
	if cs.editing {
		t.Fatal("expected editing=false after Esc")
	}
	if app.modalOpen() {
		t.Fatal("expected no quit modal when Esc cancels edit")
	}
}

func TestAppQDoesNotQuitWhileTyping(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection
	next, _ := app.Update(keyPress("q", 'q', 0))
	app = next.(*App)
	if app.conn.Host != "q" {
		t.Fatalf("Host = %q, want q typed into field", app.conn.Host)
	}
	next, cmd := app.Update(keyPress("", tea.KeyEscape, 0))
	app = next.(*App)
	if app.modalOpen() {
		t.Fatal("expected first Esc to leave fields, not quit")
	}
	next, cmd = app.Update(keyPress("", tea.KeyEscape, 0))
	app = next.(*App)
	if !app.modalOpen() {
		t.Fatal("expected quit modal from Esc on overview")
	}
	if cmd != nil {
		t.Fatal("expected no immediate quit before confirmation")
	}
}

func TestComputeETA(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		current int
		total   int
		wantOK  bool
	}{
		{"zero current", 1 * time.Second, 0, 10, false},
		{"first event", 1 * time.Second, 1, 10, false},
		{"halfway", 10 * time.Second, 5, 10, true},
		{"complete", 10 * time.Second, 10, 10, false},
		{"over complete", 10 * time.Second, 11, 10, false},
		{"zero total", 1 * time.Second, 1, 0, false},
		{"negative total", 1 * time.Second, 1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eta, ok := computeETA(tt.elapsed, tt.current, tt.total)
			if ok != tt.wantOK {
				t.Fatalf("computeETA(%v, %d, %d) ok = %v, want %v", tt.elapsed, tt.current, tt.total, ok, tt.wantOK)
			}
			if ok && eta < 0 {
				t.Fatalf("computeETA returned negative eta: %v", eta)
			}
		})
	}
}

func TestComputeETAValue(t *testing.T) {
	// 10s elapsed, 3 of 6 done → remaining 3 items at 10/3 = 3.33s each → ~10s ETA
	eta, ok := computeETA(10*time.Second, 3, 6)
	if !ok {
		t.Fatal("expected ok for halfway progress")
	}
	if eta != 10*time.Second {
		t.Fatalf("eta = %v, want 10s", eta)
	}
}

func TestRenderProgressBar(t *testing.T) {
	bar := renderProgressBar(40, 5, 10, int64(5*time.Second), "")
	if !strings.Contains(bar, "50%") {
		t.Fatalf("bar missing 50%%: %q", bar)
	}
	if !strings.Contains(stripANSIForGolden(bar), "▓") {
		t.Fatalf("bar missing filled chars: %q", bar)
	}
	if !strings.Contains(bar, "░") {
		t.Fatalf("bar missing track chars: %q", bar)
	}
}

func TestRenderProgressBarETA(t *testing.T) {
	bar := renderProgressBar(40, 3, 6, int64(10*time.Second), "")
	if !strings.Contains(bar, "ETA") {
		t.Fatalf("bar missing ETA: %q", bar)
	}
}

func TestRenderProgressBarNoETAOnFirstEvent(t *testing.T) {
	bar := renderProgressBar(40, 1, 10, int64(1*time.Second), "")
	if strings.Contains(bar, "ETA") {
		t.Fatalf("bar should not have ETA on first event: %q", bar)
	}
}

func TestRenderProgressBarWidthClamp(t *testing.T) {
	bar := renderProgressBar(2, 5, 10, 0, "")
	if !strings.Contains(bar, "50%") {
		t.Fatalf("clamped bar missing 50%%: %q", bar)
	}
	bar = renderProgressBar(200, 5, 10, 0, "")
	if !strings.Contains(bar, "50%") {
		t.Fatalf("clamped bar missing 50%%: %q", bar)
	}
}

func TestRenderProgressBarZeroTotal(t *testing.T) {
	bar := renderProgressBar(40, 0, 0, 0, "")
	if bar != "" {
		t.Fatalf("zero total should return empty, got: %q", bar)
	}
}

func TestAppDumpProgressETAGuarded(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewApp()
	app.db = conn
	app.screen = ScreenDump
	app.width = 80
	app.height = 24
	app.dumpStatus = DumpStatusRunning

	// First event: no ETA
	app.dumpProgress = &DumpProgressEvent{
		Phase:   "table_start",
		Table:   "users",
		Current: 1,
		Total:   5,
		Elapsed: 1 * time.Second,
	}
	view := app.screens[ScreenDump].View(80, 24)
	if strings.Contains(view, "ETA") {
		t.Fatal("first event should not show ETA")
	}

	// Second event: ETA should appear
	app.dumpProgress = &DumpProgressEvent{
		Phase:   "table_start",
		Table:   "orders",
		Current: 2,
		Total:   5,
		Elapsed: 2 * time.Second,
	}
	view = app.screens[ScreenDump].View(80, 24)
	if !strings.Contains(view, "ETA") {
		t.Fatal("second event should show ETA")
	}

	// Complete: no ETA
	app.dumpProgress = &DumpProgressEvent{
		Phase:   "table_end",
		Table:   "orders",
		Current: 5,
		Total:   5,
		Elapsed: 10 * time.Second,
	}
	view = app.screens[ScreenDump].View(80, 24)
	if strings.Contains(view, "ETA") {
		t.Fatal("complete event should not show ETA")
	}
}

func TestAppDumpProgressBarRenders(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewApp()
	app.db = conn
	app.screen = ScreenDump
	app.width = 80
	app.height = 24
	app.dumpStatus = DumpStatusRunning
	app.dumpProgress = &DumpProgressEvent{
		Phase:   "table_start",
		Table:   "users",
		Current: 3,
		Total:   10,
		Elapsed: 6 * time.Second,
	}

	view := app.screens[ScreenDump].View(80, 24)
	if !strings.Contains(view, "30%") {
		t.Fatalf("view missing 30%%: %q", stripANSIForGolden(view))
	}
	if !strings.Contains(stripANSIForGolden(view), "▓") {
		t.Fatalf("view missing filled bar: %q", stripANSIForGolden(view))
	}
}

func TestAppCloneProgressBarRenders(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewApp()
	app.db = conn
	app.screen = ScreenClone
	app.width = 80
	app.height = 24
	app.cloneStatus = CloneStatusRunning
	app.cloneProgress = &CloneProgressEvent{
		Phase:   "copying_table",
		Step:    "copying users",
		Table:   "users",
		Current: 2,
		Total:   5,
		Elapsed: 2 * time.Second,
	}

	view := app.screens[ScreenClone].View(80, 24)
	if !strings.Contains(view, "40%") {
		t.Fatalf("view missing 40%%: %q", stripANSIForGolden(view))
	}
}

func TestAppProgressDrain100(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	events := make([]dump.ProgressEvent, 100)
	for i := range events {
		events[i] = dump.ProgressEvent{
			Phase:   "table_start",
			Table:   fmt.Sprintf("table_%d", i),
			Current: i + 1,
			Total:   100,
			Elapsed: time.Duration(i+1) * 100 * time.Millisecond,
		}
	}
	runner := mockDumpRunner{events: events}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, ctrlEnter())

	if app.dumpStatus != DumpStatusComplete {
		t.Fatalf("dumpStatus = %v, want complete", app.dumpStatus)
	}
	if app.dumpProgress != nil {
		t.Fatal("expected dumpProgress cleared after completion")
	}
	if len(app.dumpLog) > dumpLogMaxLines {
		t.Fatalf("dumpLog len = %d, exceeds cap %d", len(app.dumpLog), dumpLogMaxLines)
	}
	if len(app.dumpLog) == 0 {
		t.Fatal("expected progress in dumpLog")
	}
	// Verify the last entry is the completion message.
	last := app.dumpLog[len(app.dumpLog)-1]
	if !strings.Contains(last, "dump complete") {
		t.Fatalf("last log entry = %q, want dump complete", last)
	}
}

func TestAppRestoreProgressHandled(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := t.TempDir()
	restoreRunner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, restoreRunner, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, restoreRequestedMsg{inputDir: dumpPath})

	if !containsPlain(app.statusMsg, "Restore complete") {
		t.Fatalf("statusMsg = %q, want restore complete", stripANSIForGolden(app.statusMsg))
	}
	if app.restoreProgress != nil {
		t.Fatal("expected restoreProgress cleared after completion")
	}
}
