package dumphistory

import (
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func TestResolveStorePathDefault(t *testing.T) {
	cwd := t.TempDir()
	got, err := ResolveStorePath(config.DefaultConfig(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, ".dolly", "dump-history.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestResolveStorePathCustomRelative(t *testing.T) {
	cwd := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Dump.HistoryPath = "state/history.json"
	got, err := ResolveStorePath(cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "state", "history.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestResolveStorePathCustomAbsolute(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.HistoryPath = filepath.Join(t.TempDir(), "custom-history.json")
	got, err := ResolveStorePath(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg.Dump.HistoryPath {
		t.Fatalf("path = %q, want %q", got, cfg.Dump.HistoryPath)
	}
}
