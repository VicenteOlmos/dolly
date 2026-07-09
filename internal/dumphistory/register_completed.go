package dumphistory

import (
	"github.com/VicenteOlmos/dolly/internal/dump"
)

// RegisterCompletedDump reads metadata from outputDir and records it in the store.
func RegisterCompletedDump(store Store, baseDir string, seq int, outputDir, sourceDB string, schemas []string) error {
	if store == nil {
		return nil
	}
	meta, err := dump.ReadMetadata(outputDir)
	if err != nil {
		return err
	}
	rec := RecordFromMetadata(baseDir, seq, outputDir, sourceDB, schemas, meta)
	return store.Register(rec)
}
