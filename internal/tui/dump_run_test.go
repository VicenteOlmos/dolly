package tui

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/runopts"
)

func TestDumpOverridesRequireSafeKeyFromDraft(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.RequireSafeKey = true

	overrides, err := dumpOverridesFromDraft(DumpDraft{})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.RequireSafeKeySet {
		t.Fatal("empty draft must not set RequireSafeKeySet")
	}
	opts, err := runopts.BuildDumpOptions(overrides, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectRequireSafeKey(opts...) {
		t.Fatal("expected require_safe_key from config when draft untouched")
	}

	overrides, err = dumpOverridesFromDraft(DumpDraft{RequireSafeKey: false, RequireSafeKeySet: true})
	if err != nil {
		t.Fatal(err)
	}
	opts, err = runopts.BuildDumpOptions(overrides, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if dump.InspectRequireSafeKey(opts...) {
		t.Fatal("draft false must override config true")
	}

	cfg.Dump.RequireSafeKey = false
	overrides, err = dumpOverridesFromDraft(DumpDraft{RequireSafeKey: true, RequireSafeKeySet: true})
	if err != nil {
		t.Fatal(err)
	}
	opts, err = runopts.BuildDumpOptions(overrides, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectRequireSafeKey(opts...) {
		t.Fatal("draft true must override config false")
	}
}

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

func TestDumpOverridesFromDraftExcludeTableFile(t *testing.T) {
	overrides, err := dumpOverridesFromDraft(DumpDraft{ExcludeTableFile: "tables/exclude.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides.ExcludeTableFiles) != 1 || overrides.ExcludeTableFiles[0] != "tables/exclude.txt" {
		t.Fatalf("ExcludeTableFiles = %v", overrides.ExcludeTableFiles)
	}
}

func TestDumpOverridesFromDraftMaxInListSize(t *testing.T) {
	overrides, err := dumpOverridesFromDraft(DumpDraft{MaxInListText: "120"})
	if err != nil {
		t.Fatal(err)
	}
	if overrides.MaxInListSize != 120 {
		t.Fatalf("MaxInListSize = %d, want 120", overrides.MaxInListSize)
	}

	cfg := config.DefaultConfig()
	cfg.Subset.MaxInListSize = 80
	opts, err := runopts.BuildDumpOptions(runopts.DumpOverrides{
		Percent: 10,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	subset := dump.InspectOptions(opts...)
	if subset == nil || subset.Limits.MaxInListSize != 80 {
		t.Fatalf("config max_in_list_size = %d, want 80", subset.Limits.MaxInListSize)
	}

	opts, err = runopts.BuildDumpOptions(runopts.DumpOverrides{
		Percent:       10,
		MaxInListSize: 42,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	subset = dump.InspectOptions(opts...)
	if subset == nil || subset.Limits.MaxInListSize != 42 {
		t.Fatalf("override max_in_list_size = %d, want 42", subset.Limits.MaxInListSize)
	}
}

func TestDumpOverridesFromDraftChunkTableFile(t *testing.T) {
	overrides, err := dumpOverridesFromDraft(DumpDraft{ChunkTableFile: "tables/chunk.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides.ChunkTableFiles) != 1 || overrides.ChunkTableFiles[0] != "tables/chunk.txt" {
		t.Fatalf("ChunkTableFiles = %v", overrides.ChunkTableFiles)
	}
}

func TestProductionDumpRunnerHonorsExcludeSchemas(t *testing.T) {
	var dumpSchemas []string
	var captureSchemas []string

	oldLoad := tuiLoadConfig
	oldRun := tuiDumpRun
	oldCapture := tuiSchemaCapture
	t.Cleanup(func() {
		tuiLoadConfig = oldLoad
		tuiDumpRun = oldRun
		tuiSchemaCapture = oldCapture
	})

	cfg := config.DefaultConfig()
	cfg.Dump.ExcludeSchemas = []string{"staging"}
	tuiLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	tuiDumpRun = func(_ context.Context, _ *sql.DB, _ string, opts ...dump.Option) error {
		dumpSchemas = append([]string(nil), dump.InspectSchemas(opts...)...)
		return nil
	}
	tuiSchemaCapture = func(_ context.Context, _ string, _ string, schemas []string) error {
		captureSchemas = append([]string(nil), schemas...)
		return nil
	}

	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := productionDumpRunner{}
	err = runner.Run(context.Background(), conn, t.TempDir(), DumpDraft{}, []string{"app", "staging"}, "db", "postgres://u:p@h/db", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(dumpSchemas) != 1 || dumpSchemas[0] != "app" {
		t.Fatalf("dump schemas = %v, want [app]", dumpSchemas)
	}
	if len(captureSchemas) != 1 || captureSchemas[0] != "app" {
		t.Fatalf("schema capture schemas = %v, want [app]", captureSchemas)
	}
}

func TestProductionDumpRunnerExcludeSchemasEmptyScope(t *testing.T) {
	oldLoad := tuiLoadConfig
	oldRun := tuiDumpRun
	t.Cleanup(func() {
		tuiLoadConfig = oldLoad
		tuiDumpRun = oldRun
	})

	cfg := config.DefaultConfig()
	cfg.Dump.ExcludeSchemas = []string{"app"}
	tuiLoadConfig = func(string) (*config.Config, error) { return cfg, nil }
	tuiDumpRun = func(context.Context, *sql.DB, string, ...dump.Option) error {
		t.Fatal("dump should not run when exclusions empty schema scope")
		return nil
	}

	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	runner := productionDumpRunner{}
	err = runner.Run(context.Background(), conn, t.TempDir(), DumpDraft{}, []string{"app"}, "db", "", nil)
	if err == nil || !strings.Contains(err.Error(), "empty after applying exclude schemas") {
		t.Fatalf("err = %v", err)
	}
}
