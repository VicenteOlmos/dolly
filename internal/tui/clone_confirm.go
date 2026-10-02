package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/clonework"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

// cloneConfirmPreflight loads warnings shown in the clone confirm modal.
// A nil App field uses clonework.PreflightForConfirm.
type cloneConfirmPreflight func(context.Context, clonework.Params) ([]string, error)

func cloneWorkParams(a *App, schemas []string) clonework.Params {
	draft := a.clone
	return clonework.Params{
		SourceDSN:         a.conn.DSN(),
		CloneName:         draft.CloneName,
		TargetDSN:         draft.TargetDSN,
		Strategy:          draft.Strategy,
		Schemas:           schemas,
		IncludePrivileges: draft.IncludePrivileges,
		Replace:           draft.Replace,
		ReplaceSet:        draft.ReplaceSet,
		OnConflict:        draft.OnConflict,
		TargetDir:         draft.TargetDir,
		DumpDir:           draft.DumpDir,
		SkipCreate:        draft.SkipCreate,
		SkipCreateSet:     true,
	}
}

func cloneSchemaSourceLabel() string {
	if _, err := exec.LookPath("pg_dump"); err == nil {
		return "pg_dump"
	}
	return "catalog replay"
}

func formatCloneConfirmBody(targetDSN, strategy, schemaSource string, schemas []string, replacePolicy string, warnings []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Target: %s\n\n", connections.RedactMessage(targetDSN))
	fmt.Fprintf(&b, "Strategy: %s\n", strategy)
	switch strategy {
	case "template":
		b.WriteString("Scope: entire source database")
	case "physical-backup":
		b.WriteString("Scope: entire cluster")
	default:
		fmt.Fprintf(&b, "Schema: %s\n", schemaSource)
		fmt.Fprintf(&b, "Schemas: %s", strings.Join(schemas, ", "))
	}
	if replacePolicy != "" {
		fmt.Fprintf(&b, "\n\nThis will %s.", replacePolicy)
	}
	if len(warnings) > 0 {
		b.WriteString("\n\n")
		for i, w := range warnings {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(w)
		}
	}
	return b.String()
}

type clonePreflightResultMsg struct {
	gen      int
	schemas  []string
	warnings []string
	err      error
}

func (a *App) startClonePreflight(schemas []string) (tea.Model, tea.Cmd) {
	if a.clonePreflightPending {
		return a, nil
	}
	a.clonePreflightGen++
	gen := a.clonePreflightGen
	ctx, cancel := context.WithCancel(context.Background())
	a.clonePreflightCancel = cancel
	a.clonePreflightPending = true
	a.statusMsg = "Checking clone… · c/Esc cancel"
	params := cloneWorkParams(a, schemas)
	preflight := a.clonePreflight
	if preflight == nil {
		preflight = clonework.PreflightForConfirm
	}
	copied := append([]string(nil), schemas...)
	return a, func() tea.Msg {
		warnings, err := preflight(ctx, params)
		return clonePreflightResultMsg{gen: gen, schemas: copied, warnings: warnings, err: err}
	}
}

func (a *App) handleClonePreflightResult(msg clonePreflightResultMsg) (tea.Model, tea.Cmd) {
	if msg.gen != a.clonePreflightGen || !a.clonePreflightPending {
		return a, nil
	}
	a.clonePreflightPending = false
	a.clonePreflightCancel = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			a.statusMsg = truncateStatus(StyleMuted.Render("Clone check cancelled"), a.width)
			return a, nil
		}
		a.statusMsg = truncateStatus(StyleWarning.Render(redactUserError(msg.err)), a.width)
		return a, nil
	}
	return a.mountCloneConfirm(msg.schemas, msg.warnings)
}

func (a *App) cancelClonePreflight() {
	if a.clonePreflightCancel != nil {
		a.clonePreflightCancel()
		a.clonePreflightCancel = nil
	}
	a.clonePreflightPending = false
	a.clonePreflightGen++
	a.statusMsg = truncateStatus(StyleMuted.Render("Clone check cancelled"), a.width)
}

func (a *App) mountCloneConfirm(schemas, warnings []string) (tea.Model, tea.Cmd) {
	strategy := effectiveCloneStrategyForDraft(a.clone, a.cfg)
	replacePolicy := ""
	title := "Clone?"
	if effectiveCloneReplace(a.clone, a.cfg) {
		title = "Clone with replace?"
		replacePolicy = "truncate existing tables before clone"
	}
	body := formatCloneConfirmBody(a.clone.TargetDSN, strategy, cloneSchemaSourceLabel(), schemas, replacePolicy, warnings)
	a.mountCloneConfirmModal(title, body, nil)
	a.statusMsg = ""
	return a, nil
}
