package tui

import (
	"os"
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func collectDumpResultSummary(dir string, runErr error) DumpResultSummary {
	summary := DumpResultSummary{
		OutputDir: dir,
		Outcome:   DumpOutcomeSuccess,
	}
	if runErr != nil {
		summary.Outcome = DumpOutcomeError
		summary.Error = connections.RedactMessage(runErr.Error())
	}

	meta, ok := dumpMetadataTables(dir)
	if !ok {
		summary.MetadataMissing = true
	} else {
		summary.Tables = meta
		summary.TableCount = len(meta)
		for _, tbl := range meta {
			if tbl.RowEstimate != nil {
				if summary.TotalRowEstimate == nil {
					zero := int64(0)
					summary.TotalRowEstimate = &zero
				}
				*summary.TotalRowEstimate += *tbl.RowEstimate
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return summary
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tmp") {
			summary.HasIncomplete = true
			continue
		}
		if name == "metadata.json" || strings.HasSuffix(name, ".ndjson") {
			summary.Files = append(summary.Files, name)
		}
	}
	sort.Strings(summary.Files)
	return summary
}
