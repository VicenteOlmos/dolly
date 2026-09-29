package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/dumphistory"
)

func TestFormatDumpHistoryLabel(t *testing.T) {
	label := formatDumpHistoryLabel(dumphistory.Record{
		Seq:         3,
		SchemaLabel: "public",
		TableCount:  12,
	})
	want := "#3 · public · 12 tables"
	if label != want {
		t.Fatalf("label = %q, want %q", label, want)
	}

	emptySchema := formatDumpHistoryLabel(dumphistory.Record{
		Seq:        1,
		TableCount: 5,
	})
	if emptySchema != "#1 · ? · 5 tables" {
		t.Fatalf("empty schema label = %q, want #1 · ? · 5 tables", emptySchema)
	}

	withMeta := formatDumpHistoryLabel(dumphistory.Record{
		Seq:            2,
		SchemaLabel:    "app",
		TableCount:     4,
		CreatedAt:      time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		SourceDatabase: "prod",
	})
	if withMeta != "#2 · app · 4 tables · Jun 7 · prod" {
		t.Fatalf("meta label = %q, want date and source DB", withMeta)
	}
}

func TestRefreshDumpHistory(t *testing.T) {
	baseDir := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := dumphistory.NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Register(dumphistory.Record{
		Seq:         1,
		BaseDir:     baseDir,
		Path:        filepath.Join(baseDir, "1"),
		CreatedAt:   now,
		SchemaLabel: "app",
		TableCount:  3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(dumphistory.Record{
		Seq:         2,
		BaseDir:     baseDir,
		Path:        filepath.Join(baseDir, "2"),
		CreatedAt:   now,
		SchemaLabel: "billing",
		TableCount:  7,
	}); err != nil {
		t.Fatal(err)
	}

	draft := &DumpDraft{OutputDir: baseDir}
	refreshDumpHistory(draft, store)

	if len(draft.History.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(draft.History.Entries))
	}
	labels := map[string]bool{
		draft.History.Entries[0].Label: true,
		draft.History.Entries[1].Label: true,
	}
	wantLabels := []string{
		"#1 · app · 3 tables · " + now.Format("Jan 2"),
		"#2 · billing · 7 tables · " + now.Format("Jan 2"),
	}
	if !labels[wantLabels[0]] || !labels[wantLabels[1]] {
		t.Fatalf("labels = %v, want %v", []string{
			draft.History.Entries[0].Label,
			draft.History.Entries[1].Label,
		}, wantLabels)
	}

	emptyDraft := &DumpDraft{}
	refreshDumpHistory(emptyDraft, store)
	if len(emptyDraft.History.Entries) != 0 {
		t.Fatalf("empty output dir entries = %d, want 0", len(emptyDraft.History.Entries))
	}
}

func TestRenderDumpHistoryLinesCursorHighlight(t *testing.T) {
	empty := renderDumpHistoryLines(nil, 5)
	if len(empty) != 1 || !containsPlain(empty[0], "no dumps yet") {
		t.Fatalf("empty lines = %v, want muted placeholder", empty)
	}

	h := &DumpHistoryState{
		Entries: []DumpHistoryEntry{
			{Label: "#1 · public · 1 tables"},
			{Label: "#2 · app · 2 tables"},
			{Label: "#3 · billing · 3 tables"},
		},
		Cursor: 1,
	}
	lines := renderDumpHistoryLines(h, 10)
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	for i, line := range lines {
		plain := stripANSIForGolden(line)
		if i == h.Cursor {
			if plain != "> "+h.Entries[i].Label {
				t.Fatalf("cursor line = %q, want > prefix on %q", plain, h.Entries[i].Label)
			}
			continue
		}
		if plain != "  "+h.Entries[i].Label {
			t.Fatalf("line %d = %q, want two-space prefix", i, plain)
		}
	}
}

func TestDumpScreenRestoreUsesTypedDirectory(t *testing.T) {
	d := &dumpScreen{draft: &DumpDraft{History: DumpHistoryState{
		Entries: []DumpHistoryEntry{{Path: "/history/1"}},
	}}, restoreDir: "  /tmp/other-dump  "}
	cmd := d.requestRestore()
	if cmd == nil {
		t.Fatal("expected restore command")
	}
	msg, ok := cmd().(restoreConfirmRequestedMsg)
	if !ok {
		t.Fatalf("msg = %T", cmd())
	}
	if msg.inputDir != "/tmp/other-dump" {
		t.Fatalf("inputDir = %q", msg.inputDir)
	}

	d.restoreDir = ""
	cmd = d.requestRestore()
	msg = cmd().(restoreConfirmRequestedMsg)
	if msg.inputDir != "/history/1" {
		t.Fatalf("history inputDir = %q", msg.inputDir)
	}
}

func TestDumpScreenEnterRestoresFocusedPath(t *testing.T) {
	status := DumpStatusIdle
	d := &dumpScreen{draft: &DumpDraft{}, dumpStatus: &status, restoreDir: "/tmp/typed-dump", restoreDirFocus: true, nav: NewSectionNav(dumpSectionCount)}
	d.nav.EnterInside(dumpSectionHistory)
	cmd := d.Update(keyPress("", tea.KeyEnter, 0))
	if cmd == nil {
		t.Fatal("Enter while editing restore path should request restore")
	}
	msg, ok := cmd().(restoreConfirmRequestedMsg)
	if !ok || msg.inputDir != "/tmp/typed-dump" {
		t.Fatalf("restore message = %#v, want typed path", msg)
	}
}
