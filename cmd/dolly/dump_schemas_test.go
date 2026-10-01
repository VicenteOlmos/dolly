package main

import (
	"context"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func stubDumpListSchemaNames(t *testing.T, names []string, err error) {
	t.Helper()
	orig := dumpListSchemaNames
	dumpListSchemaNames = func(ctx context.Context, dsn string) ([]string, error) {
		if err != nil {
			return nil, err
		}
		return append([]string(nil), names...), nil
	}
	t.Cleanup(func() { dumpListSchemaNames = orig })
}

func TestDumpSchemasFlagRejectsEmpty(t *testing.T) {
	var f dumpSchemasFlag
	if err := f.Set(""); err == nil {
		t.Fatal("expected error for empty --schemas")
	}
	if err := f.Set(" , "); err == nil {
		t.Fatal("expected error for blank-only --schemas")
	}
}

func TestDumpSchemasFlagParsesCommaSeparated(t *testing.T) {
	var f dumpSchemasFlag
	if err := f.Set("app, billing"); err != nil {
		t.Fatal(err)
	}
	if !f.set || len(f.values) != 2 || f.values[0] != "app" || f.values[1] != "billing" {
		t.Fatalf("values = %v", f.values)
	}
}

func TestNormalizeDumpSchemaListDedupes(t *testing.T) {
	got, err := normalizeDumpSchemaList([]string{"app", "app", " billing "})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeDumpSchemaListRejectsBlank(t *testing.T) {
	_, err := normalizeDumpSchemaList([]string{"app", ""})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveEffectiveDumpSchemasPrecedence(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	cfg.Dump.Schemas = []string{"cfg_a", "cfg_b"}
	stubDumpListSchemaNames(t, []string{"public", "app", "billing", "cfg_a", "cfg_b", "flag_only", "prompt", "profile_only"}, nil)

	tests := []struct {
		name       string
		cliSet     bool
		cli        []string
		profile    []string
		cfgSchemas []string
		want       []string
	}{
		{name: "cli wins", cliSet: true, cli: []string{"flag_only"}, profile: []string{"profile_only"}, want: []string{"flag_only"}},
		{name: "profile over config", cliSet: false, profile: []string{"profile_only"}, want: []string{"profile_only"}},
		{name: "config when no cli or profile", cliSet: false, profile: nil, want: []string{"cfg_a", "cfg_b"}},
		{name: "public when all absent", cliSet: false, profile: nil, cfgSchemas: nil, want: []string{"public"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCfg := config.DefaultConfig()
			if tt.name == "public when all absent" {
				testCfg.Dump.Schemas = nil
			} else {
				testCfg.Dump.Schemas = cfg.Dump.Schemas
			}
			got, err := resolveEffectiveDumpSchemas(ctx, "postgres://u:p@h/db", tt.cliSet, tt.cli, tt.profile, testCfg)
			if err != nil {
				t.Fatalf("resolveEffectiveDumpSchemas: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v want %v", got, tt.want)
				}
			}
		})
	}
}

func TestResolveEffectiveDumpSchemasUnknownSchema(t *testing.T) {
	stubDumpListSchemaNames(t, []string{"public", "app"}, nil)
	_, err := resolveEffectiveDumpSchemas(context.Background(), "postgres://u:p@h/db", true, []string{"missing"}, nil, config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "unknown schema") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseDumpFlagsSchemas(t *testing.T) {
	got, err := parseDumpFlags([]string{"--dsn", "postgres://h/db", "--output", "/tmp/out", "--schemas", "app, billing"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.SchemasSet || len(got.Schemas) != 2 || got.Schemas[0] != "app" || got.Schemas[1] != "billing" {
		t.Fatalf("schemas = %v set=%v", got.Schemas, got.SchemasSet)
	}
}

func TestResolveEffectiveExcludeDumpSchemasFlagOverridesConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.ExcludeSchemas = []string{"config_only"}
	got, err := resolveEffectiveExcludeDumpSchemas(true, []string{"flag_only"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "flag_only" {
		t.Fatalf("got %v", got)
	}
	got, err = resolveEffectiveExcludeDumpSchemas(false, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "config_only" {
		t.Fatalf("config exclude = %v", got)
	}
}

func TestApplyDumpSchemaExclusionsEmptyScope(t *testing.T) {
	_, err := applyDumpSchemaExclusions([]string{"public"}, []string{"public"})
	if err == nil || !strings.Contains(err.Error(), "empty after applying exclude schemas") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyDumpSchemaExclusionsRemovesSchemas(t *testing.T) {
	got, err := applyDumpSchemaExclusions([]string{"app", "public", "billing"}, []string{"billing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "app" || got[1] != "public" {
		t.Fatalf("got %v", got)
	}
}

func TestParseDumpFlagsExcludeSchema(t *testing.T) {
	got, err := parseDumpFlags([]string{
		"--dsn", "postgres://h/db",
		"--output", "/tmp/out",
		"--schemas", "app,public",
		"--exclude-schema", "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExcludeSchemasSet || len(got.ExcludeSchemas) != 1 || got.ExcludeSchemas[0] != "public" {
		t.Fatalf("exclude = %v set=%v", got.ExcludeSchemas, got.ExcludeSchemasSet)
	}
}

func TestParseDumpFlagsSchemasEmptyRejected(t *testing.T) {
	_, err := parseDumpFlags([]string{"--dsn", "postgres://h/db", "--output", "/tmp/out", "--schemas", ""})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "at least one schema") {
		t.Fatalf("err = %v", err)
	}
}
