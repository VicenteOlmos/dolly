package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalDeleteProfile
	modalCancelRun
	modalRestoreConfirm
	modalCloneConfirm
	modalQuit
	modalAnalyzeResult
)

type modalState struct {
	kind     modalKind
	title    string
	body     string
	analyze  *analyzeResult
	scroll   int
	confirm  string
	cancel   string
	onOK     func() tea.Cmd
	onCancel func() tea.Cmd
}

func (a *App) mountModal(m modalState) {
	a.modal = &m
}

func (a *App) dismissModal() {
	a.modal = nil
}

func (a *App) modalOpen() bool {
	return a.modal != nil
}

func (a *App) updateModal(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if a.modal == nil {
		return a, nil, false
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return a, nil, true
	}
	k := key.Key()
	if a.modal.kind == modalAnalyzeResult {
		switch {
		case k.String() == "j", k.String() == "down":
			if a.modal.analyze != nil {
				a.modal.scroll++
				if a.modal.scroll > analyzeModalMaxScroll(*a.modal.analyze) {
					a.modal.scroll = analyzeModalMaxScroll(*a.modal.analyze)
				}
			}
			return a, nil, true
		case k.String() == "k", k.String() == "up":
			if a.modal.scroll > 0 {
				a.modal.scroll--
			}
			return a, nil, true
		case k.String() == "enter":
			onOK := a.modal.onOK
			a.dismissModal()
			if onOK != nil {
				return a, onOK(), true
			}
			return a, nil, true
		case k.String() == "q",
			strings.ToLower(k.String()) == "esc",
			strings.ToLower(k.String()) == "escape",
			k.Code == tea.KeyEscape:
			onCancel := a.modal.onCancel
			a.dismissModal()
			if onCancel != nil {
				return a, onCancel(), true
			}
			return a, nil, true
		}
		return a, nil, true
	}
	switch {
	case strings.ToLower(k.String()) == "y":
		onOK := a.modal.onOK
		a.dismissModal()
		if onOK != nil {
			return a, onOK(), true
		}
		return a, nil, true
	case strings.ToLower(k.String()) == "n",
		strings.ToLower(k.String()) == "esc",
		strings.ToLower(k.String()) == "escape",
		k.Code == tea.KeyEscape:
		onCancel := a.modal.onCancel
		a.dismissModal()
		if onCancel != nil {
			return a, onCancel(), true
		}
		return a, nil, true
	}
	return a, nil, true
}

func (a *App) renderModalBox(width int) string {
	if a.modal == nil {
		return ""
	}
	m := a.modal
	body := m.body
	if m.kind == modalAnalyzeResult && m.analyze != nil {
		body = formatAnalyzeModalBody(*m.analyze, m.scroll)
	}
	lines := []string{
		StyleHeader.Render(m.title),
		"",
		StyleBase.Render(body),
		"",
	}
	if m.kind == modalAnalyzeResult && m.analyze != nil && len(m.analyze.Objects) > analyzeModalVisibleRows {
		lines = append(lines, StyleMuted.Render("↑/↓ scroll table list"))
	}
	lines = append(lines, StyleAccent.Render(m.confirm)+StyleMuted.Render("  ·  ")+StyleMuted.Render(m.cancel))
	content := strings.Join(lines, "\n")
	boxW := min(width-4, analyzeModalMaxWidth)
	if m.kind != modalAnalyzeResult {
		boxW = min(width-4, 56)
	}
	if boxW < 20 {
		boxW = max(0, width-4)
	}
	return StyleModal.Width(boxW).Render(content)
}

func overlayModal(base string, modal string, width, height int) string {
	if modal == "" {
		return base
	}
	canvas := lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, base)
	boxW, boxH := lipgloss.Width(modal), lipgloss.Height(modal)
	mx := max(0, (width-boxW)/2)
	my := max(0, (height-boxH)/2)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(canvas),
		lipgloss.NewLayer(modal).X(mx).Y(my).Z(1),
	).Render()
}

func (a *App) mountDeleteProfileModal(name string) {
	a.mountModal(modalState{
		kind:    modalDeleteProfile,
		title:   "Delete profile?",
		body:    "Remove \"" + name + "\" from saved connections. This cannot be undone.",
		confirm: "Y confirm",
		cancel:  "N / Esc cancel",
		onOK: func() tea.Cmd {
			return func() tea.Msg { return profileDeleteConfirmedMsg{name: name} }
		},
	})
}

func (a *App) mountCancelRunModal(kind modalKind, title, body string, onConfirm func() tea.Cmd) {
	a.mountModal(modalState{
		kind:     kind,
		title:    title,
		body:     body,
		confirm:  "Y confirm",
		cancel:   "N / Esc cancel",
		onOK:     onConfirm,
		onCancel: nil,
	})
}

func (a *App) mountRestoreConfirmModal(title, body, inputDir string, onConfirm func() tea.Cmd) {
	onOK := onConfirm
	if onOK == nil {
		dir := inputDir
		onOK = func() tea.Cmd {
			return func() tea.Msg { return restoreRequestedMsg{inputDir: dir} }
		}
	}
	a.mountModal(modalState{
		kind:     modalRestoreConfirm,
		title:    title,
		body:     body,
		confirm:  "Y confirm",
		cancel:   "N / Esc cancel",
		onOK:     onOK,
		onCancel: nil,
	})
}

func (a *App) mountCloneConfirmModal(title, body string, onConfirm func() tea.Cmd) {
	onOK := onConfirm
	if onOK == nil {
		onOK = func() tea.Cmd {
			return func() tea.Msg { return cloneProceedMsg{} }
		}
	}
	a.mountModal(modalState{
		kind:     modalCloneConfirm,
		title:    title,
		body:     body,
		confirm:  "Y confirm",
		cancel:   "N / Esc cancel",
		onOK:     onOK,
		onCancel: nil,
	})
}

func (a *App) mountQuitModal() {
	a.mountModal(modalState{
		kind:    modalQuit,
		title:   "Quit dolly?",
		body:    "Close the session and disconnect. Unsaved field edits are discarded.",
		confirm: "Y quit",
		cancel:  "N / Esc stay",
		onOK: func() tea.Cmd {
			a.handleDumpCancel()
			a.handleRestoreCancel()
			a.handleCloneCancel()
			return tea.Quit
		},
	})
}

func (a *App) mountAnalyzeResultModal(r analyzeResult) {
	a.mountModal(modalState{
		kind:    modalAnalyzeResult,
		title:   "Analyze complete",
		analyze: &r,
		scroll:  0,
		confirm: "Enter continue clone",
		cancel:  "q cancel",
		onOK: func() tea.Cmd {
			return func() tea.Msg { return cloneRequestedMsg{} }
		},
		onCancel: func() tea.Cmd {
			a.clone.AnalyzeState.Result = nil
			a.statusMsg = truncateStatus(StyleMuted.Render("Clone cancelled"), a.width)
			return nil
		},
	})
}

type requestDeleteProfileMsg struct {
	name string
}

type profileDeleteConfirmedMsg struct {
	name string
}
