package clone

import (
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

// SchemasFromOptions returns schema filters configured on clone options.
func SchemasFromOptions(opts Options) []string {
	return schemasFromCloneOpts(opts)
}

func schemasFromCloneOpts(opts Options) []string {
	if names := dump.InspectSchemas(opts.DumpOpts...); len(names) > 0 {
		return names
	}
	return restore.InspectSchemas(opts.RestoreOpts...)
}
