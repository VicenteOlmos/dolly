package tui

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func (a *App) handleCloneRequested() (tea.Model, tea.Cmd) {
	if a.db == nil {
		a.statusMsg = truncateStatus(StyleWarning.Render("Connect first (screen 1)"), a.width)
		return a, nil
	}
	schemas := a.clone.SchemaPicker.SelectedNames()
	if len(schemas) == 0 {
		a.statusMsg = truncateStatus(StyleWarning.Render("Select schemas on clone screen"), a.width)
		return a, nil
	}

	// Resolve TargetDSN from TargetSource.
	a.resolveCloneTargetDSN()

	if a.clone.TargetDSN == "" {
		a.statusMsg = truncateStatus(StyleWarning.Render("Set target DSN"), a.width)
		return a, nil
	}
	if a.cloneStatus == CloneStatusRunning {
		return a, nil
	}

	// Gate on analyze when enabled and not yet completed.
	if a.clone.AnalyzeEnabled && a.clone.AnalyzeState.Result == nil {
		if a.clone.AnalyzeState.Loading {
			return a, nil // already running
		}
		// Start analyze.
		a.clone.AnalyzeState.Loading = true
		a.clone.AnalyzeState.Err = ""
		sourceDB := a.conn.Database
		nameTpl := ""
		if a.cfg != nil {
			nameTpl = a.cfg.Clone.NameTemplate
		}
		cmd, cancel := startAnalyzeCmd(a.db, sourceDB, nameTpl, schemas)
		a.clone.AnalyzeState.Cancel = cancel
		a.statusMsg = "Analyzing… · c/Esc cancel"
		return a, cmd
	}

	if needs, policy := a.cloneNeedsConfirm(); needs {
		body := fmt.Sprintf("Target: %s\n\nThis will %s.", connections.RedactMessage(a.clone.TargetDSN), policy)
		a.mountCloneConfirmModal("Clone with replace?", body, nil)
		return a, nil
	}

	return a.startCloneExecution(schemas)
}

func (a *App) cloneNeedsConfirm() (bool, string) {
	if a.cfg == nil || !a.cfg.Clone.Replace {
		return false, ""
	}
	return true, "truncate existing tables before clone"
}

func (a *App) handleCloneProceed() (tea.Model, tea.Cmd) {
	if a.cloneStatus == CloneStatusRunning {
		return a, nil
	}
	schemas := a.clone.SchemaPicker.SelectedNames()
	if len(schemas) == 0 {
		return a, nil
	}
	return a.startCloneExecution(schemas)
}

func (a *App) startCloneExecution(schemas []string) (tea.Model, tea.Cmd) {
	a.clearCloneResult()
	a.appendCloneUnsanitizedWarningIfNeeded()
	a.cloneStatus = CloneStatusRunning
	a.cloneError = ""
	if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
		cs.resetLogScroll()
	}
	a.statusMsg = "Cloning…"
	a.persistProfileSchemas(schemas)
	draft := a.clone
	draft.SourceDSN = a.conn.DSN()
	ctx := context.Background()
	cmd, ch, cancel := startCloneCmd(a.cloneRunner, ctx, draft, schemas)
	a.cloneCh = ch
	a.cloneCancel = cancel
	return a, cmd
}

// resolveCloneTargetDSN sets TargetDSN based on TargetSource.
func (a *App) resolveCloneTargetDSN() {
	resolveCloneDraftTargetDSN(&a.clone, a.conn.DSN, a.connStore)
}

func (a *App) appendCloneUnsanitizedWarningIfNeeded() {
	if a.cfg == nil {
		return
	}
	if !cloneNeedsUnsanitizedWarning(a.clone.Strategy, a.cfg.Sanitization.Enabled) {
		return
	}
	warn := connections.RedactMessage(formatCloneUnsanitizedWarning(a.clone.Strategy, a.cfg.Sanitization.Enabled))
	appendCloneLog(&a.cloneLog, warn)
}

func (a *App) handleCloneProgress(msg cloneProgressMsg) (tea.Model, tea.Cmd) {
	appendCloneLog(&a.cloneLog, msg.line)
	a.cloneProgress = &msg.ev
	if a.cloneCh == nil {
		return a, nil
	}
	return a, waitCloneCmd(a.cloneCh)
}

func (a *App) handleCloneResult(msg cloneResultMsg) (tea.Model, tea.Cmd) {
	a.cloneCancel = nil
	a.cloneCh = nil
	a.cloneProgress = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			a.cloneStatus = CloneStatusIdle
			a.statusMsg = truncateStatus(StyleMuted.Render("Clone cancelled"), a.width)
			appendCloneLog(&a.cloneLog, "cancelled")
			return a, nil
		}
		a.cloneStatus = CloneStatusComplete
		safeErr := redactUserError(msg.err)
		a.cloneError = safeErr
		a.statusMsg = truncateStatus(StyleWarning.Render("Clone failed: "+safeErr), a.width)
		appendCloneLog(&a.cloneLog, "error: "+safeErr)
		return a, nil
	}
	a.cloneStatus = CloneStatusComplete
	a.cloneError = ""
	a.statusMsg = truncateStatus(StyleBase.Render("Clone complete"), a.width)
	appendCloneLog(&a.cloneLog, "clone complete")
	return a, nil
}

func (a *App) handleAnalyzeResult(msg analyzeResultMsg) (tea.Model, tea.Cmd) {
	a.clone.AnalyzeState.Loading = false
	a.clone.AnalyzeState.Cancel = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			a.clone.AnalyzeState.Err = ""
			a.statusMsg = truncateStatus(StyleMuted.Render("Analyze cancelled"), a.width)
			return a, nil
		}
		safeErr := redactUserError(msg.err)
		a.clone.AnalyzeState.Err = safeErr
		a.statusMsg = truncateStatus(StyleWarning.Render("Analyze failed: "+safeErr), a.width)
		return a, nil
	}
	a.clone.AnalyzeState.Result = &msg.result
	a.clone.AnalyzeState.Err = ""
	// Update clone name with the next free name from analyze.
	if msg.result.NextCloneName != "" {
		a.clone.CloneName = msg.result.NextCloneName
	}
	a.mountAnalyzeResultModal(msg.result)
	a.statusMsg = ""
	return a, nil
}

func (a *App) clearCloneResult() {
	if a.cloneStatus == CloneStatusComplete {
		a.cloneStatus = CloneStatusIdle
	}
	a.cloneError = ""
}

func (a *App) handleCloneResultKeys(msg tea.Msg) (bool, tea.Cmd) {
	if a.cloneStatus != CloneStatusComplete || a.screen != ScreenClone {
		return false, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, nil
	}
	if a.helpOverlayOpen() {
		return false, nil
	}
	k := key.Key()
	switch k.String() {
	case "esc", "escape":
		a.clearCloneResult()
		a.statusMsg = ""
		return true, nil
	case "enter":
		a.clearCloneResult()
		a.statusMsg = ""
		return true, func() tea.Msg { return cloneRequestedMsg{} }
	case "j", "down":
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			cs.scrollLog(-1)
		}
		return true, nil
	case "k", "up":
		if cs, ok := a.screens[ScreenClone].(*cloneScreen); ok {
			cs.scrollLog(1)
		}
		return true, nil
	}
	return false, nil
}

func (a *App) cancelAnalyze() {
	if a.clone.AnalyzeState.Cancel != nil {
		a.clone.AnalyzeState.Cancel()
		a.clone.AnalyzeState.Cancel = nil
	}
	a.clone.AnalyzeState.Loading = false
	a.clone.AnalyzeState.Err = ""
	a.statusMsg = truncateStatus(StyleMuted.Render("Analyze cancelled"), a.width)
}

func (a *App) handleCloneCancel() {
	if a.cloneCancel != nil {
		a.statusMsg = truncateStatus(StyleMuted.Render("Cancelling…"), a.width)
		a.cloneCancel()
	}
}

func (a *App) handleCloneAbortKeys(msg tea.Msg) bool {
	if a.helpOverlayOpen() {
		return false
	}
	if _, ok := isCancelKey(msg); !ok {
		return false
	}

	if a.clone.AnalyzeState.Loading {
		a.cancelAnalyze()
		return true
	}

	if a.cloneStatus != CloneStatusRunning {
		return false
	}

	a.mountCancelRunModal(modalCancelRun, "Cancel clone?", "Stop the running clone. Partial work may remain on the target.", func() tea.Cmd {
		a.handleCloneCancel()
		return nil
	})
	return true
}
