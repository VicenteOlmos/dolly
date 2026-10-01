package runopts

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestBuildDumpOptionsFromConfigWorkersSubsetInclude(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.Workers = 4
	cfg.Dump.IncludeTables = []string{"public.events"}
	cfg.Subset.Percent = 25

	_, err := BuildDumpOptions(DumpOverrides{}, cfg)
	if err == nil {
		t.Fatal("expected workers vs subset conflict")
	}

	cfg.Subset.Percent = 0
	opts, err := BuildDumpOptions(DumpOverrides{}, cfg)
	if err != nil {
		t.Fatalf("BuildDumpOptions: %v", err)
	}
	if dump.InspectWorkers(opts...) != 4 {
		t.Fatalf("workers = %d, want 4", dump.InspectWorkers(opts...))
	}
	policy, _, err := ResolveTableSelection(DumpOverrides{}, cfg)
	if err != nil || policy == nil || len(policy.Includes) != 1 {
		t.Fatalf("include policy: %v err %v", policy, err)
	}

	cfg2 := config.DefaultConfig()
	cfg2.Subset.Percent = 10
	opts, err = BuildDumpOptions(DumpOverrides{}, cfg2)
	if err != nil {
		t.Fatalf("subset percent: %v", err)
	}
	sub := dump.InspectOptions(opts...)
	if sub == nil || sub.Percent != 10 {
		t.Fatalf("subset = %v, want 10%%", sub)
	}
}

func TestRestoreHistoryOptionsUseRestoreSection(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Restore.Workers = 2

	opts, err := RestoreHistoryOptions(cfg, dir, []string{"public"}, false, "postgres://localhost/db")
	if err != nil {
		t.Fatalf("RestoreHistoryOptions: %v", err)
	}
	if restore.InspectWorkers(opts...) != 2 {
		t.Fatalf("workers = %d, want 2", restore.InspectWorkers(opts...))
	}
	if restore.InspectPartialStateManifest(opts...) == "" {
		t.Fatal("expected partial state manifest for parallel restore")
	}

	cfg2 := config.DefaultConfig()
	cfg2.Restore.Replace = true
	cfg2.Restore.RestoreOnConflict = "skip"
	if _, err := RestoreHistoryOptions(cfg2, dir, nil, false, ""); err == nil {
		t.Fatal("expected replace vs on_conflict error")
	}

	cfg3 := config.DefaultConfig()
	cfg3.Clone.Replace = true
	cfg3.Clone.RestoreOnConflict = "upsert"
	cfg3.Restore.Replace = false
	cfg3.Restore.RestoreOnConflict = "error"
	opts, err = RestoreHistoryOptions(cfg3, dir, nil, false, "")
	if err != nil {
		t.Fatalf("RestoreHistoryOptions: %v", err)
	}
	if restore.InspectWorkers(opts...) > 1 {
		t.Fatal("clone settings must not enable parallel restore workers")
	}
}

func TestRestoreHistoryOptionsWithOverrides(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()

	opts, err := RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", RestoreHistoryUserOverrides{
		OnConflict: "skip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if restore.InspectConflictPolicy(opts...) != restore.ConflictSkip {
		t.Fatalf("policy = %v, want skip", restore.InspectConflictPolicy(opts...))
	}

	opts, err = RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", RestoreHistoryUserOverrides{
		OnConflict: "upsert",
	})
	if err != nil {
		t.Fatal(err)
	}
	if restore.InspectConflictPolicy(opts...) != restore.ConflictUpsert {
		t.Fatalf("policy = %v, want upsert", restore.InspectConflictPolicy(opts...))
	}

	opts, err = RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", RestoreHistoryUserOverrides{
		Replace:    true,
		ReplaceSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !restore.InspectReplace(opts...) {
		t.Fatal("expected replace from overrides")
	}

	_, err = RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", RestoreHistoryUserOverrides{
		OnConflict: "skip",
		Replace:    true,
		ReplaceSet: true,
	})
	if err == nil {
		t.Fatal("expected replace with skip conflict to fail")
	}
}

func TestBuildDumpOptionsRequireSafeKeyFromConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.RequireSafeKey = true
	opts, err := BuildDumpOptions(DumpOverrides{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !dump.InspectRequireSafeKey(opts...) {
		t.Fatal("expected require_safe_key from config")
	}

	cfg.Dump.RequireSafeKey = true
	opts, err = BuildDumpOptions(DumpOverrides{RequireSafeKey: false, RequireSafeKeySet: true}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if dump.InspectRequireSafeKey(opts...) {
		t.Fatal("explicit --require-safe-key=false must override config true")
	}
}

func TestRestoreHistoryOptionsWorkersOverride(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Restore.Workers = 1

	opts, err := RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", RestoreHistoryUserOverrides{
		Workers:    4,
		WorkersSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if restore.InspectWorkers(opts...) != 4 {
		t.Fatalf("workers = %d, want 4", restore.InspectWorkers(opts...))
	}

	_, err = RestoreHistoryOptionsWithOverrides(cfg, dir, nil, false, "", RestoreHistoryUserOverrides{
		Workers:    4,
		WorkersSet: true,
		Replace:    true,
		ReplaceSet: true,
	})
	if err == nil {
		t.Fatal("expected parallel restore with replace to fail")
	}
}
