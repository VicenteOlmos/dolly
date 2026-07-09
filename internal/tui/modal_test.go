package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestModal_Fits60x20Viewport(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	cases := []struct {
		name  string
		mount func(*App)
	}{
		{
			name: "delete",
			mount: func(app *App) {
				app.saveConnections = true
				app.connStore = newMockConnectionStore(connections.Connection{
					Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u",
				})
				cs := app.screens[ScreenConnection].(*connectionScreen)
				cs.store = app.connStore
				cs.saveConnections = true
				enterConnectionList(cs)
				cs.refreshProfiles()
				app.mountDeleteProfileModal("staging")
			},
		},
		{name: "quit", mount: func(app *App) { app.mountQuitModal() }},
		{
			name: "analyze",
			mount: func(app *App) {
				app.mountAnalyzeResultModal(analyzeResult{
					TableCount:       42,
					DatabaseSize:     128 * 1024 * 1024,
					NextCloneName:    "db_stub_dolly_1",
					TotalRowEstimate: 51200,
					Objects: []ObjectStat{
						{Schema: "public", Name: "users", Kind: "table", RowEstimate: 1200, SizeBytes: 1024 * 1024},
						{Schema: "app", Name: "orders", Kind: "table", RowEstimate: 50000, SizeBytes: 8 * 1024 * 1024},
					},
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp()
			app.screen = ScreenConnection
			app.width = 60
			app.height = 20
			tc.mount(app)

			view := stripANSIForGolden(app.View().Content)
			assertViewFitsViewport(t, view, 20)
			wantConfirm := "Y "
			if tc.name == "analyze" {
				wantConfirm = "Enter"
			}
			if !strings.Contains(view, wantConfirm) {
				t.Fatalf("missing confirm key %q in view:\n%s", wantConfirm, view)
			}
		})
	}
}

func TestModal_StateMachine(t *testing.T) {
	tests := []struct {
		name       string
		keys       []tea.KeyPressMsg
		wantOpen   bool
		wantDelete bool
		profile    string
	}{
		{
			name:     "cancel with n",
			keys:     []tea.KeyPressMsg{keyPress("d", 'd', 0), keyPress("n", 'n', 0)},
			wantOpen: false,
		},
		{
			name:       "confirm with y",
			keys:       []tea.KeyPressMsg{keyPress("d", 'd', 0), keyPress("y", 'y', 0)},
			wantOpen:   false,
			wantDelete: true,
			profile:    "staging",
		},
		{
			name:     "cancel with esc",
			keys:     []tea.KeyPressMsg{keyPress("d", 'd', 0), keyPress("", tea.KeyEscape, 0)},
			wantOpen: false,
		},
		{
			name:     "ctrl tab ignored while open",
			keys:     []tea.KeyPressMsg{keyPress("d", 'd', 0), keyPress("", tea.KeyTab, tea.ModCtrl)},
			wantOpen: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockConnectionStore(connections.Connection{
				Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u", Password: "p",
			})
			app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
			cs := app.screens[ScreenConnection].(*connectionScreen)
			enterConnectionList(cs)
			cs.refreshProfiles()

			for _, k := range tt.keys {
				app = drainUpdate(app, k)
			}

			if app.modalOpen() != tt.wantOpen {
				t.Fatalf("modalOpen = %v, want %v", app.modalOpen(), tt.wantOpen)
			}
			if tt.wantDelete {
				list, err := store.List()
				if err != nil {
					t.Fatal(err)
				}
				if len(list) != 0 {
					t.Fatalf("expected profile deleted, got %+v", list)
				}
			}
			if tt.name == "cancel with n" || tt.name == "cancel with esc" {
				list, _ := store.List()
				if len(list) != 1 {
					t.Fatalf("expected profile kept, got %+v", list)
				}
			}
			if tt.name == "ctrl tab ignored while open" && app.screen != ScreenConnection {
				t.Fatalf("screen = %v, want connection while modal blocks nav", app.screen)
			}
		})
	}
}
