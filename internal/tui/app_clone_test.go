package tui

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

type mockCloneRunner struct {
	err      error
	lines    []string
	blockCtx bool
}

type schemasRecordingCloneRunner struct {
	lastSchemas []string
	lastDraft   CloneDraft
}

func (r *schemasRecordingCloneRunner) Run(_ context.Context, draft CloneDraft, schemas []string, _ func(CloneProgressEvent)) error {
	r.lastSchemas = append([]string(nil), schemas...)
	r.lastDraft = draft
	return nil
}

func (m mockCloneRunner) Run(ctx context.Context, _ CloneDraft, _ []string, onProgress func(CloneProgressEvent)) error {
	for _, line := range m.lines {
		if onProgress != nil {
			onProgress(CloneProgressEvent{Phase: "test", Step: line, Current: 1, Total: 1})
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

func cloneApp(t *testing.T, runner CloneRunner) *App {
	t.Helper()
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, runner, nil, nil, false)
	app.screen = ScreenClone
	app.width = 80
	app.height = 24
	return app
}

func cloneAppWithSession(t *testing.T, runner CloneRunner) *App {
	t.Helper()
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := cloneApp(t, runner)
	app.db = conn
	app.conn.Host = "h-x"
	app.conn.Database = "db_stub"
	app.conn.User = "u"
	app.conn.Password = "p"
	SeedSchemaPicker(&app.clone.SchemaPicker, []string{"public", "app"}, []string{"public", "app"})
	if app.cfg == nil {
		app.cfg = config.DefaultConfig()
	}
	if app.clone.CloneName == "" {
		app.clone.CloneName = cloneName(app.conn.Database, app.cfg.Clone.NameTemplate)
	}
	app.clone.TargetDSN = "postgres://u:p@h-y/target"
	app.clone.TargetSource = TargetSourceManual
	return app
}

func enterCloneForm(app *App) *cloneScreen {
	cs := app.screens[ScreenClone].(*cloneScreen)
	cs.nav.EnterInside(cloneSectionForm)
	return cs
}

func TestAppCloneSuccess(t *testing.T) {
	runner := mockCloneRunner{lines: []string{"strategy: template", "schemas: public, app"}}
	app := cloneAppWithSession(t, runner)

	app = drainUpdate(app, ctrlEnter())

	if app.cloneStatus != CloneStatusComplete {
		t.Fatalf("cloneStatus = %v, want complete", app.cloneStatus)
	}
	if app.cloneError != "" {
		t.Fatalf("cloneError = %q, want empty", app.cloneError)
	}
	if len(app.cloneLog) < 3 {
		t.Fatalf("cloneLog = %v, want progress + complete lines", app.cloneLog)
	}
	if !containsPlain(app.statusMsg, "Clone complete") {
		t.Fatalf("statusMsg = %q, want clone complete", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppClonePassesPickerSchemas(t *testing.T) {
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)

	app = drainUpdate(app, ctrlEnter())

	if len(runner.lastSchemas) != 2 || runner.lastSchemas[0] != "app" || runner.lastSchemas[1] != "public" {
		t.Fatalf("clone runner schemas = %v, want [app public]", runner.lastSchemas)
	}
	if runner.lastDraft.TargetDSN != app.clone.TargetDSN {
		t.Fatalf("draft TargetDSN = %q, want %q", runner.lastDraft.TargetDSN, app.clone.TargetDSN)
	}
}

func TestAppCloneError(t *testing.T) {
	runner := mockCloneRunner{err: errors.New("permission denied")}
	app := cloneAppWithSession(t, runner)

	app = drainUpdate(app, ctrlEnter())

	if app.cloneStatus != CloneStatusComplete {
		t.Fatalf("cloneStatus = %v, want complete", app.cloneStatus)
	}
	if app.cloneError != "permission denied" {
		t.Fatalf("cloneError = %q", app.cloneError)
	}
	if !containsPlain(app.statusMsg, "Clone failed") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppCloneErrorRedactsEmbeddedDSN(t *testing.T) {
	runner := mockCloneRunner{err: errors.New("failed postgres://u:secret@host/db?password=querysecret")}
	app := cloneAppWithSession(t, runner)

	app = drainUpdate(app, ctrlEnter())
	view := stripANSIForGolden(app.screens[ScreenClone].View(80, 24))

	for _, got := range []string{app.cloneError, stripANSIForGolden(app.statusMsg), strings.Join(app.cloneLog, "\n"), view} {
		if strings.Contains(got, "secret") || strings.Contains(got, ":secret@") {
			t.Fatalf("clone error leaked password:\n%s", got)
		}
	}
}

func TestAppCloneNoSession(t *testing.T) {
	app := cloneApp(t, mockCloneRunner{})

	app = drainUpdate(app, cloneRequestedMsg{})

	if app.cloneStatus != CloneStatusIdle {
		t.Fatalf("cloneStatus = %v, want idle", app.cloneStatus)
	}
	if !containsPlain(app.statusMsg, "Connect first") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppCloneNoSchema(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	app := cloneApp(t, mockCloneRunner{})
	app.db = conn
	app.clone.TargetDSN = "postgres://u:p@h-y/target"

	app = drainUpdate(app, cloneRequestedMsg{})

	if app.cloneStatus != CloneStatusIdle {
		t.Fatalf("cloneStatus = %v, want idle", app.cloneStatus)
	}
	if !containsPlain(app.statusMsg, "Select schemas") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppCloneNoTarget(t *testing.T) {
	app := cloneAppWithSession(t, mockCloneRunner{})
	app.clone.TargetDSN = ""
	app.clone.TargetSource = TargetSourceManual

	app = drainUpdate(app, cloneRequestedMsg{})

	if app.cloneStatus != CloneStatusIdle {
		t.Fatalf("cloneStatus = %v, want idle", app.cloneStatus)
	}
	if !containsPlain(app.statusMsg, "Set target DSN") {
		t.Fatalf("statusMsg = %q", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppCloneIgnoresDuplicateEnter(t *testing.T) {
	app := cloneAppWithSession(t, mockCloneRunner{blockCtx: true})
	app.cloneStatus = CloneStatusRunning

	next, cmd := app.Update(cloneRequestedMsg{})
	if cmd != nil {
		t.Fatal("expected no cmd while already running")
	}
	if next.(*App).cloneStatus != CloneStatusRunning {
		t.Fatal("expected still running")
	}
}

func TestCloneAnalyzeGatesClone(t *testing.T) {
	orig := analyzeSourceFunc
	analyzeSourceFunc = func(_ context.Context, _ *sql.DB, _, _ string, _ []string) (analyzeResult, error) {
		return analyzeResult{
			TableCount:    12,
			DatabaseSize:  5 * 1024 * 1024,
			NextCloneName: "db_stub_dolly_1",
			ComputedAt:    time.Now(),
		}, nil
	}
	t.Cleanup(func() { analyzeSourceFunc = orig })

	app := cloneAppWithSession(t, mockCloneRunner{})
	app.clone.AnalyzeEnabled = true

	app = drainUpdate(app, ctrlEnter())
	if app.cloneStatus != CloneStatusIdle {
		t.Fatalf("cloneStatus = %v, want idle while analyze gates clone", app.cloneStatus)
	}
	if app.clone.AnalyzeState.Result == nil {
		t.Fatal("expected analyze result before clone starts")
	}
	if !app.modalOpen() {
		t.Fatal("expected analyze result modal open")
	}

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	if app.cloneStatus != CloneStatusComplete {
		t.Fatalf("cloneStatus = %v, want complete after Enter on analyze modal", app.cloneStatus)
	}
	if app.modalOpen() {
		t.Fatal("expected modal dismissed after Enter")
	}
}

func TestAppCloneReplaceRequiresConfirm(t *testing.T) {
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.cfg.Clone.Replace = true

	app = drainUpdate(app, ctrlEnter())

	if !app.modalOpen() {
		t.Fatal("expected clone confirm modal")
	}
	if app.modal.kind != modalCloneConfirm {
		t.Fatalf("modal kind = %v, want modalCloneConfirm", app.modal.kind)
	}
	if app.cloneStatus != CloneStatusIdle {
		t.Fatalf("cloneStatus = %v, want idle before confirm", app.cloneStatus)
	}
	if runner.lastSchemas != nil {
		t.Fatalf("clone runner called before confirm: schemas = %v", runner.lastSchemas)
	}
}

func TestAppCloneReplaceConfirmRedactsTargetDSN(t *testing.T) {
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.cfg.Clone.Replace = true

	app = drainUpdate(app, ctrlEnter())

	if !app.modalOpen() || app.modal.kind != modalCloneConfirm {
		t.Fatal("expected clone confirm modal")
	}
	if strings.Contains(app.modal.body, ":p@") {
		t.Fatalf("confirm modal leaked password in body:\n%s", app.modal.body)
	}
	if !strings.Contains(app.modal.body, "***") && !strings.Contains(app.modal.body, "%2A%2A%2A") {
		t.Fatalf("confirm modal missing redacted password in body:\n%s", app.modal.body)
	}
}

func TestAppCloneReplaceConfirmRedactsLibpqTargetDSN(t *testing.T) {
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.cfg.Clone.Replace = true
	app.clone.TargetDSN = "host=db.example.com user=app password=secret dbname=app"

	app = drainUpdate(app, ctrlEnter())

	if !app.modalOpen() || app.modal.kind != modalCloneConfirm {
		t.Fatal("expected clone confirm modal")
	}
	if strings.Contains(app.modal.body, "secret") || strings.Contains(app.modal.body, "password=secret") {
		t.Fatalf("confirm modal leaked libpq password in body:\n%s", app.modal.body)
	}
	if !strings.Contains(app.modal.body, "password=***") {
		t.Fatalf("confirm modal missing redacted libpq password in body:\n%s", app.modal.body)
	}
}

func TestAppCloneReplaceConfirmStartsClone(t *testing.T) {
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.cfg.Clone.Replace = true

	app = drainUpdate(app, ctrlEnter())
	if !app.modalOpen() {
		t.Fatal("expected clone confirm modal")
	}

	app = drainUpdate(app, keyPress("y", 'y', 0))

	if app.cloneStatus != CloneStatusComplete {
		t.Fatalf("cloneStatus = %v, want complete after confirm", app.cloneStatus)
	}
	if len(runner.lastSchemas) != 2 {
		t.Fatalf("clone runner schemas = %v, want 2 schemas", runner.lastSchemas)
	}
}

func TestAppCloneNoReplaceSkipsConfirm(t *testing.T) {
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.cfg.Clone.Replace = false

	app = drainUpdate(app, ctrlEnter())

	if app.modalOpen() {
		t.Fatal("expected no confirm modal when replace is false")
	}
	if app.cloneStatus != CloneStatusComplete {
		t.Fatalf("cloneStatus = %v, want complete", app.cloneStatus)
	}
	if len(runner.lastSchemas) != 2 {
		t.Fatalf("clone runner schemas = %v, want 2 schemas", runner.lastSchemas)
	}
}

func TestCloneAnalyzeReplaceRequiresConfirmBeforeClone(t *testing.T) {
	orig := analyzeSourceFunc
	analyzeSourceFunc = func(_ context.Context, _ *sql.DB, _, _ string, _ []string) (analyzeResult, error) {
		return analyzeResult{
			TableCount:    12,
			DatabaseSize:  5 * 1024 * 1024,
			NextCloneName: "db_stub_dolly_1",
			ComputedAt:    time.Now(),
		}, nil
	}
	t.Cleanup(func() { analyzeSourceFunc = orig })

	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.cfg.Clone.Replace = true
	app.clone.AnalyzeEnabled = true

	app = drainUpdate(app, ctrlEnter())
	if !app.modalOpen() || app.modal.kind != modalAnalyzeResult {
		t.Fatal("expected analyze result modal first")
	}
	if runner.lastSchemas != nil {
		t.Fatal("clone runner called during analyze")
	}

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	if !app.modalOpen() || app.modal.kind != modalCloneConfirm {
		t.Fatal("expected clone confirm modal after analyze")
	}
	if app.cloneStatus != CloneStatusIdle {
		t.Fatalf("cloneStatus = %v, want idle before replace confirm", app.cloneStatus)
	}
	if runner.lastSchemas != nil {
		t.Fatal("clone runner called before replace confirm")
	}

	app = drainUpdate(app, keyPress("y", 'y', 0))
	if app.cloneStatus != CloneStatusComplete {
		t.Fatalf("cloneStatus = %v, want complete after confirm", app.cloneStatus)
	}
	if len(runner.lastSchemas) != 2 {
		t.Fatalf("clone runner schemas = %v, want 2 schemas", runner.lastSchemas)
	}
}

func TestCloneCurrentDSNDisplayMasked(t *testing.T) {
	app := cloneAppWithSession(t, mockCloneRunner{})
	app.clone.TargetSource = TargetSourceCurrent
	app.clone.TargetDSN = app.conn.DSN()
	cs := enterCloneForm(app)
	cs.formField = 1

	view := stripANSIForGolden(cs.View(80, 24))
	if strings.Contains(view, ":p@") {
		t.Fatalf("view leaked password:\n%s", view)
	}
	if !strings.Contains(view, "%2A%2A%2A") && !strings.Contains(view, "***") {
		t.Fatalf("view missing masked password:\n%s", view)
	}
}

func TestCloneSavedProfileDSNMasked(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
	})
	app := cloneAppWithSession(t, mockCloneRunner{})
	app.connStore = store
	cs := app.screens[ScreenClone].(*cloneScreen)
	cs.store = store
	app.clone.TargetSource = TargetSourceSaved
	app.clone.TargetProfileName = "staging"
	cs.resolveTargetDSN()
	enterCloneForm(app)
	cs.formField = 1

	view := stripANSIForGolden(cs.View(80, 24))
	if strings.Contains(view, ":x@") {
		t.Fatalf("view leaked saved profile password:\n%s", view)
	}
	if !strings.Contains(view, "%2A%2A%2A") && !strings.Contains(view, "***") {
		t.Fatalf("view missing masked password:\n%s", view)
	}
}

func TestAppCloneUnsanitizedWarningStrategies(t *testing.T) {
	strategies := []string{"template", "logical-stream", "physical-backup"}
	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			t.Parallel()
			runner := &schemasRecordingCloneRunner{}
			app := cloneAppWithSession(t, runner)
			app.cfg.Sanitization.Enabled = true
			app.clone.Strategy = strategy

			app = drainUpdate(app, ctrlEnter())

			log := strings.Join(app.cloneLog, "\n")
			if !strings.Contains(log, "warning: clone will copy unsanitized data") {
				t.Fatalf("cloneLog missing unsanitized warning:\n%s", log)
			}
			if !strings.Contains(log, "strategy="+strategy) {
				t.Fatalf("cloneLog missing strategy=%s:\n%s", strategy, log)
			}
			if strings.Contains(log, "secret") || strings.Contains(log, ":p@") {
				t.Fatalf("cloneLog leaked credentials:\n%s", log)
			}
		})
	}
}

func TestAppCloneSchemaReplayNoUnsanitizedWarning(t *testing.T) {
	t.Parallel()
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.cfg.Sanitization.Enabled = true
	app.clone.Strategy = "schema-replay"

	app = drainUpdate(app, ctrlEnter())

	log := strings.Join(app.cloneLog, "\n")
	if strings.Contains(log, "warning: clone will copy unsanitized data") {
		t.Fatalf("schema-replay with sanitization should not warn:\n%s", log)
	}
}

func TestAppCloneStrategyCycleRefreshesTargetBeforeStart(t *testing.T) {
	t.Parallel()
	runner := &schemasRecordingCloneRunner{}
	app := cloneAppWithSession(t, runner)
	app.clone.TargetSource = TargetSourceCurrent
	app.clone.TargetDSN = "postgres://stale@old-host/wrong"
	cs := enterCloneForm(app)
	cs.formField = 2
	cs.cycleStrategy(1) // schema-replay -> template

	app = drainUpdate(app, ctrlEnter())

	want := app.conn.DSN()
	if runner.lastDraft.TargetDSN != want {
		t.Fatalf("runner TargetDSN = %q, want refreshed %q", runner.lastDraft.TargetDSN, want)
	}
}
