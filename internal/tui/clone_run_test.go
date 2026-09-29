package tui

import (
	"context"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/clonework"
)

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
	if err := runner.Run(context.Background(), draft, []string{"public"}, nil); err != nil {
		t.Fatal(err)
	}
	if got.TargetDir != "/data/pgclone" {
		t.Fatalf("TargetDir = %q, want /data/pgclone", got.TargetDir)
	}
}
