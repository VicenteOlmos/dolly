package tui

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/runopts"
)

func TestDumpOverridesFromDraftChunkSize(t *testing.T) {
	overrides, err := dumpOverridesFromDraft(DumpDraft{ChunkSizeText: "500"})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.ChunkSize != 500 {
		t.Fatalf("ChunkSize = %d, want 500", overrides.ChunkSize)
	}

	overrides, err = dumpOverridesFromDraft(DumpDraft{})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.ChunkSize != 0 {
		t.Fatalf("empty chunk size = %d, want 0", overrides.ChunkSize)
	}

	_, err = dumpOverridesFromDraft(DumpDraft{ChunkSizeText: "0"})
	if err == nil {
		t.Fatal("expected error for chunk size 0")
	}
	_, err = dumpOverridesFromDraft(DumpDraft{ChunkSizeText: "-1"})
	if err == nil {
		t.Fatal("expected error for negative chunk size")
	}

	cfg := config.DefaultConfig()
	cfg.Dump.SlowChunkSize = 250

	overrides, err = dumpOverridesFromDraft(DumpDraft{ChunkSizeText: "100", SlowConnection: true})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := runopts.BuildDumpOptions(overrides, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectSlowChunkSizeEquals(100, opts...) {
		t.Fatal("expected draft chunk size override")
	}

	opts, err = runopts.BuildDumpOptions(runopts.DumpOverrides{SlowConnection: true}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectSlowChunkSizeEquals(250, opts...) {
		t.Fatal("expected config chunk size when override unset")
	}
}
