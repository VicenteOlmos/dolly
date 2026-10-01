package tui

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/runopts"
)

func TestCycleRestoreOnConflict(t *testing.T) {
	if cycleRestoreOnConflict("") != "skip" {
		t.Fatal("empty should cycle to skip")
	}
	if cycleRestoreOnConflict("skip") != "upsert" {
		t.Fatal("skip should cycle to upsert")
	}
	if cycleRestoreOnConflict("upsert") != "error" {
		t.Fatal("upsert should cycle to error")
	}
}

func TestRestoreHistoryParallelRequiresAckInRunopts(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	_, err := runopts.RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", runopts.RestoreHistoryUserOverrides{
		Workers:    4,
		WorkersSet: true,
	})
	if err == nil || !strings.Contains(err.Error(), "ack-partial-state") {
		t.Fatalf("err = %v, want ack-partial-state requirement", err)
	}
}

func TestRestoreHistoryParallelWithAck(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	_, err := runopts.RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", runopts.RestoreHistoryUserOverrides{
		Workers:         4,
		WorkersSet:      true,
		AckPartialState: true,
	})
	if err != nil {
		t.Fatalf("RestoreHistoryOptionsWithOverrides: %v", err)
	}
}

func TestRestoreHistoryAckGateBlocksRestoreStart(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dumpPath := filepath.Join(t.TempDir(), "1")
	if err := os.MkdirAll(dumpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRestoreRunner{}
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, runner, nil, nil, nil, false)
	app.db = conn
	app.cfg = config.DefaultConfig()
	app.cfg.Restore.Workers = 4
	app.screen = ScreenDump
	app.width, app.height = 80, 24
	app.dump.History = DumpHistoryState{Entries: []DumpHistoryEntry{{Path: dumpPath, Schemas: []string{"public"}}}}
	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionHistory)

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	if !app.modalOpen() {
		t.Fatal("expected confirm modal for parallel restore")
	}
	app = drainUpdate(app, keyPress("y", 'y', 0))
	if app.restoreRunning {
		t.Fatal("expected restore blocked without ack toggle")
	}
	if !containsPlain(app.statusMsg, "acknowledging partial-state") {
		t.Fatalf("statusMsg = %q, want ack gate message", stripANSIForGolden(app.statusMsg))
	}

	app.dump.RestoreAckPartial = true
	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	if !app.modalOpen() {
		t.Fatal("expected confirm modal again")
	}
	app = drainUpdate(app, keyPress("y", 'y', 0))
	if runner.inputDir != dumpPath {
		t.Fatalf("restore inputDir = %q, want restore after ack", runner.inputDir)
	}
}
