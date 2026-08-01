package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func testCloneConn() connections.Connection {
	return connections.Connection{Name: "local", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}
}

func TestMain(m *testing.M) {
	orig := cloneLoadDotEnv
	cloneLoadDotEnv = func(string, config.EnvVarNames) (string, error) { return testCloneConn().DSN(), nil }
	code := m.Run()
	cloneLoadDotEnv = orig
	os.Exit(code)
}

func stubCloneLoadDotEnv(t *testing.T, fn func(string, config.EnvVarNames) (string, error)) {
	t.Helper()
	orig := cloneLoadDotEnv
	cloneLoadDotEnv = fn
	t.Cleanup(func() { cloneLoadDotEnv = orig })
}

func stubCloneLoadDotEnvConn(t *testing.T, conn connections.Connection) {
	t.Helper()
	stubCloneLoadDotEnv(t, func(string, config.EnvVarNames) (string, error) { return conn.DSN(), nil })
}

func useRealLoadDotEnv(t *testing.T) {
	t.Helper()
	stubCloneLoadDotEnv(t, config.LoadDotEnv)
}

func assertNotContainsAny(t *testing.T, text string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(text, sub) {
			t.Fatalf("unexpected %q in %q", sub, text)
		}
	}
}

func assertContainsAll(t *testing.T, text string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(text, sub) {
			t.Fatalf("%q missing %q", text, sub)
		}
	}
}

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
	stubCloneListSchemaNames(t, nil, nil)

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
		{name: "public default when all empty", flag: nil, fromPrompt: nil, want: []string{"public"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCfg := config.DefaultConfig()
			testCfg.Clone.Schemas = cfg.Clone.Schemas
			if tt.name == "prompt when no flag or config" || tt.name == "public default when all empty" {
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

func TestRunCloneFFSchemasOverrideSavedProfile(t *testing.T) {
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
		User: "u", Password: "p", Schemas: []string{"saved_only"},
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

	err = runClone([]string{"-ff", "--connection", "prod", "--schemas", "cli_only"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedOpts.SourceDSN, "h-a:5432/db_a") {
		t.Fatalf("SourceDSN = %q", capturedOpts.SourceDSN)
	}
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 1 || got[0] != "cli_only" {
		t.Fatalf("dump schemas = %v, want [cli_only] (CLI flag overrides saved profile)", got)
	}
	rs := restore.InspectSchemas(capturedOpts.RestoreOpts...)
	if len(rs) != 1 || rs[0] != "cli_only" {
		t.Fatalf("restore schemas = %v, want [cli_only]", rs)
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

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	err := runClone([]string{"-ff"})
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	// Default when no flag, no config, no profile: public
	got := dump.InspectSchemas(capturedOpts.DumpOpts...)
	if len(got) != 1 || got[0] != "public" {
		t.Fatalf("dump schemas = %v, want [public] (default schema)", got)
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
	if len(capturedOpts.RestoreOpts) != 1 {
		t.Fatalf("expected 1 RestoreOpts (default schemas), got %d", len(capturedOpts.RestoreOpts))
	}
	if got := dump.InspectSchemas(capturedOpts.DumpOpts...); len(got) != 1 || got[0] != "public" {
		t.Fatalf("dump schemas = %v, want [public]", got)
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
	if len(capturedOpts.RestoreOpts) != 3 {
		t.Fatalf("expected 3 RestoreOpts (schemas + replace + skip), got %d", len(capturedOpts.RestoreOpts))
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

	stubCloneLoadDotEnv(t, func(string, config.EnvVarNames) (string, error) {
		return "", config.ErrSourceDSNNotFound
	})

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

func stubCloneFFHarnessCore(t *testing.T) {
	t.Helper()
	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) { return config.DefaultConfig(), nil }
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })
	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })
}

func stubCloneFFHarness(t *testing.T) {
	t.Helper()
	stubCloneListSchemaNames(t, nil, nil)
	stubCloneFFHarnessCore(t)
}

type cloneSeamCalls struct{ run, list bool }

func trackCloneSeams(t *testing.T) *cloneSeamCalls {
	t.Helper()
	c := &cloneSeamCalls{}
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error { c.run = true; return nil }
	t.Cleanup(func() { cloneRun = origRun })
	origList := cloneListSchemaNames
	cloneListSchemaNames = func(ctx context.Context, dsn string) ([]string, error) { c.list = true; return nil, nil }
	t.Cleanup(func() { cloneListSchemaNames = origList })
	return c
}

func captureCloneRun(t *testing.T) *clone.Options {
	t.Helper()
	var captured clone.Options
	origRun := cloneRun
	cloneRun = func(ctx context.Context, opts clone.Options) error { captured = opts; return nil }
	t.Cleanup(func() { cloneRun = origRun })
	return &captured
}

func TestRunCloneFFDotenvSource(t *testing.T) {
	rawShellURL := "postgres://u:secret@h-a:5432/db_a?sslmode=disable&connect_timeout=3&application_name=clone"
	for _, tc := range []struct {
		name      string
		args      []string
		setup     func(t *testing.T) *clone.Options
		wantErr   string
		noSideFX  bool
		checkLeak []string
		wants     []string
		rejects   []string
	}{
		{
			name: "shell_url",
			setup: func(t *testing.T) *clone.Options {
				stubCloneFFHarness(t)
				captured := captureCloneRun(t)
				t.Setenv("DB_URL", rawShellURL)
				for _, key := range []string{"DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"} {
					t.Setenv(key, "")
				}
				useRealLoadDotEnv(t)
				return captured
			},
			checkLeak: []string{"secret", rawShellURL},
			wants:     []string{"sslmode=disable", "connect_timeout=3", "application_name=clone", "statement_timeout=5min"},
			rejects:   []string{"sslmode=verify-full"},
		},
		{
			name: "custom_file",
			setup: func(t *testing.T) *clone.Options {
				stubCloneFFHarness(t)
				captured := captureCloneRun(t)
				dir := t.TempDir()
				envPath := filepath.Join(dir, "custom.env")
				rawURL := "postgres://u:secret@file-host:5433/filedb?sslmode=disable&channel_binding=prefer"
				if err := os.WriteFile(envPath, []byte("CUSTOM_URL="+rawURL+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				origLoadConfig := cloneLoadConfig
				cloneLoadConfig = func(path string) (*config.Config, error) {
					cfg := config.DefaultConfig()
					cfg.Env.Path = envPath
					cfg.Env.URLVar = "CUSTOM_URL"
					return cfg, nil
				}
				t.Cleanup(func() { cloneLoadConfig = origLoadConfig })
				useRealLoadDotEnv(t)
				return captured
			},
			wants: []string{"sslmode=disable", "channel_binding=prefer", "statement_timeout=5min", "file-host:5433/filedb"},
		},
		{
			name: "component_fallback",
			setup: func(t *testing.T) *clone.Options {
				stubCloneFFHarness(t)
				captured := captureCloneRun(t)
				t.Setenv("DB_URL", "")
				t.Setenv("DB_HOST", "comp-host")
				t.Setenv("DB_PORT", "5434")
				t.Setenv("DB_NAME", "compdb")
				t.Setenv("DB_USER", "compuser")
				t.Setenv("DB_PASSWORD", "comppass")
				useRealLoadDotEnv(t)
				return captured
			},
			wants:   []string{"comp-host:5434/compdb"},
			rejects: []string{"sslmode=verify-full"},
		},
		{
			name: "missing",
			setup: func(t *testing.T) *clone.Options {
				stubCloneFFHarnessCore(t)
				for _, key := range []string{"DB_URL", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"} {
					t.Setenv(key, "")
				}
				useRealLoadDotEnv(t)
				return nil
			},
			wantErr:  "resolve source DSN",
			noSideFX: true,
		},
		{
			name: "invalid_no_db",
			setup: func(t *testing.T) *clone.Options {
				stubCloneFFHarnessCore(t)
				stubCloneLoadDotEnv(t, func(string, config.EnvVarNames) (string, error) { return "postgres://u:p@h-a:5432", nil })
				return nil
			},
			wantErr:  "parse source DB name",
			noSideFX: true,
		},
		{
			name: "malformed_url",
			setup: func(t *testing.T) *clone.Options {
				stubCloneFFHarnessCore(t)
				stubCloneLoadDotEnv(t, func(string, config.EnvVarNames) (string, error) { return "://bad-url", nil })
				return nil
			},
			wantErr:  "parse source DB name",
			noSideFX: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls *cloneSeamCalls
			if tc.noSideFX {
				calls = trackCloneSeams(t)
			}
			captured := tc.setup(t)
			var err error
			if len(tc.checkLeak) > 0 {
				stdout := captureStdout(func() {
					stderr := captureStderr(func() { err = runClone([]string{"-ff"}) })
					assertNotContainsAny(t, stderr, tc.checkLeak...)
				})
				assertNotContainsAny(t, stdout, tc.checkLeak...)
			} else {
				err = runClone(append([]string{"-ff"}, tc.args...))
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if calls != nil && (calls.run || calls.list) {
					t.Fatalf("side effects: run=%v list=%v", calls.run, calls.list)
				}
				return
			}
			if err != nil {
				t.Fatalf("runClone: %v", err)
			}
			if captured != nil {
				assertContainsAll(t, captured.SourceDSN, tc.wants...)
				for _, bad := range tc.rejects {
					if strings.Contains(captured.SourceDSN, bad) {
						t.Fatalf("SourceDSN = %q, reject %q", captured.SourceDSN, bad)
					}
				}
			}
		})
	}
}

func TestRunCloneFFConnectionBypassesDotenv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	store, err := connections.OpenStore(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(connections.Connection{Name: "prod", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p", Schemas: []string{"app"}}); err != nil {
		t.Fatal(err)
	}
	captured := captureCloneRun(t)
	stubCloneListSchemaNames(t, nil, nil)
	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		c := config.DefaultConfig()
		c.SaveConnections = true
		return c, nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })
	envCalled := false
	stubCloneLoadDotEnv(t, func(string, config.EnvVarNames) (string, error) {
		envCalled = true
		return "postgres://dotenv:wrong@ignored/db", nil
	})
	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })
	t.Chdir(dir)
	if err := runClone([]string{"-ff", "--connection", "prod"}); err != nil {
		t.Fatalf("runClone: %v", err)
	}
	if envCalled {
		t.Fatal("cloneLoadDotEnv should not run when --connection is set")
	}
	if !strings.Contains(captured.SourceDSN, "h-a:5432/db_a") {
		t.Fatalf("SourceDSN = %q", captured.SourceDSN)
	}
}

const broadDotenvWarning = "warning: dotenv permissions allow group or other access; continuing without changing the file"

type cwdDotenvSnap struct {
	bytes []byte
	mode  os.FileMode
	mtime time.Time
}

func snapshotCwdDotenv(t *testing.T, path string) cwdDotenvSnap {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return cwdDotenvSnap{bytes: b, mode: info.Mode(), mtime: info.ModTime()}
}

func assertCwdDotenvUnchanged(t *testing.T, path string, before cwdDotenvSnap) {
	t.Helper()
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.bytes, afterBytes) {
		t.Fatal(".env bytes changed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != before.mode.Perm() {
		t.Fatalf("mode changed %o -> %o", before.mode.Perm(), info.Mode().Perm())
	}
	if !info.ModTime().Equal(before.mtime) {
		t.Fatal(".env mtime changed")
	}
}

func TestCloneNoPermissionOptOutFlag(t *testing.T) {
	out := captureStderr(printCloneUsage)
	for _, forbidden := range []string{
		"--skip-permission",
		"--no-tighten",
		"--ignore-permission",
		"permission-enforcement",
		"skip permission",
	} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Fatalf("clone usage must not expose permission opt-out %q:\n%s", forbidden, out)
		}
	}
	_, err := parseCloneFlags([]string{"--skip-permission"})
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("unexpected parse result for --skip-permission: %v", err)
	}
}

func TestRunCloneFFBroadCwdDotenvNonMutating(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit advisory not applicable on Windows")
	}

	dir := t.TempDir()
	t.Chdir(dir)

	rawURL := "postgres://dotenv-user:dotenv-secret@h-a:5432/db_a?sslmode=disable"
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("DB_URL="+rawURL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotCwdDotenv(t, envPath)

	for _, key := range []string{"DB_URL", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"} {
		t.Setenv(key, "")
	}

	stubCloneFFHarness(t)
	captured := captureCloneRun(t)
	useRealLoadDotEnv(t)
	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.Env.Path = ".env"
		return cfg, nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	leaks := []string{"dotenv-secret", rawURL, dir, envPath, ".env"}
	var stderr string
	stdout := captureStdout(func() {
		stderr = captureStderr(func() {
			if err := runClone([]string{"-ff"}); err != nil {
				t.Fatalf("runClone: %v", err)
			}
		})
	})
	assertNotContainsAny(t, stderr, leaks...)
	assertNotContainsAny(t, stdout, leaks...)

	if n := strings.Count(stderr, broadDotenvWarning); n != 1 {
		t.Fatalf("want exactly one broad dotenv warning, got %d in %q", n, stderr)
	}
	assertContainsAll(t, captured.SourceDSN, "sslmode=disable", "h-a:5432/db_a", "statement_timeout=5min", "dotenv-user")
	assertCwdDotenvUnchanged(t, envPath, before)
}

// --- Phase 1 RED tests: clone signal context ordering and cancellation ---

func TestRunCloneInteractiveSignalContextAfterPrompt(t *testing.T) {
	stubCloneListSchemaNames(t, nil, nil)

	var order []string

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return true }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, _ *config.SavedSourcePicker) (config.PromptResult, error) {
		order = append(order, "prompt")
		return config.PromptResult{
			SourceDSN:     "postgres://u:p@h-a:5432/db_a?channel_binding=require&sslmode=verify-full",
			SourceSchemas: []string{"public"},
			CloneName:     "db_clone_x",
			TargetURL:     "postgres://u:p@h-b:5432/db_tgt",
			Strategy:      "template",
		}, nil
	}
	t.Cleanup(func() { clonePromptSource = origPrompt })

	origSignal := cloneSignalContext
	cloneSignalContext = func() (context.Context, context.CancelFunc) {
		order = append(order, "signal")
		return context.Background(), func() {}
	}
	t.Cleanup(func() { cloneSignalContext = origSignal })

	origRun := cloneRun
	cloneRun = func(runCtx context.Context, opts clone.Options) error {
		order = append(order, "run")
		return nil
	}
	t.Cleanup(func() { cloneRun = origRun })

	err := runClone([]string{})
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	if len(order) != 3 || order[0] != "prompt" || order[1] != "signal" || order[2] != "run" {
		t.Fatalf("order = %v, want [prompt signal run]", order)
	}
}

func TestRunCloneConnectionSignalContextCancellable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	store, err := connections.OpenStore(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(connections.Connection{Name: "prod", Host: "h-a", Port: "5432", Database: "db_a", User: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}

	stubCloneListSchemaNames(t, nil, nil)

	origLoadConfig := cloneLoadConfig
	cloneLoadConfig = func(path string) (*config.Config, error) {
		c := config.DefaultConfig()
		c.SaveConnections = true
		return c, nil
	}
	t.Cleanup(func() { cloneLoadConfig = origLoadConfig })

	origIsTerminal := cloneIsTerminal
	cloneIsTerminal = func() bool { return false }
	t.Cleanup(func() { cloneIsTerminal = origIsTerminal })

	promptCalled := false
	origPrompt := clonePromptSource
	clonePromptSource = func(r io.Reader, w io.Writer, defaults config.PromptDefaults, _ *config.SavedSourcePicker) (config.PromptResult, error) {
		promptCalled = true
		return config.PromptResult{}, nil
	}
	t.Cleanup(func() { clonePromptSource = origPrompt })

	cancelledCtx, cancelFn := context.WithCancel(context.Background())
	cancelFn()
	origSignal := cloneSignalContext
	cloneSignalContext = func() (context.Context, context.CancelFunc) {
		return cancelledCtx, func() {}
	}
	t.Cleanup(func() { cloneSignalContext = origSignal })

	origRun := cloneRun
	cloneRun = func(runCtx context.Context, opts clone.Options) error {
		return runCtx.Err()
	}
	t.Cleanup(func() { cloneRun = origRun })

	t.Chdir(dir)
	err = runClone([]string{"-ff", "--connection", "prod"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if promptCalled {
		t.Fatal("clonePromptSource should not be called for --connection path")
	}
}

func TestRunCloneFFSignalContextCancellable(t *testing.T) {
	stubCloneFFHarness(t)

	cancelledCtx, cancelFn := context.WithCancel(context.Background())
	cancelFn()
	origSignal := cloneSignalContext
	cloneSignalContext = func() (context.Context, context.CancelFunc) {
		return cancelledCtx, func() {}
	}
	t.Cleanup(func() { cloneSignalContext = origSignal })

	origRun := cloneRun
	cloneRun = func(runCtx context.Context, opts clone.Options) error {
		return runCtx.Err()
	}
	t.Cleanup(func() { cloneRun = origRun })

	err := runClone([]string{"-ff"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRunCloneFFJSONCancelEnvelope(t *testing.T) {
	stubCloneFFHarness(t)

	cancelledCtx, cancelFn := context.WithCancel(context.Background())
	cancelFn()
	origSignal := cloneSignalContext
	cloneSignalContext = func() (context.Context, context.CancelFunc) {
		return cancelledCtx, func() {}
	}
	t.Cleanup(func() { cloneSignalContext = origSignal })

	origRun := cloneRun
	cloneRun = func(runCtx context.Context, opts clone.Options) error {
		return runCtx.Err()
	}
	t.Cleanup(func() { cloneRun = origRun })

	var err error
	stderr := captureStderr(func() {
		err = runClone([]string{"-ff", "--json"})
	})
	if !errors.Is(err, errJSONHandled) {
		t.Fatalf("err = %v, want errJSONHandled", err)
	}
	if !strings.Contains(stderr, `"ok": false`) || !strings.Contains(stderr, `"command": "clone"`) {
		t.Fatalf("stderr missing JSON error envelope:\n%s", stderr)
	}
}
