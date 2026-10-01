package dump

import (
	"errors"
	"strings"

	appconfig "github.com/VicenteOlmos/dolly/internal/config"
)

// NormalizeSchemaList deduplicates schema names and rejects blanks.
func NormalizeSchemaList(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("schema list is empty")
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, name := range raw {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("schema name cannot be empty")
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, errors.New("schema list is empty")
	}
	return out, nil
}

// ResolveEffectiveExcludeSchemas returns dump exclude-schema names from CLI
// overrides or config dump.exclude_schemas when excludeSet is false.
func ResolveEffectiveExcludeSchemas(excludeSet bool, excludeValues []string, cfg *appconfig.Config) ([]string, error) {
	var raw []string
	if excludeSet {
		raw = excludeValues
	} else if cfg != nil && len(cfg.Dump.ExcludeSchemas) > 0 {
		raw = cfg.Dump.ExcludeSchemas
	}
	if len(raw) == 0 {
		return nil, nil
	}
	return NormalizeSchemaList(raw)
}

// ApplySchemaExclusions removes exclude names from schemas.
func ApplySchemaExclusions(schemas, exclude []string) ([]string, error) {
	if len(exclude) == 0 {
		return schemas, nil
	}
	remove := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		remove[name] = struct{}{}
	}
	out := make([]string, 0, len(schemas))
	for _, name := range schemas {
		if _, skip := remove[name]; skip {
			continue
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, errors.New("dump schema scope is empty after applying exclude schemas")
	}
	return out, nil
}
