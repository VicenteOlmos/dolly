package tui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/dumphistory"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

type mockDumpRunner struct {
	err      error
	events   []dump.ProgressEvent
	blockCtx bool
}

type schemasRecordingDumpRunner struct {
	lastSchemas []string
}

func (r *schemasRecordingDumpRunner) Run(_ context.Context, _ *sql.DB, _ string, _ DumpDraft, schemas []string, _, _ string, _ func(dump.ProgressEvent)) error {
	r.lastSchemas = append([]string(nil), schemas...)
	return nil
}

func (m mockDumpRunner) Run(ctx context.Context, _ *sql.DB, _ string, _ DumpDraft, _ []string, _, _ string, onProgress func(dump.ProgressEvent)) error {
	for _, ev := range m.events {
		if onProgress != nil {
			onProgress(ev)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if m.blockCtx {
		<-ctx.Done()
		return ctx.Err()
	}
	return m.err
}

const sampleMetadataJSON = `{
  "generated_at": "2026-01-01T00:00:00Z",
  "schema": "public",
  "tables": [
    {"schema": "public", "name": "users", "row_count": 100, "columns": []}
  ]
}`

type artifactDumpRunner struct {
	events []dump.ProgressEvent
	err    error
}

func (a artifactDumpRunner) Run(_ context.Context, _ *sql.DB, outputDir string, _ DumpDraft, _ []string, _, _ string, onProgress func(dump.ProgressEvent)) error {
	for _, ev := range a.events {
		if onProgress != nil {
			onProgress(ev)
		}
	}
	dir := outputDir
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(sampleMetadataJSON), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), []byte("{}\n"), 0o644); err != nil {
		return err
	}
	return a.err
}

type recordingFolderOpener struct {
	paths []string
}

func (r *recordingFolderOpener) Open(path string) error {
	r.paths = append(r.paths, path)
	return nil
}

type recordingRestoreRunner struct {
	inputDir string
	schemas  []string
	dsn      string
}

func (r *recordingRestoreRunner) Run(_ context.Context, _ *sql.DB, inputDir string, schemas []string, dsn string, _ func(restore.ProgressEvent)) error {
	r.dsn = dsn
	r.inputDir = inputDir
	r.schemas = append([]string(nil), schemas...)
	return nil
}

type mockRestoreRunner struct {
	err      error
	blockCtx bool
}

func (m mockRestoreRunner) Run(ctx context.Context, _ *sql.DB, _ string, _ []string, _ string, _ func(restore.ProgressEvent)) error {
	if m.blockCtx {
		<-ctx.Done()
		return ctx.Err()
	}
	return m.err
}

func seedDumpSchemas(app *App, schemas ...string) {
	if len(schemas) == 0 {
		schemas = []string{"public"}
	}
	SeedSchemaPicker(&app.dump.SchemaPicker, schemas, schemas)
}

func TestAppDumpNoSchema(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()

	app = drainUpdate(app, dumpRequestedMsg{})

	if app.dumpStatus != DumpStatusIdle {
		t.Fatalf("dumpStatus = %v, want idle", app.dumpStatus)
	}
	if !containsPlain(app.statusMsg, "Select schemas") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppDumpSuccess(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	outDir := t.TempDir()
	runner := artifactDumpRunner{
		events: []dump.ProgressEvent{
			{Phase: "table_start", Table: "users"},
			{Phase: "table_end", Table: "users"},
		},
	}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = outDir
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, ctrlEnter())

	if app.dumpStatus != DumpStatusComplete {
		t.Fatalf("dumpStatus = %v, want complete", app.dumpStatus)
	}
	if app.dumpResult == nil {
		t.Fatal("expected dump result summary")
	}
	if app.dumpResult.Outcome != DumpOutcomeSuccess {
		t.Fatalf("Outcome = %v, want success", app.dumpResult.Outcome)
	}
	if len(app.dumpLog) < 3 {
		t.Fatalf("dumpLog = %v, want progress + complete lines", app.dumpLog)
	}
	if !containsPlain(app.statusMsg, "Dump complete") {
		t.Fatalf("statusMsg = %q, want dump complete", stripANSIForGolden(app.statusMsg))
	}
	for _, name := range []string{"metadata.json", "users.ndjson"} {
		if _, err := os.Stat(filepath.Join(outDir, "1", name)); err != nil {
			t.Fatalf("expected artifact %s: %v", name, err)
		}
	}
}

func TestAppDumpPassesPickerSchemas(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := &schemasRecordingDumpRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	SeedSchemaPicker(&app.dump.SchemaPicker, []string{"app", "billing"}, []string{"app", "billing"})
	app.width = 80
	app.height = 24

	app = drainUpdate(app, ctrlEnter())

	if len(runner.lastSchemas) != 2 || runner.lastSchemas[0] != "app" || runner.lastSchemas[1] != "billing" {
		t.Fatalf("dump runner schemas = %v, want [app billing]", runner.lastSchemas)
	}
}

func TestAppDumpError(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	outDir := t.TempDir()
	runner := artifactDumpRunner{err: errors.New("disk full at postgres://u:secret@host/db?password=querysecret")}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = outDir
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, ctrlEnter())

	if app.dumpStatus != DumpStatusComplete {
		t.Fatalf("dumpStatus = %v, want complete", app.dumpStatus)
	}
	if app.dumpResult == nil {
		t.Fatal("expected dump result summary on error")
	}
	if app.dumpResult.Outcome != DumpOutcomeError {
		t.Fatalf("Outcome = %v, want error", app.dumpResult.Outcome)
	}
	if strings.Contains(app.dumpError, "secret") || strings.Contains(app.dumpResult.Error, "secret") {
		t.Fatalf("dump result leaked password: dumpError=%q result=%q", app.dumpError, app.dumpResult.Error)
	}
	if !strings.Contains(app.dumpError, "disk full") {
		t.Fatalf("dumpError = %q", app.dumpError)
	}
	view := stripANSIForGolden(app.screens[ScreenDump].View(80, 24))
	if strings.Contains(view, "secret") {
		t.Fatalf("dump result view leaked password:\n%s", view)
	}
	if !containsPlain(app.statusMsg, "Dump failed") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppDumpCancel(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := mockDumpRunner{blockCtx: true}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	next, reqCmd := app.Update(ctrlEnter())
	app = next.(*App)
	if reqCmd == nil {
		t.Fatal("expected wait cmd from dump request")
	}
	if app.dumpStatus != DumpStatusRunning {
		t.Fatal("expected running after start")
	}

	app = drainUpdate(app, keyPress("c", 'c', 0))
	app = drainUpdate(app, keyPress("y", 'y', 0))
	app = drainUpdate(app, reqCmd())

	if app.dumpStatus != DumpStatusIdle {
		t.Fatalf("dumpStatus = %v, want idle after cancel", app.dumpStatus)
	}
	if app.dumpResult != nil {
		t.Fatal("expected no dump result after cancel")
	}
	if !containsPlain(app.statusMsg, "cancelled") {
		t.Fatalf("statusMsg = %q, want cancelled", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppDumpNoSession(t *testing.T) {
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, nil, false)
	app.screen = ScreenDump
	app.dump.OutputDir = "/tmp/out"
	app.width = 80
	app.height = 24

	app = drainUpdate(app, dumpRequestedMsg{})

	if app.dumpStatus != DumpStatusIdle {
		t.Fatalf("dumpStatus = %v, want idle", app.dumpStatus)
	}
	if !containsPlain(app.statusMsg, "Connect first") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
	view := app.screens[ScreenDump].View(80, 20)
	if !containsPlain(view, "Connect first (screen 1)") {
		t.Fatalf("view = %q", stripANSIForGolden(view))
	}
}

func TestAppDumpEmptyPath(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.width = 80
	app.height = 24

	app = drainUpdate(app, dumpRequestedMsg{})

	if !containsPlain(app.statusMsg, "Set output directory") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppQuitDuringDump(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := mockDumpRunner{blockCtx: true}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	next, reqCmd := app.Update(ctrlEnter())
	app = next.(*App)
	if reqCmd == nil {
		t.Fatal("expected wait cmd from dump request")
	}
	if app.dumpStatus != DumpStatusRunning {
		t.Fatal("expected running dump with wait cmd")
	}

	app = drainUpdate(app, keyPress("", 'c', tea.ModCtrl))
	if !app.modalOpen() {
		t.Fatal("expected quit confirmation modal")
	}
	next, quitCmd := app.Update(keyPress("y", 'y', 0))
	if quitCmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", quitCmd())
	}

	app = drainUpdate(next.(*App), reqCmd())
	if app.dumpStatus != DumpStatusIdle {
		t.Fatalf("dumpStatus = %v, want idle after cancel", app.dumpStatus)
	}
	if !containsPlain(app.statusMsg, "cancelled") {
		t.Fatalf("statusMsg = %q, want cancelled", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppQuitDuringRestoreCancelsRestore(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, mockRestoreRunner{blockCtx: true}, nil, nil, nil, false)
	app.db = conn
	app.conn = ConnectionDraft{Host: "h-x", Port: "5432", Database: "db_stub", User: "u", Password: "p"}
	app.screen = ScreenDump
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	next, reqCmd := app.Update(restoreRequestedMsg{inputDir: t.TempDir()})
	app = next.(*App)
	if reqCmd == nil || !app.restoreRunning {
		t.Fatal("expected running restore with wait cmd")
	}

	app = drainUpdate(app, keyPress("", 'c', tea.ModCtrl))
	if !app.modalOpen() {
		t.Fatal("expected quit confirmation modal")
	}
	next, quitCmd := app.Update(keyPress("y", 'y', 0))
	if quitCmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", quitCmd())
	}

	app = drainUpdate(next.(*App), reqCmd())
	if app.restoreRunning {
		t.Fatal("expected restore stopped after quit cancel")
	}
	if !containsPlain(app.statusMsg, "Restore cancelled") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestDumpScreenLogScroll(t *testing.T) {
	log := make([]string, 20)
	for i := range log {
		log[i] = fmt.Sprintf("line-%02d", i)
	}
	status := DumpStatusIdle
	var dumpErr string
	var dumpResult *DumpResultSummary
	ds := newDumpScreen(&DumpDraft{}, func() bool { return true }, &status, &log, &dumpErr, &dumpResult, nil, nil, nil, nil).(*dumpScreen)
	enterDumpSection(ds, dumpSectionLog)

	ds.Update(keyPress("", tea.KeyUp, 0))
	ds.Update(keyPress("", tea.KeyUp, 0))
	if ds.logTailOffset != 2 {
		t.Fatalf("logTailOffset = %d, want 2 after scrolling up", ds.logTailOffset)
	}

	ds.Update(keyPress("", tea.KeyDown, 0))
	if ds.logTailOffset != 1 {
		t.Fatalf("logTailOffset = %d, want 1 after scrolling down", ds.logTailOffset)
	}

	status = DumpStatusRunning
	before := ds.logTailOffset
	ds.Update(keyPress("", tea.KeyUp, 0))
	if ds.logTailOffset != before {
		t.Fatalf("logTailOffset while running = %d, want unchanged %d", ds.logTailOffset, before)
	}
	status = DumpStatusIdle

	ds.resetLogScroll()
	if ds.logTailOffset != 0 {
		t.Fatalf("logTailOffset after reset = %d, want 0", ds.logTailOffset)
	}
	enterDumpSection(ds, dumpSectionLog)
	ds.Update(keyPress("", tea.KeyUp, 0))
	if ds.logTailOffset != 1 {
		t.Fatalf("logTailOffset after scroll from reset = %d, want 1", ds.logTailOffset)
	}
}

func TestDumpScreenPathEdit(t *testing.T) {
	draft := DumpDraft{}
	status := DumpStatusIdle
	var log []string
	var dumpErr string
	var dumpResult *DumpResultSummary
	screen := newDumpScreen(&draft, func() bool { return true }, &status, &log, &dumpErr, &dumpResult, nil, nil, nil, nil)
	ds := screen.(*dumpScreen)
	enterDumpSection(ds, dumpSectionPath)

	screen.Update(keyPress("/", '/', 0))
	screen.Update(keyPress("v", 'v', 0))
	screen.Update(keyPress("a", 'a', 0))
	screen.Update(keyPress("r", 'r', 0))
	screen.Update(keyPress("/", '/', 0))
	screen.Update(keyPress("d", 'd', 0))
	screen.Update(keyPress("m", 'm', 0))
	screen.Update(keyPress("p", 'p', 0))
	if draft.OutputDir != "/var/dmp" {
		t.Fatalf("OutputDir = %q, want /var/dmp", draft.OutputDir)
	}

	screen.Update(keyPress("", tea.KeyBackspace, 0))
	if draft.OutputDir != "/var/dm" {
		t.Fatalf("OutputDir after backspace = %q, want /var/dm", draft.OutputDir)
	}

	screen.Update(keyPress("t", 't', 0))
	if !draft.NoTransaction {
		t.Fatal("expected transaction off after first t")
	}
	screen.Update(keyPress("t", 't', 0))
	if draft.NoTransaction {
		t.Fatal("expected transaction on after second t")
	}
}

func TestAppDumpIgnoresDuplicateEnter(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{blockCtx: true}, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.dumpStatus = DumpStatusRunning
	app.width = 80
	app.height = 24

	next, cmd := app.Update(dumpRequestedMsg{})
	if cmd != nil {
		t.Fatal("expected no cmd while already running")
	}
	if next.(*App).dumpStatus != DumpStatusRunning {
		t.Fatal("expected still running")
	}
}

func TestAppDumpResultEscDismiss(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, artifactDumpRunner{}, nil, nil, nil, nil, false)
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

	app = drainUpdate(app, keyPress("", tea.KeyEscape, 0))
	if app.dumpStatus != DumpStatusIdle {
		t.Fatalf("dumpStatus = %v, want idle after Esc", app.dumpStatus)
	}
	if app.dumpResult != nil {
		t.Fatal("expected result cleared after Esc")
	}
}

func TestAppDumpResultEnterRerun(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, artifactDumpRunner{}, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, ctrlEnter())
	if app.dumpStatus != DumpStatusComplete {
		t.Fatalf("expected complete after first dump, got %v", app.dumpStatus)
	}

	next, cmd := app.Update(keyPress("", tea.KeyEnter, 0))
	if cmd == nil {
		t.Fatal("expected rerun cmd from Enter on result")
	}
	app = drainUpdate(next.(*App), cmd())
	if app.dumpStatus != DumpStatusComplete {
		t.Fatalf("dumpStatus = %v, want complete after rerun", app.dumpStatus)
	}
	if app.dumpResult == nil {
		t.Fatal("expected new result summary after rerun")
	}
}

func TestAppDumpResultOpenFolder(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	outDir := t.TempDir()
	opener := &recordingFolderOpener{}
	app := NewAppWithOptions(mockSchemaLoader{}, artifactDumpRunner{}, nil, nil, opener, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = outDir
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, ctrlEnter())
	app = drainUpdate(app, keyPress("o", 'o', 0))

	if len(opener.paths) != 1 || opener.paths[0] != filepath.Join(outDir, "1") {
		t.Fatalf("opener paths = %v, want [%s]", opener.paths, filepath.Join(outDir, "1"))
	}
	if app.dumpStatus != DumpStatusComplete {
		t.Fatalf("dumpStatus = %v, want still complete after open", app.dumpStatus)
	}
}

func TestAppDumpResultIgnoresTabAndToggle(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, artifactDumpRunner{}, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.dump.NoTransaction = false
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, ctrlEnter())
	if app.dumpStatus != DumpStatusComplete {
		t.Fatal("expected complete")
	}

	app = drainUpdate(app, keyPress("", tea.KeyTab, 0))

	before := app.dump.NoTransaction
	app = drainUpdate(app, keyPress("t", 't', 0))
	if app.dump.NoTransaction != before {
		t.Fatal("t should be ignored in result mode")
	}
}

func TestAppDumpResultFileScroll(t *testing.T) {
	app := NewApp()
	app.screen = ScreenDump
	app.width = 60
	app.height = 20
	app.dumpStatus = DumpStatusComplete

	files := make([]string, 0, 25)
	for i := 0; i < 24; i++ {
		files = append(files, fmt.Sprintf("table_%02d.ndjson", i))
	}
	files = append(files, "metadata.json")
	sort.Strings(files)

	app.dumpResult = &DumpResultSummary{
		Outcome:          DumpOutcomeSuccess,
		OutputDir:        t.TempDir(),
		Files:            files,
		TableCount:       24,
		TotalRowEstimate: int64Ptr(5000),
	}

	ds := app.screens[ScreenDump].(*dumpScreen)
	if ds.fileListOffset != 0 {
		t.Fatalf("initial fileListOffset = %d, want 0", ds.fileListOffset)
	}

	app = drainUpdate(app, keyPress("k", 'k', 0))
	ds = app.screens[ScreenDump].(*dumpScreen)
	if ds.fileListOffset == 0 {
		t.Fatal("k should scroll file list (increase offset)")
	}

	for range 30 {
		app = drainUpdate(app, keyPress("k", 'k', 0))
	}
	ds = app.screens[ScreenDump].(*dumpScreen)
	if ds.fileListOffset != len(files) {
		t.Fatalf("fileListOffset = %d, want clamped at %d", ds.fileListOffset, len(files))
	}

	app = drainUpdate(app, keyPress("j", 'j', 0))
	ds = app.screens[ScreenDump].(*dumpScreen)
	if ds.fileListOffset >= len(files) {
		t.Fatal("j should scroll file list toward start (decrease offset)")
	}

	for range 30 {
		app = drainUpdate(app, keyPress("j", 'j', 0))
	}
	ds = app.screens[ScreenDump].(*dumpScreen)
	if ds.fileListOffset != 0 {
		t.Fatalf("fileListOffset = %d, want 0 after scrolling to start", ds.fileListOffset)
	}

	app = drainUpdate(app, keyPress("", tea.KeyUp, 0))
	ds = app.screens[ScreenDump].(*dumpScreen)
	if ds.fileListOffset == 0 {
		t.Fatal("↑ should scroll file list like k")
	}
}

func TestAppDumpRegistersHistory(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	outDir := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := dumphistory.NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	runner := artifactDumpRunner{
		events: []dump.ProgressEvent{
			{Phase: "table_start", Table: "users"},
			{Phase: "table_end", Table: "users"},
		},
	}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.cfg = config.DefaultConfig()
	app.dumpHistoryStore = store
	app.conn.Database = "db_stub"
	app.screen = ScreenDump
	app.dump.OutputDir = outDir
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, ctrlEnter())

	if app.dumpStatus != DumpStatusComplete {
		t.Fatalf("dumpStatus = %v, want complete", app.dumpStatus)
	}
	recs, err := store.ListBase(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("store records = %d, want 1", len(recs))
	}
	if recs[0].Seq != 1 {
		t.Fatalf("seq = %d, want 1", recs[0].Seq)
	}
	if recs[0].SchemaLabel != "public" {
		t.Fatalf("schema_label = %q, want public", recs[0].SchemaLabel)
	}
	if len(app.dump.History.Entries) != 1 {
		t.Fatalf("history entries = %d, want 1", len(app.dump.History.Entries))
	}
	if !containsPlain(app.dump.History.Entries[0].Label, "#1 · public · 1 tables") {
		t.Fatalf("history label = %q", app.dump.History.Entries[0].Label)
	}
}

func TestAppDumpRestoreFromHistoryEnter(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := filepath.Join(t.TempDir(), "1")
	if err := os.MkdirAll(dumpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreRunner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, restoreRunner, nil, nil, nil, false)
	app.db = conn
	app.conn = ConnectionDraft{Host: "h-x", Port: "5432", Database: "db_stub", User: "u", Password: "p", SSLMODE: "disable"}
	app.cfg = config.DefaultConfig()
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)
	app.dump.History = DumpHistoryState{
		Entries: []DumpHistoryEntry{{
			Seq:     1,
			Path:    dumpPath,
			Label:   "#1 · public · 1 tables",
			Schemas: []string{"public"},
		}},
		Cursor: 0,
	}

	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionHistory)

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if restoreRunner.inputDir != dumpPath {
		t.Fatalf("restore inputDir = %q, want %q", restoreRunner.inputDir, dumpPath)
	}
	if len(restoreRunner.schemas) != 1 || restoreRunner.schemas[0] != "public" {
		t.Fatalf("restore schemas = %v, want [public]", restoreRunner.schemas)
	}
	if !strings.Contains(restoreRunner.dsn, "db_stub") {
		t.Fatalf("restore dsn = %q, want db_stub", restoreRunner.dsn)
	}
	if !containsPlain(app.statusMsg, "Restore complete") {
		t.Fatalf("statusMsg = %q, want restore complete", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppRestoreCancel(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, mockRestoreRunner{blockCtx: true}, nil, nil, nil, false)
	app.db = conn
	app.conn = ConnectionDraft{Host: "h-x", Port: "5432", Database: "db_stub", User: "u", Password: "p"}
	app.screen = ScreenDump
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	next, reqCmd := app.Update(restoreRequestedMsg{inputDir: t.TempDir()})
	app = next.(*App)
	if !app.restoreRunning {
		t.Fatal("expected restore running")
	}
	app = drainUpdate(app, keyPress("c", 'c', 0))
	if !app.modalOpen() {
		t.Fatal("expected cancel modal")
	}
	app = drainUpdate(app, keyPress("y", 'y', 0))
	app = drainUpdate(app, reqCmd())
	if app.restoreRunning {
		t.Fatal("expected restore stopped")
	}
	if !containsPlain(app.statusMsg, "Restore cancelled") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppDumpRestoreSkipPolicyImmediate(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := filepath.Join(t.TempDir(), "1")
	if err := os.MkdirAll(dumpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreRunner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, restoreRunner, nil, nil, nil, false)
	app.db = conn
	app.cfg = config.DefaultConfig()
	app.cfg.Clone.Replace = false
	app.cfg.Clone.RestoreOnConflict = "skip"
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)
	app.dump.History = DumpHistoryState{
		Entries: []DumpHistoryEntry{{
			Seq:     1,
			Path:    dumpPath,
			Label:   "#1 · public · 1 tables",
			Schemas: []string{"public"},
		}},
		Cursor: 0,
	}

	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionHistory)

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.modalOpen() {
		t.Fatal("expected no confirm modal for skip policy")
	}
	if restoreRunner.inputDir != dumpPath {
		t.Fatalf("restore inputDir = %q, want %q", restoreRunner.inputDir, dumpPath)
	}
	if !containsPlain(app.statusMsg, "Restore complete") {
		t.Fatalf("statusMsg = %q, want restore complete", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppDumpRestoreDestructiveRequiresConfirm(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := filepath.Join(t.TempDir(), "1")
	if err := os.MkdirAll(dumpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreRunner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, restoreRunner, nil, nil, nil, false)
	app.db = conn
	app.conn.Host = "h-x"
	app.conn.Database = "db_stub"
	app.conn.User = "u"
	app.conn.Password = "p"
	app.cfg = config.DefaultConfig()
	app.cfg.Clone.Replace = true
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)
	app.dump.History = DumpHistoryState{
		Entries: []DumpHistoryEntry{{
			Seq:     1,
			Path:    dumpPath,
			Label:   "#1 · public · 1 tables",
			Schemas: []string{"public"},
		}},
		Cursor: 0,
	}

	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionHistory)

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if !app.modalOpen() {
		t.Fatal("expected restore confirm modal")
	}
	if !strings.Contains(app.modal.body, "Target:") {
		t.Fatalf("modal body missing Target: line:\n%s", app.modal.body)
	}
	if strings.Contains(app.modal.body, ":p@") {
		t.Fatalf("modal body leaked password:\n%s", app.modal.body)
	}
	if restoreRunner.inputDir != "" {
		t.Fatalf("restore inputDir = %q, want empty before confirm", restoreRunner.inputDir)
	}

	app = drainUpdate(app, keyPress("y", 'y', 0))

	if restoreRunner.inputDir != dumpPath {
		t.Fatalf("restore inputDir = %q, want %q", restoreRunner.inputDir, dumpPath)
	}
	if !containsPlain(app.statusMsg, "Restore complete") {
		t.Fatalf("statusMsg = %q, want restore complete", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppDumpRestoreDestructiveCancel(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := filepath.Join(t.TempDir(), "1")
	if err := os.MkdirAll(dumpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreRunner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, restoreRunner, nil, nil, nil, false)
	app.db = conn
	app.cfg = config.DefaultConfig()
	app.cfg.Clone.Replace = true
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)
	app.dump.History = DumpHistoryState{
		Entries: []DumpHistoryEntry{{
			Seq:     1,
			Path:    dumpPath,
			Label:   "#1 · public · 1 tables",
			Schemas: []string{"public"},
		}},
		Cursor: 0,
	}

	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionHistory)

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if !app.modalOpen() {
		t.Fatal("expected restore confirm modal")
	}

	app = drainUpdate(app, keyPress("n", 'n', 0))

	if app.modalOpen() {
		t.Fatal("expected modal dismissed after N")
	}
	if restoreRunner.inputDir != "" {
		t.Fatalf("restore inputDir = %q, want empty after cancel", restoreRunner.inputDir)
	}
}

func TestAppDumpRestoreUpsertRequiresConfirm(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := filepath.Join(t.TempDir(), "1")
	if err := os.MkdirAll(dumpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreRunner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, restoreRunner, nil, nil, nil, false)
	app.db = conn
	app.cfg = config.DefaultConfig()
	app.cfg.Clone.Replace = false
	app.cfg.Clone.RestoreOnConflict = "upsert"
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)
	app.dump.History = DumpHistoryState{
		Entries: []DumpHistoryEntry{{
			Seq:     1,
			Path:    dumpPath,
			Label:   "#1 · public · 1 tables",
			Schemas: []string{"public"},
		}},
		Cursor: 0,
	}

	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionHistory)

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if !app.modalOpen() {
		t.Fatal("expected restore confirm modal for upsert policy")
	}
	if restoreRunner.inputDir != "" {
		t.Fatalf("restore inputDir = %q, want empty before confirm", restoreRunner.inputDir)
	}

	app = drainUpdate(app, keyPress("y", 'y', 0))

	if restoreRunner.inputDir != dumpPath {
		t.Fatalf("restore inputDir = %q, want %q", restoreRunner.inputDir, dumpPath)
	}
}

func TestAppDumpRestoreFromHistoryMsg(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := filepath.Join(t.TempDir(), "2")
	if err := os.MkdirAll(dumpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreRunner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, restoreRunner, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	app = drainUpdate(app, restoreRequestedMsg{inputDir: dumpPath})

	if restoreRunner.inputDir != dumpPath {
		t.Fatalf("restore inputDir = %q, want %q", restoreRunner.inputDir, dumpPath)
	}
	if !containsPlain(app.statusMsg, "Restore complete") {
		t.Fatalf("statusMsg = %q, want restore complete", stripANSIForGolden(app.statusMsg))
	}
}
