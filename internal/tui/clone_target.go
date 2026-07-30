package tui

import (
	"fmt"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

// resolveCloneDraftTargetDSN sets draft.TargetDSN from the active target source.
// Manual keeps the user-typed DSN; Current and Saved always refresh from live inputs.
func resolveCloneDraftTargetDSN(d *CloneDraft, connDSN func() string, store connections.ConnectionStore) {
	if d == nil {
		return
	}
	switch d.TargetSource {
	case TargetSourceCurrent:
		if connDSN != nil {
			d.TargetDSN = connDSN()
		}
	case TargetSourceSaved:
		if d.TargetProfileName == "" && store != nil {
			profiles, err := store.List()
			if err == nil && len(profiles) > 0 {
				d.TargetProfileName = profiles[0].Name
				d.TargetDSN = profileDSN(profiles[0])
			}
			return
		}
		if d.TargetProfileName != "" && store != nil {
			prof, err := store.Get(d.TargetProfileName)
			if err == nil {
				d.TargetDSN = profileDSN(prof)
			}
		}
	case TargetSourceManual:
		// Keep whatever the user typed.
	}
}

func effectiveCloneStrategy(strategy string) string {
	if strategy == "" {
		return "schema-replay"
	}
	return strategy
}

// cloneNeedsUnsanitizedWarning mirrors the CLI guardrail: warn when clone will not
// apply row sanitization (disabled config or unsupported strategy).
func cloneNeedsUnsanitizedWarning(strategy string, sanitizationEnabled bool) bool {
	strategy = effectiveCloneStrategy(strategy)
	return !sanitizationEnabled ||
		strategy == "template" ||
		strategy == "logical-stream" ||
		strategy == "physical-backup"
}

func formatCloneUnsanitizedWarning(strategy string, sanitizationEnabled bool) string {
	strategy = effectiveCloneStrategy(strategy)
	return fmt.Sprintf("warning: clone will copy unsanitized data (strategy=%s, sanitization=%v)", strategy, sanitizationEnabled)
}
