//go:build !windows

package tui

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/dbanalyze"
)

var update = flag.Bool("update", false, "update golden files")

func TestGoldenViews(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	screens := []struct {
		name   string
		screen Screen
		setup  func(*App)
	}{
		{name: "connection", screen: ScreenConnection},
		{
			name:   "connection_connecting",
			screen: ScreenConnection,
			setup: func(app *App) {
				app.connStatus = ConnStatusConnecting
				app.conn.Host = "db.example.com"
				app.spinnerFrame = 0
			},
		},
		{
			name:   "connection_help",
			screen: ScreenConnection,
			setup: func(app *App) {
				app.showKeysHelp = true
			},
		},
		{
			name:   "connection_help_saved",
			screen: ScreenConnection,
			setup: func(app *App) {
				app.showKeysHelp = true
				app.saveConnections = true
				app.connStore = newMockConnectionStore(connections.Connection{
					Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
				})
				cs := app.screens[ScreenConnection].(*connectionScreen)
				cs.store = app.connStore
				cs.saveConnections = true
				cs.refreshProfiles()
			},
		},
		{
			name:   "delete_modal",
			screen: ScreenConnection,
			setup: func(app *App) {
				app.saveConnections = true
				app.connStore = newMockConnectionStore(connections.Connection{
					Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
				})
				cs := app.screens[ScreenConnection].(*connectionScreen)
				cs.store = app.connStore
				cs.saveConnections = true
				enterConnectionList(cs)
				cs.refreshProfiles()
				app.mountDeleteProfileModal("staging")
			},
		},
		{
			name:   "quit_modal",
			screen: ScreenConnection,
			setup: func(app *App) {
				app.mountQuitModal()
			},
		},
		{
			name:   "schema",
			screen: ScreenSchema,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.schema = SchemaDraft{
					Tables:      []string{"orders", "products", "users"},
					TableCount:  3,
					ColumnCount: 8,
					FKCount:     1,
				}
				app.connStatus = ConnStatusConnected
			},
		},
		{name: "dump_idle_no_session", screen: ScreenDump},
		{
			name:   "dump_help",
			screen: ScreenDump,
			setup: func(app *App) {
				app.showKeysHelp = true
			},
		},
		{
			name:   "dump_idle_session",
			screen: ScreenDump,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.dump.OutputDir = "/tmp/dolly-dump"
				SeedSchemaPicker(&app.dump.SchemaPicker, []string{"public", "app"}, nil)
			},
		},
		{
			name:   "dump_running",
			screen: ScreenDump,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.dump.OutputDir = "/tmp/dolly-dump"
				SeedSchemaPicker(&app.dump.SchemaPicker, []string{"public"}, []string{"public"})
				app.dumpStatus = DumpStatusRunning
				app.dumpLog = []string{"dumping users…", "done users"}
				app.statusMsg = "Dumping…"
				app.spinnerFrame = 0
			},
		},
		{
			name:   "dump_error",
			screen: ScreenDump,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.dump.OutputDir = "/tmp/dolly-dump"
				SeedSchemaPicker(&app.dump.SchemaPicker, []string{"public"}, []string{"public"})
				app.dumpStatus = DumpStatusError
				app.dumpError = "permission denied"
				app.statusMsg = "Dump failed: permission denied"
			},
		},
		{name: "clone_idle_no_session", screen: ScreenClone},
		{
			name:   "clone_help",
			screen: ScreenClone,
			setup: func(app *App) {
				app.showKeysHelp = true
			},
		},
		{
			name:   "clone_idle_session",
			screen: ScreenClone,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				SeedSchemaPicker(&app.clone.SchemaPicker, []string{"app", "public"}, []string{"app", "public"})
				app.clone.CloneName = "staging-clone"
				app.clone.TargetDSN = "postgres://u:p@h-y/target"
				app.clone.Strategy = "template"
			},
		},
		{
			name:   "clone_idle_no_schemas",
			screen: ScreenClone,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.clone.TargetDSN = "postgres://u:p@h-y/target"
				SeedSchemaPicker(&app.clone.SchemaPicker, []string{"public", "app"}, nil)
			},
		},
		{
			name:   "clone_running",
			screen: ScreenClone,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				SeedSchemaPicker(&app.clone.SchemaPicker, []string{"public"}, []string{"public"})
				app.clone.TargetDSN = "postgres://u:p@h-y/target"
				app.cloneStatus = CloneStatusRunning
				app.cloneLog = []string{"strategy: template", "schemas: public"}
				app.statusMsg = "Cloning…"
				app.spinnerFrame = 0
			},
		},
		{
			name:   "clone_complete_success",
			screen: ScreenClone,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				SeedSchemaPicker(&app.clone.SchemaPicker, []string{"public"}, []string{"public"})
				app.clone.TargetDSN = "postgres://u:p@h-y/target"
				app.cloneStatus = CloneStatusComplete
				app.statusMsg = "Clone complete"
			},
		},
		{
			name:   "clone_complete_error",
			screen: ScreenClone,
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				SeedSchemaPicker(&app.clone.SchemaPicker, []string{"public"}, []string{"public"})
				app.clone.TargetDSN = "postgres://u:p@h-y/target"
				app.cloneStatus = CloneStatusComplete
				app.cloneError = "permission denied"
				app.statusMsg = "Clone failed: permission denied"
			},
		},
		{name: "config", screen: ScreenConfig},
	}

	for _, tt := range screens {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp()
			app.screen = tt.screen
			app.width = 80
			app.height = 24
			if tt.setup != nil {
				tt.setup(app)
			}
			got := stripANSIForGolden(app.View().Content)

			path := filepath.Join("testdata", tt.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", tt.name)
			}
		})
	}
}

func TestGoldenConfigSizes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "config_80x24", width: 80, height: 24},
		{name: "config_60x20", width: 60, height: 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp()
			app.screen = ScreenConfig
			app.width = tc.width
			app.height = tc.height

			got := stripANSIForGolden(app.View().Content)
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", tc.name)
			}
		})
	}
}

func TestGoldenConfigEdit(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	cases := []struct {
		name  string
		setup func(*App)
	}{
		{
			name: "config_edit_string",
			setup: func(app *App) {
				cfg := config.DefaultConfig()
				app.cfg = cfg
				cs := app.screens[ScreenConfig].(*configScreen)
				// cursor=0 is env.path (string field); enter edit mode
				cs.editing = true
				cs.editValue = ".env"
				cs.editCursor = 2
			},
		},
		{
			name: "config_edit_bool",
			setup: func(app *App) {
				cfg := config.DefaultConfig()
				app.cfg = cfg
				cs := app.screens[ScreenConfig].(*configScreen)
				// cursor=11 is clone.replace (bool); toggle it to true so View shows new value
				cs.cursor = 11
				cfg.Clone.Replace = true
			},
		},
		{
			name: "config_invalid_int",
			setup: func(app *App) {
				cfg := config.DefaultConfig()
				app.cfg = cfg
				cs := app.screens[ScreenConfig].(*configScreen)
				// cursor=18 is subset.percent (int field)
				cs.cursor = 18
				cs.editing = true
				cs.editValue = "notanint"
				cs.editCursor = 8
				cs.editErr = `invalid integer "notanint"`
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp()
			app.screen = ScreenConfig
			app.width = 80
			app.height = 24
			if tc.setup != nil {
				tc.setup(app)
			}
			got := stripANSIForGolden(app.View().Content)
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", tc.name)
			}
		})
	}
}

func TestGoldenConnectionListSizes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	profiles := []connections.Connection{
		{Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "deploy", Password: "x"},
		{Name: "prod", Host: "prod.internal", Port: "5432", Database: "app", User: "app", Password: "y"},
	}

	cases := []struct {
		name   string
		width  int
		height int
		store  *mockConnectionStore
	}{
		{name: "connection_list_empty_80x24", width: 80, height: 24, store: newMockConnectionStore()},
		{name: "connection_list_empty_60x20", width: 60, height: 20, store: newMockConnectionStore()},
		{name: "connection_list_populated_80x24", width: 80, height: 24, store: newMockConnectionStore(profiles...)},
		{name: "connection_list_populated_60x20", width: 60, height: 20, store: newMockConnectionStore(profiles...)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, tc.store, true)
			app.screen = ScreenConnection
			app.width = tc.width
			app.height = tc.height
			cs := app.screens[ScreenConnection].(*connectionScreen)
			enterConnectionList(cs)
			cs.refreshProfiles()

			got := stripANSIForGolden(app.View().Content)
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", tc.name)
			}
		})
	}
}

func TestGoldenConnectionEditSizes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	prof := connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "deploy", Password: "secret",
		Schemas: []string{"app", "billing"},
	}

	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "connection_edit_80x24", width: 80, height: 24},
		{name: "connection_edit_60x20", width: 60, height: 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockConnectionStore(prof)
			app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
			app.screen = ScreenConnection
			app.width = tc.width
			app.height = tc.height
			cs := app.screens[ScreenConnection].(*connectionScreen)
			enterConnectionList(cs)
			cs.listCursor = 0
			cs.refreshProfiles()
			_ = cs.Update(keyPress("e", 'e', 0))

			got := stripANSIForGolden(app.View().Content)
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", tc.name)
			}
		})
	}
}

func TestGoldenModalSizes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	profiles := []connections.Connection{
		{Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app"},
	}

	cases := []struct {
		name   string
		width  int
		height int
		setup  func(*App)
	}{
		{
			name: "delete_modal_60x20", width: 60, height: 20,
			setup: func(app *App) {
				app.saveConnections = true
				app.connStore = newMockConnectionStore(profiles...)
				cs := app.screens[ScreenConnection].(*connectionScreen)
				cs.store = app.connStore
				cs.saveConnections = true
				enterConnectionList(cs)
				cs.refreshProfiles()
				app.mountDeleteProfileModal("staging")
			},
		},
		{
			name: "quit_modal_60x20", width: 60, height: 20,
			setup: func(app *App) {
				app.mountQuitModal()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, newMockConnectionStore(profiles...), true)
			app.screen = ScreenConnection
			app.width = tc.width
			app.height = tc.height
			if tc.setup != nil {
				tc.setup(app)
			}
			got := stripANSIForGolden(app.View().Content)
			if !strings.Contains(got, "Y ") || !strings.Contains(got, "Esc") {
				t.Fatalf("modal copy missing confirm/cancel keys: %s", got)
			}
			assertViewFitsViewport(t, got, tc.height)

			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", tc.name)
			}
		})
	}
}

func TestGoldenCapabilitiesStripSizes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	sizes := []struct {
		name   string
		width  int
		height int
		screen Screen
	}{
		{name: "strip_80x24", width: 80, height: 24, screen: ScreenConnection},
		{name: "strip_60x20", width: 60, height: 20, screen: ScreenConnection},
		{name: "strip_clone_80x24", width: 80, height: 24, screen: ScreenClone},
	}

	for _, sz := range sizes {
		t.Run(sz.name, func(t *testing.T) {
			app := NewApp()
			app.screen = sz.screen
			app.width = sz.width
			app.height = sz.height
			got := stripANSIForGolden(app.View().Content)
			if !strings.Contains(got, "dump") || !strings.Contains(got, "restore") {
				t.Fatalf("view missing capabilities strip: %s", got)
			}

			path := filepath.Join("testdata", sz.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", sz.name)
			}
		})
	}
}

func TestGoldenCloneFeatureViews(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	setups := map[string]func(*testing.T, *App){
		"clone_picker_saved": func(t *testing.T, app *App) {
			conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			store := newMockConnectionStore(connections.Connection{
				Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
			})
			app.db = conn
			app.connStore = store
			app.saveConnections = true
			app.cfg = config.DefaultConfig()
			app.conn.Host = "h-x"
			app.conn.Database = "db_stub"
			app.conn.User = "u"
			app.conn.Password = "p"
			app.clone.CloneName = "db_stub_dolly_1"
			app.clone.TargetSource = TargetSourceSaved
			app.clone.TargetProfileName = "staging"
			app.clone.TargetDSN = profileDSN(connections.Connection{
				Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
			})
			app.clone.Strategy = "schema-replay"
			SeedSchemaPicker(&app.clone.SchemaPicker, []string{"app", "public"}, []string{"app", "public"})
			cs := app.screens[ScreenClone].(*cloneScreen)
			cs.store = store
			cs.saveConnections = true
			cs.nav.EnterInside(cloneSectionForm)
			cs.formField = 1
		},
		"clone_strategy_cycled": func(t *testing.T, app *App) {
			conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			app.db = conn
			app.cfg = config.DefaultConfig()
			app.conn.Host = "h-x"
			app.conn.Database = "db_stub"
			app.conn.User = "u"
			app.conn.Password = "p"
			app.clone.CloneName = "db_stub_dolly_1"
			app.clone.TargetSource = TargetSourceCurrent
			app.clone.TargetDSN = app.conn.DSN()
			app.clone.Strategy = "template"
			SeedSchemaPicker(&app.clone.SchemaPicker, []string{"app", "public"}, []string{"app", "public"})
			cs := app.screens[ScreenClone].(*cloneScreen)
			cs.nav.EnterInside(cloneSectionForm)
			cs.formField = 2
		},
		"clone_analyze_loading": func(t *testing.T, app *App) {
			conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			app.db = conn
			app.cfg = config.DefaultConfig()
			app.conn.Host = "h-x"
			app.conn.Database = "db_stub"
			app.conn.User = "u"
			app.conn.Password = "p"
			app.clone.CloneName = "db_stub_dolly_1"
			app.clone.TargetSource = TargetSourceCurrent
			app.clone.TargetDSN = app.conn.DSN()
			app.clone.Strategy = "schema-replay"
			app.clone.AnalyzeEnabled = true
			app.clone.AnalyzeState.Loading = true
			app.spinnerFrame = 0
			SeedSchemaPicker(&app.clone.SchemaPicker, []string{"app", "public"}, []string{"app", "public"})
			cs := app.screens[ScreenClone].(*cloneScreen)
			cs.nav.EnterInside(cloneSectionForm)
		},
		"clone_analyze_result": func(t *testing.T, app *App) {
			conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			app.db = conn
			app.cfg = config.DefaultConfig()
			app.conn.Host = "h-x"
			app.conn.Database = "db_stub"
			app.conn.User = "u"
			app.conn.Password = "p"
			app.clone.CloneName = "db_stub_dolly_1"
			app.clone.TargetSource = TargetSourceCurrent
			app.clone.TargetDSN = app.conn.DSN()
			app.clone.Strategy = "schema-replay"
			app.clone.AnalyzeEnabled = true
			app.clone.AnalyzeState.Result = &analyzeResult{
				TableCount:       42,
				DatabaseSize:     128 * 1024 * 1024,
				NextCloneName:    "db_stub_dolly_1",
				TotalRowEstimate: 51200,
				Objects: []dbanalyze.ObjectStat{
					{Schema: "public", Name: "users", Kind: "table", RowEstimate: 1200, SizeBytes: 1024 * 1024},
					{Schema: "app", Name: "orders", Kind: "table", RowEstimate: 50000, SizeBytes: 8 * 1024 * 1024},
				},
			}
			app.mountAnalyzeResultModal(*app.clone.AnalyzeState.Result)
			SeedSchemaPicker(&app.clone.SchemaPicker, []string{"app", "public"}, []string{"app", "public"})
			cs := app.screens[ScreenClone].(*cloneScreen)
			cs.nav.EnterInside(cloneSectionForm)
		},
	}

	sizes := []struct {
		suffix string
		width  int
		height int
	}{
		{suffix: "80x24", width: 80, height: 24},
		{suffix: "60x20", width: 60, height: 20},
	}

	names := make([]string, 0, len(setups))
	for name := range setups {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, base := range names {
		for _, sz := range sizes {
			name := base + "_" + sz.suffix
			t.Run(name, func(t *testing.T) {
				app := NewApp()
				app.screen = ScreenClone
				app.width = sz.width
				app.height = sz.height
				setups[base](t, app)
				got := stripANSIForGolden(app.View().Content)

				path := filepath.Join("testdata", name+".golden")
				if *update {
					if err := os.MkdirAll("testdata", 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden: %v (run with -update)", err)
				}
				if got != string(want) {
					t.Fatalf("golden mismatch for %s", name)
				}
			})
		}
	}
}

func TestGoldenDumpResultViews(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	screens := []struct {
		name  string
		setup func(*App)
	}{
		{
			name: "dump_result_success",
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.dump.OutputDir = "/tmp/dolly-out"
				app.dumpStatus = DumpStatusComplete
				app.dumpResult = &DumpResultSummary{
					Outcome:          DumpOutcomeSuccess,
					OutputDir:        "/tmp/dolly-out",
					Files:            []string{"metadata.json", "users.ndjson"},
					TableCount:       1,
					TotalRowEstimate: int64Ptr(100),
				}
			},
		},
		{
			name: "dump_result_error",
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.dump.OutputDir = "/tmp/dolly-out"
				app.dumpStatus = DumpStatusComplete
				app.dumpResult = &DumpResultSummary{
					Outcome:   DumpOutcomeError,
					OutputDir: "/tmp/dolly-out",
					Error:     "disk full",
					Files:     []string{"metadata.json", "partial.ndjson"},
				}
			},
		},
		{
			name: "dump_result_many_files",
			setup: func(app *App) {
				conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				app.db = conn
				app.dump.OutputDir = "/tmp/dolly-out"
				app.dumpStatus = DumpStatusComplete
				files := make([]string, 0, 21)
				for i := 0; i < 19; i++ {
					files = append(files, fmt.Sprintf("table_%02d.ndjson", i))
				}
				files = append(files, "metadata.json")
				sort.Strings(files)
				app.dumpResult = &DumpResultSummary{
					Outcome:          DumpOutcomeSuccess,
					OutputDir:        "/tmp/dolly-out",
					Files:            files,
					TableCount:       20,
					TotalRowEstimate: int64Ptr(5000),
				}
			},
		},
	}

	for _, tt := range screens {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp()
			app.screen = ScreenDump
			app.width = 60
			app.height = 20
			if tt.setup != nil {
				tt.setup(app)
			}
			got := stripANSIForGolden(app.View().Content)

			path := filepath.Join("testdata", tt.name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s", tt.name)
			}
		})
	}
}

func int64Ptr(n int64) *int64 {
	return &n
}

func assertViewFitsViewport(t *testing.T, view string, height int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) > height {
		t.Fatalf("view has %d lines, exceeds %d-row viewport", len(lines), height)
	}
	actionLine := -1
	for i, line := range lines {
		if strings.Contains(line, "Y confirm") || strings.Contains(line, "Y quit") ||
			strings.Contains(line, "Enter continue") {
			actionLine = i
			break
		}
	}
	if actionLine < 0 {
		t.Fatal("modal action line not found in view")
	}
	if actionLine >= height {
		t.Fatalf("modal action on line %d (1-based), outside %d-row viewport", actionLine+1, height)
	}
}

func stripANSIForGolden(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	skip := false
	for _, r := range s {
		if r == '\x1b' {
			skip = true
			continue
		}
		if skip {
			if r == 'm' {
				skip = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
