package tui

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestDumpOverridesFromDraft(t *testing.T) {
	got, err := dumpOverridesFromDraft(DumpDraft{
		NoTransaction:       true,
		SlowConnection:      true,
		RequireSafeKey:      true,
		PercentText:         " 25 ",
		SeedFile:            " seeds.json ",
		ChunkTables:         "public.orders, public.events",
		MaxDepthText:        "3",
		MaxTablesText:       "4",
		MaxRowsText:         "100",
		MaxRowsPerTableText: "50",
		IncludeTables:       "public.users",
		ExcludeTables:       "public.audit",
		Workers:             4,
		WorkersSet:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.NoTransaction || !got.SlowConnection || !got.RequireSafeKey || got.Percent != 25 || got.SeedFile != "seeds.json" {
		t.Fatalf("overrides = %+v", got)
	}
	if !got.WorkersSet || got.Workers != 4 {
		t.Fatalf("workers = %d set=%v", got.Workers, got.WorkersSet)
	}
	if len(got.ChunkTables) != 2 || got.ChunkTables[0] != "public.orders" || got.ChunkTables[1] != "public.events" {
		t.Fatalf("chunk tables = %v", got.ChunkTables)
	}
	if got.MaxDepth != 3 || got.MaxTables != 4 || got.MaxRows != 100 || got.MaxRowsPerTable != 50 {
		t.Fatalf("subset limits = %+v", got)
	}
	if len(got.IncludeTables) != 1 || got.IncludeTables[0] != "public.users" || len(got.ExcludeTables) != 1 || got.ExcludeTables[0] != "public.audit" {
		t.Fatalf("selectors = include %v exclude %v", got.IncludeTables, got.ExcludeTables)
	}
	if _, err := dumpOverridesFromDraft(DumpDraft{PercentText: "0"}); err == nil {
		t.Fatal("percent 0 text should be rejected")
	}
}

func TestDumpModeEditsChunkTableFile(t *testing.T) {
	app := NewApp()
	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionMode)
	ds.modeField = modeFieldChunkTableFile
	if !ds.modeTextFocused() || !strings.Contains(strings.Join(ds.modeSectionLines(), "\n"), "Chunk table file") {
		t.Fatal("chunk table file is not an editable Mode row")
	}
	for _, r := range "tables/chunk.txt" {
		ds.Update(keyPress(string(r), r, 0))
	}
	if app.dump.ChunkTableFile != "tables/chunk.txt" {
		t.Fatalf("ChunkTableFile = %q", app.dump.ChunkTableFile)
	}
	if !strings.Contains(ds.modeSummary(), "chunk-file") {
		t.Fatalf("mode summary = %q", ds.modeSummary())
	}
}

type draftRecordingDumpRunner struct {
	draft DumpDraft
}

func (r *draftRecordingDumpRunner) Run(_ context.Context, _ *sql.DB, _ string, draft DumpDraft, _ []string, _, _ string, _ func(dump.ProgressEvent)) error {
	r.draft = draft
	return nil
}

func TestDumpModeSectionReachesRunner(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := &draftRecordingDumpRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, runner, nil, nil, nil, nil, false)
	app.db = conn
	app.screen = ScreenDump
	app.dump.OutputDir = t.TempDir()
	app.width = 80
	app.height = 24
	seedDumpSchemas(app)

	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionMode)

	app = drainUpdate(app, keyPress("s", 's', 0))
	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	app = drainUpdate(app, keyPress("k", 'k', 0))
	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	app = drainUpdate(app, keyPress("", tea.KeyRight, 0))
	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	app = drainUpdate(app, keyPress("2", '2', 0))
	app = drainUpdate(app, keyPress("5", '5', 0))
	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	for _, r := range "seeds.json" {
		app = drainUpdate(app, keyPress(string(r), r, 0))
	}
	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	for _, r := range "public.orders" {
		app = drainUpdate(app, keyPress(string(r), r, 0))
	}

	app = drainUpdate(app, dumpRequestedMsg{})
	if app.dumpStatus != DumpStatusComplete && app.dumpStatus != DumpStatusIdle && app.dumpError == "" {
		if runner.draft.PercentText == "" && app.dumpStatus == DumpStatusError {
			t.Fatalf("dump failed before recording draft: %s", app.dumpError)
		}
	}
	got := runner.draft
	if !got.SlowConnection || !got.RequireSafeKey || !got.WorkersSet || got.Workers != 1 {
		t.Fatalf("draft toggles = %+v", got)
	}
	if got.PercentText != "25" || got.SeedFile != "seeds.json" || got.ChunkTables != "public.orders" {
		t.Fatalf("draft text = percent %q seed %q chunk %q", got.PercentText, got.SeedFile, got.ChunkTables)
	}
}
