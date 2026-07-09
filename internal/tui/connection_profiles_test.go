package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/db"
)

type failingUpsertStore struct {
	connections.ConnectionStore
}

func (f *failingUpsertStore) UpsertBySignature(c connections.Connection) (connections.Connection, error) {
	return connections.Connection{}, fmt.Errorf("disk full")
}

func TestConnectionAutoSaveFailureStillConnects(t *testing.T) {
	base := newMockConnectionStore()
	store := &failingUpsertStore{ConnectionStore: base}
	loader := mockSchemaLoader{tables: []db.Table{{Name: "users", Schema: "public"}}}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	app.conn = ConnectionDraft{
		Host: "db.example.com", Port: "5432", Database: "app", User: "u", Password: "p",
	}
	enterConnectionFields(app.screens[ScreenConnection].(*connectionScreen))

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.connStatus != ConnStatusConnected {
		t.Fatalf("connStatus = %v, want connected despite save failure", app.connStatus)
	}
	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema after successful connect", app.screen)
	}
	if !containsPlain(app.statusMsg, "Save profile") {
		t.Fatalf("statusMsg = %q, want save warning", app.statusMsg)
	}
	if app.activeProfile != nil {
		t.Fatal("activeProfile should be nil when upsert fails")
	}
}

func TestConnectionAutoSaveOnConnect(t *testing.T) {
	store := newMockConnectionStore()
	app := NewAppWithOptions(mockSchemaLoader{tables: []db.Table{{Name: "users", Schema: "public"}}}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	app.conn = ConnectionDraft{
		Host: "db.example.com", Port: "5432", Database: "app", User: "u", Password: "p",
	}

	enterConnectionFields(app.screens[ScreenConnection].(*connectionScreen))
	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.activeProfile == nil {
		t.Fatal("expected active profile after connect")
	}
	if app.activeProfile.Name != "conn-1" {
		t.Fatalf("profile name = %q, want conn-1", app.activeProfile.Name)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("store list = %v, err = %v", list, err)
	}
}

func TestConnectionTestDoesNotAutoSave(t *testing.T) {
	store := newMockConnectionStore()
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("", 't', tea.ModCtrl))

	list, _ := store.List()
	if len(list) != 0 {
		t.Fatalf("expected no auto-save after ping, got %d profiles", len(list))
	}
}

func TestConnectionSaveAsDuplicateRejected(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Database: "d", User: "u",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	cs := app.screens[ScreenConnection].(*connectionScreen)
	cs.panel = connPanelSaveAs
	cs.nameInput = "staging"

	next, _ := app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	cs = app.screens[ScreenConnection].(*connectionScreen)
	if cs.listErr == "" || !containsPlain(cs.listErr, "already exists") {
		t.Fatalf("listErr = %q, want duplicate name error", cs.listErr)
	}
}

func TestConnectionPickPopulatesDraft(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "prod", Host: "prod.db", Port: "5432", Database: "app", User: "app", Password: "secret",
		Schemas: []string{"app"},
	})
	loader := mockSchemaLoader{tables: []db.Table{{Name: "t", Schema: "app"}}}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.listCursor = 0

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.conn.Host != "prod.db" {
		t.Fatalf("Host = %q, want prod.db", app.conn.Host)
	}
	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema", app.screen)
	}
	names := app.clone.SchemaPicker.SelectedNames()
	if len(names) != 1 || names[0] != "app" {
		t.Fatalf("clone picker = %v, want [app]", names)
	}
}

func TestConnectionDeleteRename(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "old", Host: "h", Database: "d", User: "u",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.refreshProfiles()

	cs.panel = connPanelRename
	cs.nameInput = "new"
	_ = cs.Update(keyPress("", tea.KeyEnter, 0))
	cs.refreshProfiles()
	if len(cs.profiles) != 1 || cs.profiles[0].Name != "new" {
		t.Fatalf("profiles after rename = %+v", cs.profiles)
	}

	app = drainUpdate(app, keyPress("d", 'd', 0))
	app = drainUpdate(app, keyPress("y", 'y', 0))
	cs = app.screens[ScreenConnection].(*connectionScreen)
	cs.refreshProfiles()
	if len(cs.profiles) != 0 {
		t.Fatalf("expected empty list after delete, got %+v", cs.profiles)
	}
}

func TestConnection_DeleteRequiresConfirmation(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Database: "d", User: "u",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.refreshProfiles()

	app = drainUpdate(app, keyPress("d", 'd', 0))
	if !app.modalOpen() {
		t.Fatal("expected delete confirmation modal")
	}
	list, _ := store.List()
	if len(list) != 1 {
		t.Fatalf("delete should not run before confirm, got %+v", list)
	}

	app = drainUpdate(app, keyPress("n", 'n', 0))
	if app.modalOpen() {
		t.Fatal("expected modal dismissed")
	}
	list, _ = store.List()
	if len(list) != 1 {
		t.Fatalf("profile should remain after cancel, got %+v", list)
	}

	app = drainUpdate(app, keyPress("d", 'd', 0))
	app = drainUpdate(app, keyPress("y", 'y', 0))
	list, _ = store.List()
	if len(list) != 0 {
		t.Fatalf("expected delete after confirm, got %+v", list)
	}
}

func TestConnectionEditEscCancel(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "old.host", Port: "5432", Database: "app", User: "u", Password: "p",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.listCursor = 0

	_ = cs.Update(keyPress("e", 'e', 0))
	cs.draft.Host = "changed.host"
	_ = cs.Update(keyPress("", tea.KeyEscape, 0))

	if cs.panel != connPanelList {
		t.Fatalf("panel = %v, want list after Esc", cs.panel)
	}
	got, err := store.Get("staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "old.host" {
		t.Fatalf("Host = %q, want unchanged old.host", got.Host)
	}
}

func TestConnectionListCursorPreviewsDraft(t *testing.T) {
	store := newMockConnectionStore(
		connections.Connection{Name: "a", Host: "host-a", Port: "5432", Database: "db", User: "u", Password: "p"},
		connections.Connection{Name: "b", Host: "host-b", Port: "5432", Database: "db", User: "u", Password: "p"},
	)
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.refreshProfiles()

	if cs.draft.Host != "host-a" {
		t.Fatalf("initial Host = %q, want host-a", cs.draft.Host)
	}
	_ = cs.Update(keyPress("", tea.KeyDown, 0))
	if cs.draft.Host != "host-b" {
		t.Fatalf("after down Host = %q, want host-b", cs.draft.Host)
	}
	if cs.previewProfileName != "b" {
		t.Fatalf("previewProfileName = %q, want b", cs.previewProfileName)
	}
}

func TestConnectionDuplicateSignatureConnectFromFieldsBlocked(t *testing.T) {
	sig := connections.Connection{Host: "h", Port: "5432", Database: "db", User: "u"}.Signature()
	store := newMockConnectionStore(
		connections.Connection{Name: "alpha", Host: "h", Port: "5432", Database: "db", User: "u", Password: "p"},
		connections.Connection{Name: "beta", Host: "h", Port: "5432", Database: "db", User: "u", Password: "p"},
	)
	_ = sig
	app := NewAppWithOptions(mockSchemaLoader{tables: []db.Table{{Name: "t", Schema: "public"}}}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.conn = ConnectionDraft{Host: "h", Port: "5432", Database: "db", User: "u", Password: "p"}
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionFields(cs)
	cs.clearProfilePreview()

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want stay on connection", app.screen)
	}
	if cs.listErr == "" || !containsPlain(cs.listErr, "multiple saved profiles") {
		t.Fatalf("listErr = %q, want ambiguity error", cs.listErr)
	}
}

func TestConnectionDuplicateSignatureConnectUsesPreview(t *testing.T) {
	store := newMockConnectionStore(
		connections.Connection{Name: "alpha", Host: "h", Port: "5432", Database: "db", User: "u", Password: "p", Schemas: []string{"app"}},
		connections.Connection{Name: "beta", Host: "h", Port: "5432", Database: "db", User: "u", Password: "p", Schemas: []string{"billing"}},
	)
	loader := mockSchemaLoader{
		schemaNames: []string{"app", "billing"},
		tables:      []db.Table{{Name: "t", Schema: "billing"}},
	}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.refreshProfiles()
	cs.listCursor = 1
	cs.previewListProfile()

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema", app.screen)
	}
	names := app.dump.SchemaPicker.SelectedNames()
	if len(names) != 1 || names[0] != "billing" {
		t.Fatalf("schemas = %v, want [billing] from beta profile", names)
	}
}

func TestConnectionSaveAsDuplicateSignatureNote(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Port: "5432", Database: "db", User: "u", Password: "p",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.conn = ConnectionDraft{Host: "h", Port: "5432", Database: "db", User: "u", Password: "p"}
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)

	_ = cs.Update(keyPress("s", 's', 0))
	cs.nameInput = "alias"
	_ = cs.Update(keyPress("", tea.KeyEnter, 0))

	if cs.listErr == "" || !containsPlain(cs.listErr, "same credentials as staging") {
		t.Fatalf("listErr = %q, want alias note", cs.listErr)
	}
}

func TestConnectionSaveAsFromListUsesHighlightedProfile(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "u", Password: "p",
		Schemas: []string{"app", "billing"},
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.conn = ConnectionDraft{Host: "stale.host", Port: "5432", Database: "wrong", User: "x", Password: "y"}
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.refreshProfiles()
	cs.listCursor = 0

	_ = cs.Update(keyPress("s", 's', 0))
	cs.nameInput = "copy"
	_ = cs.Update(keyPress("", tea.KeyEnter, 0))

	got, err := store.Get("copy")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Host != "db.example.com" {
		t.Fatalf("Host = %q, want db.example.com from highlighted profile", got.Host)
	}
	if len(got.Schemas) != 2 || got.Schemas[0] != "app" {
		t.Fatalf("Schemas = %v, want [app billing]", got.Schemas)
	}
}

func TestConnectionSaveAsViaLetter(t *testing.T) {
	store := newMockConnectionStore()
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.conn = ConnectionDraft{Host: "h", Port: "5432", Database: "d", User: "u", Password: "p"}
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)

	_ = cs.Update(keyPress("s", 's', 0))
	if cs.panel != connPanelSaveAs {
		t.Fatalf("panel = %v, want save-as", cs.panel)
	}
	cs.nameInput = "via-s"
	_ = cs.Update(keyPress("", tea.KeyEnter, 0))

	if _, err := store.Get("via-s"); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestConnectionRenameViaLetter(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "old", Host: "h", Port: "5432", Database: "d", User: "u", Password: "p",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.refreshProfiles()

	_ = cs.Update(keyPress("r", 'r', 0))
	if cs.panel != connPanelRename {
		t.Fatalf("panel = %v, want rename", cs.panel)
	}
	cs.nameInput = "new"
	_ = cs.Update(keyPress("", tea.KeyEnter, 0))
	cs.refreshProfiles()
	if len(cs.profiles) != 1 || cs.profiles[0].Name != "new" {
		t.Fatalf("profiles = %+v", cs.profiles)
	}
}

func TestConnectionEditPutNoConnect(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "old.host", Port: "5432", Database: "app", User: "u", Password: "p",
		Schemas: []string{"app"},
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.listCursor = 0

	_ = cs.Update(keyPress("e", 'e', 0))
	if cs.panel != connPanelEdit {
		t.Fatalf("panel = %v, want edit", cs.panel)
	}
	cs.draft.Host = "new.host"
	_ = cs.Update(keyPress("", tea.KeyEnter, 0))

	if cs.panel != connPanelList {
		t.Fatalf("panel = %v, want list after save", cs.panel)
	}
	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want stay on connection", app.screen)
	}
	got, err := store.Get("staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "new.host" {
		t.Fatalf("Host = %q, want new.host", got.Host)
	}
}

func TestConnectionTypingInHostDoesNotTriggerTest(t *testing.T) {
	store := newMockConnectionStore()
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionFields(cs)

	_ = cs.Update(keyPress("j", 'j', 0))
	_ = cs.Update(keyPress("t", 't', 0))

	if cs.draft.Host != "jt" {
		t.Fatalf("Host = %q, want jt", cs.draft.Host)
	}
	list, _ := store.List()
	if len(list) != 0 {
		t.Fatalf("expected no store activity, got %+v", list)
	}
}

func TestConnectionListLetterPingNoStoreWrite(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "p", Host: "h", Port: "5432", Database: "d", User: "u", Password: "x",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)

	app = drainUpdate(app, keyPress("t", 't', 0))

	list, _ := store.List()
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1 unchanged profile", len(list))
	}
}

func TestConnectionManualEnterUsesStoredSchemas(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "u", Password: "p",
		Schemas: []string{"app"},
	})
	loader := mockSchemaLoader{tables: []db.Table{{Name: "users", Schema: "app"}}}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	app.conn = ConnectionDraft{
		Host: "db.example.com", Port: "5432", Database: "app", User: "u", Password: "p",
	}
	enterConnectionFields(app.screens[ScreenConnection].(*connectionScreen))

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema", app.screen)
	}
	names := app.dump.SchemaPicker.SelectedNames()
	if len(names) != 1 || names[0] != "app" {
		t.Fatalf("dump picker = %v, want [app]", names)
	}
}

func TestConnectionAutoSaveReusesExistingName(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "u", Password: "p",
	})
	loader := mockSchemaLoader{tables: []db.Table{{Name: "users", Schema: "public"}}}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	app.conn = ConnectionDraft{
		Host: "db.example.com", Port: "5432", Database: "app", User: "u", Password: "p",
	}
	enterConnectionFields(app.screens[ScreenConnection].(*connectionScreen))

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.activeProfile == nil || app.activeProfile.Name != "staging" {
		t.Fatalf("activeProfile = %+v, want name staging", app.activeProfile)
	}
}

func TestConnectionSaveAsSuccess(t *testing.T) {
	store := newMockConnectionStore()
	loader := mockSchemaLoader{tables: []db.Table{{Name: "users", Schema: "public"}}}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	app.conn = ConnectionDraft{Host: "h", Port: "5432", Database: "d", User: "u", Password: "p"}
	enterConnectionFields(app.screens[ScreenConnection].(*connectionScreen))
	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	app.screen = ScreenConnection
	cs := app.screens[ScreenConnection].(*connectionScreen)
	cs.panel = connPanelSaveAs
	cs.nameInput = "manual-save"
	_ = cs.Update(keyPress("", tea.KeyEnter, 0))

	got, err := store.Get("manual-save")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Host != "h" {
		t.Fatalf("saved profile = %+v", got)
	}
}

func TestConnectionEmptySchemasSeedsCatalog(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "fresh", Host: "h", Port: "5432", Database: "d", User: "u", Password: "p",
	})
	loader := mockSchemaLoader{schemaNames: []string{"app", "billing"}}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	cs.listCursor = 0

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if len(app.clone.SchemaPicker.AvailableSchemas) != 2 {
		t.Fatalf("catalog = %v", app.clone.SchemaPicker.AvailableSchemas)
	}
	if len(app.clone.SchemaPicker.SelectedNames()) != 0 {
		t.Fatal("expected no pre-selected schemas for empty profile")
	}
}

func TestSaveConnectionsFalseHidesSavedList(t *testing.T) {
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, nil, false)
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	view := stripANSIForGolden(app.View().Content)
	if containsPlain(view, "Saved profiles") {
		t.Fatal("save_connections false should not render saved profile section")
	}
}

func TestClonePickerPersistOnStart(t *testing.T) {
	store := newMockConnectionStore()
	loader := mockSchemaLoader{
		schemaNames: []string{"app", "billing"},
		tables:      []db.Table{{Name: "users", Schema: "app"}},
	}
	runner := &schemasRecordingCloneRunner{}
	app := NewAppWithOptions(loader, mockDumpRunner{}, nil, runner, nil, store, true)
	app.screen = ScreenConnection
	app.conn = ConnectionDraft{Host: "h", Database: "d", User: "u", Password: "p"}
	enterConnectionFields(app.screens[ScreenConnection].(*connectionScreen))

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	app.screen = ScreenClone
	app.clone.TargetDSN = "postgres://u:p@h-y/target"
	app.clone.SchemaPicker.HandleKey(tea.Key{Code: tea.KeySpace})
	app.clone.SchemaPicker.MoveCursor(1)
	app.clone.SchemaPicker.HandleKey(tea.Key{Code: tea.KeySpace})

	app = drainUpdate(app, ctrlEnter())

	if app.activeProfile == nil || len(app.activeProfile.Schemas) != 2 {
		t.Fatalf("profile schemas = %v", app.activeProfile)
	}
	if len(runner.lastSchemas) != 2 {
		t.Fatalf("clone schemas = %v", runner.lastSchemas)
	}
}
