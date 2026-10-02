package tui

import (
	"context"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/clonework"
)

func TestProductionCloneRunnerReturnsVerifyReport(t *testing.T) {
	orig := cloneworkRun
	t.Cleanup(func() { cloneworkRun = orig })
	cloneworkRun = func(_ context.Context, p clonework.Params, _ func(clonework.ProgressEvent)) error {
		if p.VerifyWarnings == nil || p.VerifyTables == nil {
			t.Fatal("verify report pointers were not passed")
		}
		*p.VerifyTables = 4
		*p.VerifyWarnings = []string{"verify: public.orders row count source=2 target=1"}
		return nil
	}
	result, err := (productionCloneRunner{}).Run(context.Background(), CloneDraft{
		SourceDSN: "postgres://u:p@h/src",
		CloneName: "src_dolly_1",
	}, []string{"public"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.VerifyRan || result.VerifyTables != 4 || len(result.VerifyWarnings) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestProductionCloneRunnerPassesTargetDir(t *testing.T) {
	orig := cloneworkRun
	defer func() { cloneworkRun = orig }()

	var got clonework.Params
	cloneworkRun = func(_ context.Context, p clonework.Params, _ func(clonework.ProgressEvent)) error {
		got = p
		return nil
	}

	draft := CloneDraft{
		SourceDSN: "postgres://u:p@h/src",
		CloneName: "src_kloned_1",
		TargetDir: "/data/pgclone",
	}
	runner := productionCloneRunner{}
	if _, err := runner.Run(context.Background(), draft, []string{"public"}, nil); err != nil {
		t.Fatal(err)
	}
	if got.TargetDir != "/data/pgclone" {
		t.Fatalf("TargetDir = %q, want /data/pgclone", got.TargetDir)
	}
}

func TestProductionCloneRunnerPassesDumpDir(t *testing.T) {
	orig := cloneworkRun
	defer func() { cloneworkRun = orig }()

	var got clonework.Params
	cloneworkRun = func(_ context.Context, p clonework.Params, _ func(clonework.ProgressEvent)) error {
		got = p
		return nil
	}

	draft := CloneDraft{
		SourceDSN: "postgres://u:p@h/src",
		CloneName: "src_kloned_1",
		DumpDir:   "/data/dumps",
	}
	runner := productionCloneRunner{}
	if _, err := runner.Run(context.Background(), draft, []string{"public"}, nil); err != nil {
		t.Fatal(err)
	}
	if got.DumpDir != "/data/dumps" {
		t.Fatalf("DumpDir = %q, want /data/dumps", got.DumpDir)
	}
}

func TestProductionCloneRunnerSetsSkipCreateFromDraft(t *testing.T) {
	orig := cloneworkRun
	defer func() { cloneworkRun = orig }()

	var got clonework.Params
	cloneworkRun = func(_ context.Context, p clonework.Params, _ func(clonework.ProgressEvent)) error {
		got = p
		return nil
	}

	draft := CloneDraft{SourceDSN: "postgres://u:p@h/src", CloneName: "src_kloned_1", SkipCreate: true}
	runner := productionCloneRunner{}
	if _, err := runner.Run(context.Background(), draft, []string{"public"}, nil); err != nil {
		t.Fatal(err)
	}
	if !got.SkipCreateSet {
		t.Fatal("expected SkipCreateSet from TUI send")
	}
	if !got.SkipCreate {
		t.Fatal("expected SkipCreate=true from draft")
	}
}
