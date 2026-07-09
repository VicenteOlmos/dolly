package clonework

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
