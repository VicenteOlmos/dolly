package dump

import (
	"strings"
	"testing"

	appconfig "github.com/VicenteOlmos/dolly/internal/config"
)

func TestNormalizeSchemaListDedupes(t *testing.T) {
	got, err := NormalizeSchemaList([]string{"app", "app", " billing "})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "app" || got[1] != "billing" {
		t.Fatalf("got %v", got)
	}
}

func TestApplySchemaExclusionsEmptyScope(t *testing.T) {
	_, err := ApplySchemaExclusions([]string{"public"}, []string{"public"})
	if err == nil || !strings.Contains(err.Error(), "empty after applying exclude schemas") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveEffectiveExcludeSchemasUsesConfig(t *testing.T) {
	cfg := appconfig.DefaultConfig()
	cfg.Dump.ExcludeSchemas = []string{"config_only"}
	got, err := ResolveEffectiveExcludeSchemas(false, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "config_only" {
		t.Fatalf("got %v", got)
	}
	got, err = ResolveEffectiveExcludeSchemas(true, []string{"flag_only"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "flag_only" {
		t.Fatalf("got %v", got)
	}
}
