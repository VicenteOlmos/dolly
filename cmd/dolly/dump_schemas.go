package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/config"
)

// dumpListSchemaNames lists schema names on the source for dump validation.
var dumpListSchemaNames = func(ctx context.Context, dsn string) ([]string, error) {
	return cloneListSchemaNames(ctx, dsn)
}

type dumpSchemasFlag struct {
	set    bool
	values []string
}

func (f *dumpSchemasFlag) String() string {
	return strings.Join(f.values, ",")
}

func (f *dumpSchemasFlag) Set(s string) error {
	f.set = true
	parsed := parseCommaSeparatedSchemas(s)
	if len(parsed) == 0 {
		return errors.New("--schemas requires at least one schema name")
	}
	f.values = parsed
	return nil
}

func normalizeDumpSchemaList(raw []string) ([]string, error) {
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

func validateDumpSchemasInCatalog(ctx context.Context, dsn string, schemas []string) error {
	catalog, err := dumpListSchemaNames(ctx, dsn)
	if err != nil {
		return fmt.Errorf("list source schemas: %w", err)
	}
	allowed := make(map[string]struct{}, len(catalog))
	for _, name := range catalog {
		allowed[name] = struct{}{}
	}
	for _, name := range schemas {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("unknown schema %q (source has: %s)", name, strings.Join(catalog, ", "))
		}
	}
	return nil
}

func resolveEffectiveDumpSchemas(ctx context.Context, dsn string, cliSet bool, cliSchemas []string, profileSchemas []string, cfg *config.Config) ([]string, error) {
	var raw []string
	validate := false
	if cliSet {
		raw = cliSchemas
		validate = true
	} else if len(profileSchemas) > 0 {
		raw = profileSchemas
		validate = true
	} else if cfg != nil && len(cfg.Dump.Schemas) > 0 {
		raw = cfg.Dump.Schemas
		validate = true
	} else {
		return []string{"public"}, nil
	}

	normalized, err := normalizeDumpSchemaList(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid schemas: %w", err)
	}
	if validate {
		if err := validateDumpSchemasInCatalog(ctx, dsn, normalized); err != nil {
			return nil, err
		}
	}
	return normalized, nil
}
