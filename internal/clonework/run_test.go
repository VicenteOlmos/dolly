package clonework

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestRunRequiresSchemas(t *testing.T) {
	err := Run(context.Background(), Params{SourceDSN: "postgres://u:p@h/db"}, nil)
	if err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("err = %v, want schema required", err)
	}
}

func TestRunPropagatesSchemasToCloneOptions(t *testing.T) {
	orig := runInProcess
	defer func() { runInProcess = orig }()

	var got []string
	var gotProgressCallback bool
	var progress []clone.ProgressEvent
	runInProcess = func(_ context.Context, opts clone.Options, onProgress func(clone.ProgressEvent)) error {
		got = append([]string(nil), clone.SchemasFromOptions(opts)...)
		gotProgressCallback = onProgress != nil
		if onProgress != nil {
			onProgress(clone.ProgressEvent{Phase: "test", Step: "mock progress", Current: 1, Total: 1})
		}
		return nil
	}

	err := Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src",
		TargetDSN: "postgres://u:p@h/target",
		Schemas:   []string{"public", "app"},
	}, func(ev clone.ProgressEvent) { progress = append(progress, ev) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "public" || got[1] != "app" {
		t.Fatalf("schemas = %v, want [public app]", got)
	}
	if !gotProgressCallback {
		t.Fatal("Run did not pass progress callback to clone runner boundary")
	}
	if len(progress) != 1 || progress[0].Step != "mock progress" {
		t.Fatalf("progress = %v, want [mock progress]", progress)
	}
}

func TestRunPropagatesConfiguredTargetDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(`{"clone":{"target_dir":"/data/clone"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	orig := runInProcess
	t.Cleanup(func() { runInProcess = orig })
	var got clone.Options
	runInProcess = func(_ context.Context, opts clone.Options, _ func(clone.ProgressEvent)) error { got = opts; return nil }
	if err := Run(context.Background(), Params{SourceDSN: "postgres://u:p@h/src", Schemas: []string{"public"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got.TargetDir != "/data/clone" {
		t.Fatalf("TargetDir = %q, want /data/clone", got.TargetDir)
	}
}

func TestRunWiresSanitizationWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(`{"sanitization":{"enabled":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	orig := runInProcess
	defer func() { runInProcess = orig }()

	var gotOpts clone.Options
	runInProcess = func(_ context.Context, opts clone.Options, _ func(clone.ProgressEvent)) error {
		gotOpts = opts
		return nil
	}

	if err := Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src",
		Schemas:   []string{"public"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	rt := dump.InspectRowTransform(gotOpts.DumpOpts...)
	if rt == nil {
		t.Fatal("expected row transform when sanitization enabled")
	}
	out, err := rt("public", "users", []db.Column{{Name: "email", DataType: "text"}}, map[string]any{"email": "a@b.c"})
	if err != nil {
		t.Fatal(err)
	}
	if out["email"] != "redacted@example.com" {
		t.Fatalf("email = %v, want sanitized placeholder", out["email"])
	}
}

func TestRunSanitizationDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	orig := runInProcess
	defer func() { runInProcess = orig }()

	var gotOpts clone.Options
	runInProcess = func(_ context.Context, opts clone.Options, _ func(clone.ProgressEvent)) error {
		gotOpts = opts
		return nil
	}

	if err := Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src",
		Schemas:   []string{"public"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if rt := dump.InspectRowTransform(gotOpts.DumpOpts...); rt != nil {
		t.Fatal("expected no row transform when sanitization disabled")
	}
}

func TestRunInProcessWiresProgressAndSilentRunner(t *testing.T) {
	orig := cloneRun
	defer func() { cloneRun = orig }()

	var gotOpts clone.Options
	var progress []clone.ProgressEvent
	onProgress := func(ev clone.ProgressEvent) { progress = append(progress, ev) }

	cloneRun = func(_ context.Context, opts clone.Options) error {
		gotOpts = opts
		if opts.ProgressEvent != nil {
			opts.ProgressEvent(clone.ProgressEvent{Phase: "test", Step: "boundary progress", Current: 1, Total: 1})
		}
		return nil
	}

	err := runInProcess(context.Background(), clone.Options{
		SourceDSN: "postgres://u:p@h/src",
		CloneName: "src_kloned_1",
		Strategy:  "schema-replay",
	}, onProgress)
	if err != nil {
		t.Fatal(err)
	}
	if gotOpts.ProgressEvent == nil {
		t.Fatal("runInProcess did not set opts.ProgressEvent")
	}
	silent, ok := gotOpts.CommandRunner.(clone.SilentCommandRunner)
	if !ok {
		t.Fatalf("CommandRunner = %T, want SilentCommandRunner", gotOpts.CommandRunner)
	}
	if _, ok := silent.Inner.(clone.OSCommandRunner); !ok {
		t.Fatalf("SilentCommandRunner.Inner = %T, want OSCommandRunner", silent.Inner)
	}
	if len(progress) != 1 || progress[0].Step != "boundary progress" {
		t.Fatalf("progress = %v, want [boundary progress]", progress)
	}
}

func TestRunAdapterLegacyStringPath(t *testing.T) {
	orig := cloneRun
	defer func() { cloneRun = orig }()

	var gotOpts clone.Options
	cloneRun = func(_ context.Context, opts clone.Options) error {
		gotOpts = opts
		return nil
	}

	// Pass nil onProgress — runInProcess should not set ProgressEvent
	err := runInProcess(context.Background(), clone.Options{
		SourceDSN: "postgres://u:p@h/src",
		CloneName: "src_kloned_1",
		Strategy:  "schema-replay",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotOpts.ProgressEvent != nil {
		t.Fatal("runInProcess should not set ProgressEvent when onProgress is nil")
	}
}

func TestRunRejectsInvalidCloneName(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	err = Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src",
		CloneName: "prod-copy",
		Schemas:   []string{"public"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid clone name 'prod-copy'")
	}
	if !strings.Contains(err.Error(), "validate clone name") {
		t.Fatalf("error = %q, want 'validate clone name'", err.Error())
	}
}

func TestRunPropagatesStatementTimeoutAndMaxOpenConns(t *testing.T) {
	dir := t.TempDir()
	cfgJSON := `{
		"db":{"statement_timeout":"5min","max_open_conns":7},
		"clone":{"target_url":"postgres://u:p@h/target"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	orig := runInProcess
	defer func() { runInProcess = orig }()

	var got clone.Options
	runInProcess = func(_ context.Context, opts clone.Options, _ func(clone.ProgressEvent)) error {
		got = opts
		return nil
	}

	if err := Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src?sslmode=disable",
		TargetDSN: "postgres://u:p@h/other?sslmode=disable",
		Schemas:   []string{"public"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.SourceDSN, "statement_timeout=5min") {
		t.Fatalf("SourceDSN = %q, want statement_timeout", got.SourceDSN)
	}
	if !strings.Contains(got.TargetDSN, "statement_timeout=5min") {
		t.Fatalf("TargetDSN = %q, want statement_timeout", got.TargetDSN)
	}
	if got.MaxOpenConns != 7 {
		t.Fatalf("MaxOpenConns = %d, want 7", got.MaxOpenConns)
	}
}

func TestRunPropagatesPermissionCacheConfig(t *testing.T) {
	dir := t.TempDir()
	cfgJSON := `{
		"clone":{
			"preflight":{
				"cache_permissions":true,
				"cache_permissions_path":"/tmp/cache.yaml",
				"cache_permissions_ttl":"12h"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	orig := runInProcess
	defer func() { runInProcess = orig }()

	var got clone.Options
	runInProcess = func(_ context.Context, opts clone.Options, _ func(clone.ProgressEvent)) error {
		got = opts
		return nil
	}

	if err := Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src",
		Schemas:   []string{"public"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if !got.PermissionCache.Enabled {
		t.Fatal("expected permission cache enabled")
	}
	if got.PermissionCache.Path != "/tmp/cache.yaml" {
		t.Fatalf("cache path = %q, want /tmp/cache.yaml", got.PermissionCache.Path)
	}
	if got.PermissionCache.TTL != 12*time.Hour {
		t.Fatalf("cache TTL = %v, want 12h", got.PermissionCache.TTL)
	}
}

func TestRunRejectsInvalidPermissionCacheTTLBeforeClone(t *testing.T) {
	dir := t.TempDir()
	cfgJSON := `{"clone":{"preflight":{"cache_permissions":true,"cache_permissions_ttl":"nope"}}}`
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	orig := runInProcess
	called := false
	runInProcess = func(_ context.Context, _ clone.Options, _ func(clone.ProgressEvent)) error {
		called = true
		return nil
	}
	defer func() { runInProcess = orig }()

	err = Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src",
		Schemas:   []string{"public"},
	}, nil)
	if err == nil {
		t.Fatal("expected invalid TTL error")
	}
	if !strings.Contains(err.Error(), "permission cache config") {
		t.Fatalf("error = %q, want permission cache config", err.Error())
	}
	if called {
		t.Fatal("clone runner should not run when TTL is invalid")
	}
}

func TestRunDefaultMaxOpenConnsWhenUnset(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	orig := runInProcess
	defer func() { runInProcess = orig }()

	var got clone.Options
	runInProcess = func(_ context.Context, opts clone.Options, _ func(clone.ProgressEvent)) error {
		got = opts
		return nil
	}

	if err := Run(context.Background(), Params{
		SourceDSN: "postgres://u:p@h/src",
		Schemas:   []string{"public"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if got.MaxOpenConns != 5 {
		t.Fatalf("MaxOpenConns = %d, want default 5", got.MaxOpenConns)
	}
}
