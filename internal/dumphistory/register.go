package dumphistory

import (
	"time"

	"github.com/VicenteOlmos/dolly/internal/dump"
)

// RecordFromMetadata builds a history record after a successful dump.
func RecordFromMetadata(baseDir string, seq int, outputDir, sourceDB string, schemas []string, meta dump.Metadata) Record {
	var rowEst int64
	if meta.Provenance != nil && meta.Provenance.TotalRowEstimate > 0 {
		rowEst = meta.Provenance.TotalRowEstimate
	} else {
		for _, t := range meta.Tables {
			if t.RowCount != nil {
				rowEst += *t.RowCount
			}
		}
	}
	tableCount := len(meta.Tables)
	if meta.Provenance != nil && meta.Provenance.TableCount > 0 {
		tableCount = meta.Provenance.TableCount
	}
	rec := Record{
		Seq:            seq,
		BaseDir:        baseDir,
		Path:           outputDir,
		CreatedAt:      time.Now().UTC(),
		SourceDatabase: sourceDB,
		Schemas:        append([]string(nil), schemas...),
		SchemaLabel:    meta.Schema,
		TableCount:     tableCount,
		RowEstimate:    rowEst,
	}
	if meta.Provenance != nil {
		rec.ServerVersion = meta.Provenance.ServerVersion
		rec.DollyVersion = meta.Provenance.DollyVersion
	}
	return rec
}
