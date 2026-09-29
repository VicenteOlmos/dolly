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

func TestDumpOverridesFromDraftRetryMax(t *testing.T) {
	overrides, err := dumpOverridesFromDraft(DumpDraft{RetryMaxText: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.RetryMax != 3 {
		t.Fatalf("RetryMax = %d, want 3", overrides.RetryMax)
	}

	overrides, err = dumpOverridesFromDraft(DumpDraft{})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.RetryMax != 0 {
		t.Fatalf("empty retry max = %d, want 0", overrides.RetryMax)
	}

	_, err = dumpOverridesFromDraft(DumpDraft{RetryMaxText: "-1"})
	if err == nil {
		t.Fatal("expected error for negative retry max")
	}

	cfg := config.DefaultConfig()
	cfg.Dump.SlowRetryMax = 2
	overrides, err = dumpOverridesFromDraft(DumpDraft{RetryMaxText: "5", SlowConnection: true})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := runopts.BuildDumpOptions(overrides, cfg)
	if err != nil {
		t.Fatal(err)
	}
	max, _ := dump.InspectSlowRetry(opts...)
	if max != 5 {
		t.Fatalf("retry max = %d, want 5", max)
	}
}
