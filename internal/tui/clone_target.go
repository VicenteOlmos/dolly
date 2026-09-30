package tui

import (
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/config"
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

func effectiveCloneStrategyForDraft(draft CloneDraft, cfg *config.Config) string {
	strategy := strings.TrimSpace(draft.Strategy)
	if strategy == "" && cfg != nil {
		strategy = cfg.Clone.Strategy
	}
	return effectiveCloneStrategy(strategy)
}

func effectiveCloneTargetDir(draft CloneDraft, cfg *config.Config) string {
	if trimmed := strings.TrimSpace(draft.TargetDir); trimmed != "" {
		return trimmed
	}
	if cfg != nil {
		return strings.TrimSpace(cfg.Clone.TargetDir)
	}
	return ""
}

// cloneNeedsUnsanitizedWarning mirrors the CLI guardrail: warn when clone will not
// apply row sanitization (disabled config or unsupported strategy).
func cloneNeedsUnsanitizedWarning(strategy string, sanitizationEnabled bool) bool {
	strategy = effectiveCloneStrategy(strategy)
	return !sanitizationEnabled ||
		strategy == "template" ||
		strategy == "physical-backup"
}

func formatCloneUnsanitizedWarning(strategy string, sanitizationEnabled bool) string {
	strategy = effectiveCloneStrategy(strategy)
	return fmt.Sprintf("warning: clone will copy unsanitized data (strategy=%s, sanitization=%v)", strategy, sanitizationEnabled)
}

func cloneSanitizationStrategyBlock(draft CloneDraft, cfg *config.Config) (bool, string) {
	if cfg == nil || !cfg.Sanitization.Enabled {
		return false, ""
	}
	strategy := effectiveCloneStrategyForDraft(draft, cfg)
	if strategy != "template" && strategy != "physical-backup" {
		return false, ""
	}
	return true, fmt.Sprintf("Sanitization cannot rewrite %s clones; use schema-replay or logical-stream, or disable sanitization in config", strategy)
}
