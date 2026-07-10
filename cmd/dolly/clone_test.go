package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func stubCloneListSchemaNames(t *testing.T, names []string, err error) {
	t.Helper()
	orig := cloneListSchemaNames
	cloneListSchemaNames = func(ctx context.Context, dsn string) ([]string, error) {
		if err != nil {
			return nil, err
		}
		return append([]string(nil), names...), nil
	}
	t.Cleanup(func() { cloneListSchemaNames = orig })
}

func TestParseCloneFlagsHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out := captureStderr(func() {
				_, err := parseCloneFlags(args)
				if !errors.Is(err, errHelp) {
					t.Fatalf("err = %v, want errHelp", err)
				}
			})
			for _, sub := range []string{"--ff", "--schemas"} {
				if !strings.Contains(out, sub) {
					t.Fatalf("clone usage missing %q:\n%s", sub, out)
				}
			}
		})
	}
}

func TestRunCloneHelpNoTTY(t *testing.T) {
	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"--help"})
	if err != nil {
		t.Fatalf("runClone help: %v", err)
	}
}

func TestParseCloneFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    cloneFlags
		wantErr string
	}{
		{
			name: "fast forward short",
			args: []string{"-ff"},
			want: cloneFlags{FastForward: true},
		},
		{
			name: "fast forward long",
			args: []string{"--ff"},
			want: cloneFlags{FastForward: true},
		},
		{
			name: "no flags",
			args: nil,
			want: cloneFlags{FastForward: false},
		},
		{
			name: "connection flag",
			args: []string{"-ff", "--connection", "prod"},
			want: cloneFlags{FastForward: true, Connection: "prod"},
		},
		{
			name: "schemas flag",
			args: []string{"-ff", "--schemas", "app, billing"},
			want: cloneFlags{FastForward: true, Schemas: []string{"app", "billing"}},
		},
		{
			name:    "connection without ff",
			args:    []string{"--connection", "prod"},
			wantErr: "requires -ff",
		},
		{
			name:    "unknown flag",
			args:    []string{"-bogus"},
			wantErr: "flag provided but not defined: -bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCloneFlags(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.FastForward != tt.want.FastForward ||
				got.Strategy != tt.want.Strategy ||
				got.Connection != tt.want.Connection {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if len(got.Schemas) != len(tt.want.Schemas) {
				t.Fatalf("Schemas = %v, want %v", got.Schemas, tt.want.Schemas)
			}
			for i := range tt.want.Schemas {
				if got.Schemas[i] != tt.want.Schemas[i] {
					t.Fatalf("Schemas = %v, want %v", got.Schemas, tt.want.Schemas)
				}
			}
		})
	}
}

func TestResolveCloneSchemasPrecedence(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	cfg.Clone.Schemas = []string{"cfg_a", "cfg_b"}
	stubCloneListSchemaNames(t, []string{"disc_a", "disc_b"}, nil)

	tests := []struct {
		name       string
		flag       []string
		fromPrompt []string
		want       []string
	}{
		{name: "flag wins", flag: []string{"flag_only"}, fromPrompt: []string{"prompt"}, want: []string{"flag_only"}},
		{name: "prompt overrides config", flag: nil, fromPrompt: []string{"prompt_only"}, want: []string{"prompt_only"}},
		{name: "config when no flag or prompt", flag: nil, fromPrompt: nil, want: []string{"cfg_a", "cfg_b"}},
		{name: "prompt when no flag or config", flag: nil, fromPrompt: []string{"prompt_a"}, want: []string{"prompt_a"}},
		{name: "discovery when all empty", flag: nil, fromPrompt: nil, want: []string{"disc_a", "disc_b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCfg := config.DefaultConfig()
			testCfg.Clone.Schemas = cfg.Clone.Schemas
			if tt.name == "prompt when no flag or config" || tt.name == "discovery when all empty" {
				testCfg.Clone.Schemas = nil
			}
			got, err := resolveCloneSchemas(ctx, "postgres://u:p@h/db", tt.flag, testCfg, tt.fromPrompt)
			if err != nil {
				t.Fatalf("resolveCloneSchemas: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestRunCloneExecuteSchemaPropagation(t *testing.T) {
	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	cfg := config.DefaultConfig()
	err := runCloneExecute(context.Background(), cloneFlags{}, cfg, "postgres://u:p@h/db", "clone_x", "", []string{"app", "billing"}, "schema-replay")
	if err != nil {
		t.Fatalf("runCloneExecute: %v", err)
	}
	dumpSchemas := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(dumpSchemas) != 2 || dumpSchemas[0] != "app" || dumpSchemas[1] != "billing" {
		t.Fatalf("dump schemas = %v", dumpSchemas)
	}
	restoreSchemas := restore.InspectSchemas(capturedOpts.RestoreOpts...)
	if len(restoreSchemas) != 2 || restoreSchemas[0] != "app" || restoreSchemas[1] != "billing" {
		t.Fatalf("restore schemas = %v", restoreSchemas)
	}
}

func TestRunCloneFFSchemasFlag(t *testing.T) {
	stubCloneListSchemaNames(t, []string{"should_not_use"}, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	err := runClone([]string{"-ff", "--schemas", "app,billing"})
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("dump schemas = %v, want [app billing]", got)
	}
}

func TestRunCloneFFConfigSchemas(t *testing.T) {
	stubCloneListSchemaNames(t, []string{"should_not_use"}, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.Schemas = []string{"app", "billing"}
		return cfg, nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("dump schemas = %v, want [app billing]", got)
	}
}

func TestRunCloneFFDiscovery(t *testing.T) {
	stubCloneListSchemaNames(t, []string{"app", "billing"}, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("dump schemas = %v, want discovered [app billing]", got)
	}
}

func TestRunCloneFFDefaults(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts.SourceDSN != "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full&statement_timeout=5min" {
		t.Fatalf("SourceDSN = %q, want %q", capturedOpts.SourceDSN, "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full&statement_timeout=5min")
	}
	if capturedOpts.CloneName != "db_a_dolly_1" {
		t.Fatalf("CloneName = %q, want %q", capturedOpts.CloneName, "db_a_dolly_1")
	}
	if capturedOpts.TargetDSN != "" {
		t.Fatalf("TargetDSN = %q, want empty", capturedOpts.TargetDSN)
	}
	if capturedOpts.SkipCreate {
		t.Fatal("SkipCreate should be false by default")
	}
	if capturedOpts.DumpDir != "" {
		t.Fatalf("DumpDir = %q, want empty", capturedOpts.DumpDir)
	}
	if len(capturedOpts.RestoreOpts) != 0 {
		t.Fatalf("expected no RestoreOpts, got %d", len(capturedOpts.RestoreOpts))
	}
}

func TestRunCloneFFStrategyFlagOverridesConfig(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.Strategy = "template"
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff", "--strategy", "schema-replay"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts.Strategy != "schema-replay" {
		t.Fatalf("Strategy = %q, want %q", capturedOpts.Strategy, "schema-replay")
	}
}

func TestRunCloneFFTargetDirFlagOverridesConfig(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.TargetDir = "/data/from-config"
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff", "--target-dir", "/data/from-flag", "--strategy", "replication"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts.TargetDir != "/data/from-flag" {
		t.Fatalf("TargetDir = %q, want %q", capturedOpts.TargetDir, "/data/from-flag")
	}
}

func TestRunCloneFFStrategyConfigPassthrough(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.Strategy = "template"
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts.Strategy != "template" {
		t.Fatalf("Strategy = %q, want %q", capturedOpts.Strategy, "template")
	}
}

func TestRunCloneFFCustomTargetURL(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.TargetURL = "postgres://u:p@h-b:5432/otherdb"
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts.TargetDSN != "postgres://u:p@h-b:5432/otherdb?statement_timeout=5min" {
		t.Fatalf("TargetDSN = %q, want %q", capturedOpts.TargetDSN, "postgres://u:p@h-b:5432/otherdb?statement_timeout=5min")
	}
	if capturedOpts.CloneName != "db_a_dolly_1" {
		t.Fatalf("CloneName = %q, want %q", capturedOpts.CloneName, "db_a_dolly_1")
	}
}

func TestRunCloneSkipCreate(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.SkipCreate = true
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !capturedOpts.SkipCreate {
		t.Fatal("SkipCreate should be true")
	}
}

func TestRunCloneRestoreOptionsWired(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.Replace = true
		cfg.Clone.RestoreOnConflict = "skip"
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff", "--yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedOpts.RestoreOpts) == 0 {
		t.Fatal("expected RestoreOpts to be set")
	}
	if len(capturedOpts.RestoreOpts) != 2 {
		t.Fatalf("expected 2 RestoreOpts, got %d", len(capturedOpts.RestoreOpts))
	}
}

func TestRunCloneReplaceRequiresYesAllModes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Clone.Replace = true

	err := runCloneExecute(context.Background(), cloneFlags{}, cfg, "postgres://u:p@h/db", "clone_x", "", nil, "schema-replay")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("err = %v, want --yes requirement", err)
	}

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	if err := runCloneExecute(context.Background(), cloneFlags{Yes: true}, cfg, "postgres://u:p@h/db", "clone_x", "", nil, "schema-replay"); err != nil {
		t.Fatalf("runCloneExecute with --yes: %v", err)
	}
	if len(capturedOpts.RestoreOpts) == 0 {
		t.Fatal("expected restore opts with replace")
	}
}

func TestRunCloneInvalidRestoreOnConflict(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.RestoreOnConflict = "bogus"
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff"})
	if err == nil {
		t.Fatal("expected error for invalid restore_on_conflict")
	}
	if !strings.Contains(err.Error(), "invalid restore_on_conflict") {
		t.Fatalf("error = %q, want 'invalid restore_on_conflict'", err.Error())
	}
}

func TestRunClonePromptPath(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	defer func() { cloneIsTerminal = origIsTerminal }()

	origPrompt := clonePromptSource
	var promptCalled bool
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, _ *config.SavedSourcePicker) (config.PromptResult, error) {
		promptCalled = true
		return config.PromptResult{
			SourceDSN:     "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full",
			SourceSchemas: []string{"app", "billing"},
			CloneName:     "db_clone_x",
			TargetURL:     "postgres://u:p@h-b:5432/db_tgt",
			Strategy:      "template",
		}, nil
	}
	defer func() { clonePromptSource = origPrompt }()

	err := runClone([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !promptCalled {
		t.Fatal("expected prompt to be called")
	}
	if capturedOpts.CloneName != "db_clone_x" {
		t.Fatalf("CloneName = %q, want %q", capturedOpts.CloneName, "db_clone_x")
	}
	if capturedOpts.TargetDSN != "postgres://u:p@h-b:5432/db_tgt?statement_timeout=5min" {
		t.Fatalf("TargetDSN = %q, want %q", capturedOpts.TargetDSN, "postgres://u:p@h-b:5432/db_tgt?statement_timeout=5min")
	}
	if capturedOpts.Strategy != "template" {
		t.Fatalf("Strategy = %q, want %q", capturedOpts.Strategy, "template")
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("dump schemas = %v, want [app billing]", got)
	}
}

func TestRunCloneInteractivePromptOverridesConfigSchemas(t *testing.T) {
	stubCloneListSchemaNames(t, []string{"app", "billing", "cfg_only"}, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.Schemas = []string{"cfg_only"}
		return cfg, nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	const promptInput = "\nbilling\n\n\n\n"
	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, saved *config.SavedSourcePicker) (config.PromptResult, error) {
		if len(defaults.Schemas) != 1 || defaults.Schemas[0] != "cfg_only" {
			t.Fatalf("prompt defaults.Schemas = %v, want [cfg_only]", defaults.Schemas)
		}
		return config.PromptSource(strings.NewReader(promptInput), w, defaults, saved)
	}
	t.Cleanup(func() { clonePromptSource = origPrompt })

	err := runClone([]string{})
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 1 || got[0] != "billing" {
		t.Fatalf("dump schemas = %v, want [billing] (prompt override, not cfg_only)", got)
	}
}

func TestRunCloneInteractivePromptSchemas(t *testing.T) {
	stubCloneListSchemaNames(t, []string{"app", "billing", "should_not_use"}, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	const promptInput = "\napp, billing\n\n\n\n"
	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, saved *config.SavedSourcePicker) (config.PromptResult, error) {
		return config.PromptSource(strings.NewReader(promptInput), w, defaults, saved)
	}
	t.Cleanup(func() { clonePromptSource = origPrompt })

	err := runClone([]string{})
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("dump schemas = %v, want [app billing]", got)
	}
}

func TestRunClonePromptPathDefaultTargetURL(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.TargetURL = "postgres://u:p@h-b:5432/db_cfg"
		return cfg, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	defer func() { cloneIsTerminal = origIsTerminal }()

	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, _ *config.SavedSourcePicker) (config.PromptResult, error) {
		return config.PromptResult{
			SourceDSN: "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full",
			CloneName: "db_clone_x",
			TargetURL: defaults.TargetURL, // user accepted default
			Strategy:  "streaming-copy",
		}, nil
	}
	defer func() { clonePromptSource = origPrompt }()

	err := runClone([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts.TargetDSN != "postgres://u:p@h-b:5432/db_cfg?statement_timeout=5min" {
		t.Fatalf("TargetDSN = %q, want %q", capturedOpts.TargetDSN, "postgres://u:p@h-b:5432/db_cfg?statement_timeout=5min")
	}
	if capturedOpts.Strategy != "streaming-copy" {
		t.Fatalf("Strategy = %q, want %q", capturedOpts.Strategy, "streaming-copy")
	}
}

func TestRunCloneInteractiveNoDotEnvManual(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{}, config.ErrSourceDSNNotFound
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	defer func() { cloneIsTerminal = origIsTerminal }()

	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, _ *config.SavedSourcePicker) (config.PromptResult, error) {
		if defaults.SourceDSN != "" {
			t.Fatalf("expected empty SourceDSN default, got %q", defaults.SourceDSN)
		}
		return config.PromptResult{
			SourceDSN: "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full",
			CloneName: "db_a_dolly_{n}",
			TargetURL: "",
			Strategy:  "schema-replay",
		}, nil
	}
	defer func() { clonePromptSource = origPrompt }()

	err := runClone([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedOpts.SourceDSN != "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full&statement_timeout=5min" {
		t.Fatalf("SourceDSN = %q, want manual URL", capturedOpts.SourceDSN)
	}
	if capturedOpts.CloneName != "db_a_dolly_1" {
		t.Fatalf("CloneName = %q, want db_a_dolly_1", capturedOpts.CloneName)
	}
	if capturedOpts.Strategy != "schema-replay" {
		t.Fatalf("Strategy = %q, want schema-replay", capturedOpts.Strategy)
	}
}

func TestRunCloneNonTTYWithoutFF(t *testing.T) {
	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{})
	if err == nil {
		t.Fatal("expected error for non-TTY without -ff")
	}
	if !strings.Contains(err.Error(), "not a TTY; use -ff for fast-forward mode") {
		t.Fatalf("error = %q, want 'not a TTY; use -ff for fast-forward mode'", err.Error())
	}
}

func TestRunCloneInteractiveSavedConnection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	store, err := connections.OpenStore(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(connections.Connection{
		Name: "prod", Host: "h-a", Port: "5432", Database: "db_a",
		User: "u", Password: "p", Schemas: []string{"app"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		c := config.DefaultConfig()
		c.SaveConnections = true
		return c, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	defer func() { cloneIsTerminal = origIsTerminal }()

	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, saved *config.SavedSourcePicker) (config.PromptResult, error) {
		if saved == nil {
			t.Fatal("expected saved connection picker when save_connections is enabled")
		}
		return config.PromptSource(strings.NewReader("saved\n1\n\n\n\n"), w, defaults, saved)
	}
	defer func() { clonePromptSource = origPrompt }()

	err = runClone([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedOpts.SourceDSN, "h-a:5432/db_a") {
		t.Fatalf("SourceDSN = %q", capturedOpts.SourceDSN)
	}
	if capturedOpts.Strategy != "schema-replay" {
		t.Fatalf("Strategy = %q, want schema-replay", capturedOpts.Strategy)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 1 || got[0] != "app" {
		t.Fatalf("dump schemas = %v, want [app]", got)
	}
}

func TestRunCloneFFConnectionProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	store, err := connections.OpenStore(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(connections.Connection{
		Name: "prod", Host: "h-a", Port: "5432", Database: "db_a",
		User: "u", Password: "p", Schemas: []string{"app"},
	}); err != nil {
		t.Fatal(err)
	}

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		c := config.DefaultConfig()
		c.SaveConnections = true
		return c, nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	t.Chdir(dir)

	err = runClone([]string{"-ff", "--connection", "prod"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedOpts.SourceDSN, "h-a:5432/db_a") {
		t.Fatalf("SourceDSN = %q", capturedOpts.SourceDSN)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 1 || got[0] != "app" {
		t.Fatalf("dump schemas = %v, want [app]", got)
	}
	rs := restore.InspectSchemas(capturedOpts.RestoreOpts...)
	if len(rs) != 1 || rs[0] != "app" {
		t.Fatalf("restore schemas = %v, want [app]", rs)
	}
}

func TestRunCloneExecuteSanitizationEnabled(t *testing.T) {
	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	cfg := config.DefaultConfig()
	cfg.Sanitization.Enabled = true

	err := runCloneExecute(context.Background(), cloneFlags{}, cfg, "postgres://u:p@h/db", "clone_x", "", []string{"public"}, "schema-replay")
	if err != nil {
		t.Fatalf("runCloneExecute: %v", err)
	}
	rt := dump.InspectRowTransform(capturedOpts.DumpOpts...)
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

func TestRunCloneExecuteSanitizationDisabledDefault(t *testing.T) {
	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	cfg := config.DefaultConfig()

	err := runCloneExecute(context.Background(), cloneFlags{}, cfg, "postgres://u:p@h/db", "clone_x", "", []string{"public"}, "schema-replay")
	if err != nil {
		t.Fatalf("runCloneExecute: %v", err)
	}
	if rt := dump.InspectRowTransform(capturedOpts.DumpOpts...); rt != nil {
		t.Fatal("expected no row transform when sanitization disabled")
	}
}

func TestRunClonePropagatesRunError(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		return errors.New("simulated clone failure")
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff"})
	if err == nil {
		t.Fatal("expected error when cloneRun fails")
	}
	if !strings.Contains(err.Error(), "simulated clone failure") {
		t.Fatalf("error = %q, want 'simulated clone failure'", err.Error())
	}
}

func TestRunCloneProductionScaleRejected(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		// Simulate what clone.Run does: Resolve() rejects "production-scale"
		_, err := clone.Resolve("production-scale", clone.Options{})
		return err
	}
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	err := runClone([]string{"-ff", "--strategy", "production-scale"})
	if err == nil {
		t.Fatal("expected error for production-scale strategy")
	}
	if !strings.Contains(err.Error(), "unknown clone strategy") {
		t.Fatalf("error = %q, want 'unknown clone strategy'", err.Error())
	}
}

func TestRunCloneValidatesCloneName(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error { return nil }
	defer func() { cloneRun = origRun }()

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	defer func() { cloneLoadConfig = origLoadConfig }()

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	defer func() { cloneIsTerminal = origIsTerminal }()

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	defer func() { cloneDotEnvConn = origDotEnvConn }()

	// FF path: clone name is derived from DB name template => always valid.
	// The ValidateCloneName call path is exercised and non-blocking.
	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCloneWithSourceRejectsInvalidCloneName(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error { return nil }
	t.Cleanup(func() { cloneRun = origRun })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, _ *config.SavedSourcePicker) (config.PromptResult, error) {
		return config.PromptResult{
			SourceDSN: "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full",
			CloneName: "prod-copy",
			TargetURL: "",
			Strategy:  "schema-replay",
		}, nil
	}
	t.Cleanup(func() { clonePromptSource = origPrompt })

	err := runClone([]string{})
	if err == nil {
		t.Fatal("expected error for invalid clone name 'prod-copy'")
	}
	if !strings.Contains(err.Error(), "validate clone name") {
		t.Fatalf("error = %q, want 'validate clone name'", err.Error())
	}
}

func TestParseCloneFlagsJSON(t *testing.T) {
	got, err := parseCloneFlags([]string{"-ff", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.JSON {
		t.Fatal("expected JSON to be true")
	}
}

func TestRunCloneJSONOutput(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var capturedOpts clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		capturedOpts = opts
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Clone.Strategy = "template"
		return cfg, nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	args := []string{"-ff", "--json"}
	stdout := captureStdout(func() {
		if err := runClone(args); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})

	var result struct {
		OK             bool     `json:"ok"`
		Command        string   `json:"command"`
		SourceDatabase string   `json:"source_database"`
		CloneName      string   `json:"clone_name"`
		Strategy       string   `json:"strategy"`
		TargetDir      string   `json:"target_dir"`
		Schemas        []string `json:"schemas"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if !result.OK {
		t.Fatal("expected ok=true")
	}
	if result.Command != "clone" {
		t.Fatalf("command = %q, want clone", result.Command)
	}
	if result.SourceDatabase != "db_a" {
		t.Fatalf("source_database = %q, want db_a", result.SourceDatabase)
	}
	if result.CloneName != "db_a_dolly_1" {
		t.Fatalf("clone_name = %q, want db_a_dolly_1", result.CloneName)
	}
	if result.Strategy != "template" {
		t.Fatalf("strategy = %q, want template", result.Strategy)
	}
	_ = capturedOpts // keep capture alive
}

func TestRunCloneJSONRequiresFF(t *testing.T) {
	// --json without -ff must fail with JSON error on stderr
	args := []string{"dolly", "clone", "--json"}
	_ = captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch(args)
			if exit != 1 {
				t.Fatalf("dispatch exit = %d, want 1", exit)
			}
		})
		var errObj struct {
			OK      bool   `json:"ok"`
			Command string `json:"command"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &errObj); err != nil {
			t.Fatalf("stderr not valid JSON: %v\n%s", err, stderr)
		}
		if errObj.OK {
			t.Fatal("expected ok=false in error JSON")
		}
		if errObj.Command != "clone" {
			t.Fatalf("command = %q, want clone", errObj.Command)
		}
		if !strings.Contains(errObj.Error, "requires -ff") {
			t.Fatalf("error = %q, want 'requires -ff'", errObj.Error)
		}
	})
}

func TestParseCloneFlagsJSONWithoutFF(t *testing.T) {
	_, err := parseCloneFlags([]string{"--json"})
	if err == nil {
		t.Fatal("expected error for --json without -ff")
	}
	if !strings.Contains(err.Error(), "requires -ff") {
		t.Fatalf("error = %q, want 'requires -ff'", err.Error())
	}
}

func TestRunCloneJSONErrorWrap(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error {
		return errors.New("clone engine failure")
	}
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Sanitization.Enabled = true // avoid unsanitized stderr guardrail mixing with JSON error line
		return cfg, nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	args := []string{"dolly", "clone", "-ff", "--json"}
	_ = captureStdout(func() {
		stderr := captureStderr(func() {
			exit := dispatch(args)
			if exit != 1 {
				t.Fatalf("dispatch exit = %d, want 1", exit)
			}
		})
		var errObj struct {
			OK      bool   `json:"ok"`
			Command string `json:"command"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &errObj); err != nil {
			t.Fatalf("stderr not valid JSON: %v\n%s", err, stderr)
		}
		if errObj.OK {
			t.Fatal("expected ok=false in error JSON")
		}
		if errObj.Command != "clone" {
			t.Fatalf("command = %q, want clone", errObj.Command)
		}
		if !strings.Contains(errObj.Error, "clone engine failure") {
			t.Fatalf("error = %q, want 'clone engine failure'", errObj.Error)
		}
	})
}

func TestRunCloneJSONNilSchemas(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error { return nil }
	t.Cleanup(func() { cloneRun = origRun })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origDotEnvConn := cloneDotEnvConn
	cloneDotEnvConn = func(path string, names config.EnvVarNames) (connections.Connection, error) {
		return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}, nil
	}
	t.Cleanup(func() { cloneDotEnvConn = origDotEnvConn })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	args := []string{"-ff", "--json"}
	stdout := captureStdout(func() {
		if err := runClone(args); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})
	var result struct {
		Schemas []string `json:"schemas"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if result.Schemas == nil {
		t.Fatal("schemas must be empty array [], not null")
	}
}

func TestRunCloneExecuteStderrGuardrails(t *testing.T) {
	origRun := cloneRun
	cloneRun = func(context.Context, clone.Options) error { return nil }
	t.Cleanup(func() { cloneRun = origRun })

	tests := []struct {
		name       string
		cfg        func(*config.Config)
		flags      cloneFlags
		strategy   string
		targetURL  string
		wantSubstr []string
		wantAbsent []string
	}{
		{
			name: "unsanitized disabled sanitization",
			cfg: func(c *config.Config) {
				c.Sanitization.Enabled = false
			},
			wantSubstr: []string{"warning: clone will copy unsanitized"},
		},
		{
			name: "unsanitized template strategy",
			cfg: func(c *config.Config) {
				c.Sanitization.Enabled = true
			},
			strategy:   "template",
			wantSubstr: []string{"warning: clone will copy unsanitized"},
		},
		{
			name: "skip_create warning",
			cfg: func(c *config.Config) {
				c.Clone.SkipCreate = true
			},
			wantSubstr: []string{"warning: skip_create"},
		},
		{
			name: "replace yes target info",
			cfg: func(c *config.Config) {
				c.Clone.Replace = true
			},
			flags:      cloneFlags{Yes: true},
			targetURL:  "postgres://u:p@h/target_db",
			wantSubstr: []string{"info: target database: target_db"},
		},
		{
			name: "sanitized schema-replay no unsanitized warning",
			cfg: func(c *config.Config) {
				c.Sanitization.Enabled = true
			},
			wantAbsent: []string{"warning: clone will copy unsanitized"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			if tt.cfg != nil {
				tt.cfg(cfg)
			}
			strategy := tt.strategy
			if strategy == "" {
				strategy = "schema-replay"
			}
			stderr := captureStderr(func() {
				if err := runCloneExecute(context.Background(), tt.flags, cfg, "postgres://u:p@h/src", "clone_x", tt.targetURL, nil, strategy); err != nil {
					t.Fatalf("runCloneExecute: %v", err)
				}
			})
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(stderr, sub) {
					t.Fatalf("stderr = %q, want containing %q", stderr, sub)
				}
			}
			for _, sub := range tt.wantAbsent {
				if strings.Contains(stderr, sub) {
					t.Fatalf("stderr = %q, want absent %q", stderr, sub)
				}
			}
		})
	}
}
