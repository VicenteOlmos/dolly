package restore

import "github.com/VicenteOlmos/dolly/internal/dump"

// schemasFromMetadata derives the PostgreSQL schema filter used for target introspection.
func schemasFromMetadata(meta dump.Metadata) []string {
	if meta.Schema != "" && meta.Schema != "multi" {
		return []string{meta.Schema}
	}
	seen := make(map[string]struct{})
	var out []string
	for _, t := range meta.Tables {
		if t.Schema == "" {
			continue
		}
		if _, ok := seen[t.Schema]; ok {
			continue
		}
		seen[t.Schema] = struct{}{}
		out = append(out, t.Schema)
	}
	return out
}
