package tui

import (
	"testing"
	"time"

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

func TestDumpOverridesFromDraftRetryBase(t *testing.T) {
	overrides, err := dumpOverridesFromDraft(DumpDraft{RetryBaseText: "750ms"})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.RetryBase != "750ms" {
		t.Fatalf("RetryBase = %q, want 750ms", overrides.RetryBase)
	}

	overrides, err = dumpOverridesFromDraft(DumpDraft{})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.RetryBase != "" {
		t.Fatalf("empty retry base = %q, want empty", overrides.RetryBase)
	}

	_, err = dumpOverridesFromDraft(DumpDraft{RetryBaseText: "nope"})
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
	_, err = dumpOverridesFromDraft(DumpDraft{RetryBaseText: "0s"})
	if err == nil {
		t.Fatal("expected error for non-positive duration")
	}

	cfg := config.DefaultConfig()
	overrides, err = dumpOverridesFromDraft(DumpDraft{
		RetryMaxText:   "2",
		RetryBaseText:  "1s",
		SlowConnection: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := runopts.BuildDumpOptions(overrides, cfg)
	if err != nil {
		t.Fatal(err)
	}
	max, base := dump.InspectSlowRetry(opts...)
	if max != 2 || base != time.Second {
		t.Fatalf("retry = %d %v, want 2 1s", max, base)
	}
}

func TestDumpOverridesHonorConfiguredChunkSelectors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.ChunkTables = []string{"public.users"}
	cfg.Dump.SlowChunkSize = 250
	cfg.Dump.SlowRetryMax = 3
	cfg.Dump.SlowRetryBase = "500ms"

	for _, tc := range []struct {
		name  string
		draft DumpDraft
		chunk int
		max   int
		base  time.Duration
	}{
		{"config", DumpDraft{}, 250, 3, 500 * time.Millisecond},
		{"override", DumpDraft{ChunkSizeText: "100", RetryMaxText: "2", RetryBaseText: "1s"}, 100, 2, time.Second},
		{"disable retries", DumpDraft{RetryMaxText: "0", ChunkSizeText: "100"}, 100, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := dumpOverridesFromDraft(tc.draft)
			if err != nil {
				t.Fatal(err)
			}
			opts, err := runopts.BuildDumpOptions(o, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !dump.InspectSlowChunkSizeEquals(tc.chunk, opts...) {
				t.Fatalf("chunk size != %d", tc.chunk)
			}
			max, base := dump.InspectSlowRetry(opts...)
			if max != tc.max || base != tc.base {
				t.Fatalf("retry = %d %v, want %d %v", max, base, tc.max, tc.base)
			}
		})
	}
}

func TestDumpOverridesExplicitZeroRetriesSlowConnection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.SlowRetryMax = 3
	o, err := dumpOverridesFromDraft(DumpDraft{SlowConnection: true, RetryMaxText: "0"})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := runopts.BuildDumpOptions(o, cfg)
	if err != nil {
		t.Fatal(err)
	}
	max, _ := dump.InspectSlowRetry(opts...)
	if max != 0 {
		t.Fatalf("retry max = %d, want 0", max)
	}
}

func TestDumpOverridesFromDraftIncludeTableFile(t *testing.T) {
	overrides, err := dumpOverridesFromDraft(DumpDraft{IncludeTableFile: "tables/a.txt,\ntables/b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tables/a.txt", "tables/b.txt"}
	if len(overrides.IncludeTableFiles) != len(want) {
		t.Fatalf("IncludeTableFiles = %v, want %v", overrides.IncludeTableFiles, want)
	}
	for i := range want {
		if overrides.IncludeTableFiles[i] != want[i] {
			t.Fatalf("IncludeTableFiles[%d] = %q, want %q", i, overrides.IncludeTableFiles[i], want[i])
		}
	}

	empty, err := dumpOverridesFromDraft(DumpDraft{IncludeTableFile: "  \n  "})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.IncludeTableFiles) != 0 {
		t.Fatalf("whitespace-only IncludeTableFiles = %v, want none", empty.IncludeTableFiles)
	}
}
