package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

type connectionField struct {
	label  string
	value  *string
	masked bool
}

type connectionPanel int

const (
	connPanelOverview connectionPanel = iota
	connPanelFields
	connPanelList
	connPanelSaveAs
	connPanelRename
	connPanelEdit
)

const (
	connSectionFields = iota
	connSectionList
	connSectionCount
)

type connectionScreen struct {
	draft              *ConnectionDraft
	connStatus         *ConnectionStatus
	errMsg             *string
	fields             []connectionField
	focus              int
	panel              connectionPanel
	store              connections.ConnectionStore
	saveConnections    bool
	getDefaultName     func() string
	setDefaultName     func(string) error
	profiles           []connections.Connection
	listCursor         int
	nameInput          string
	listErr            string
	pickedProfile      *connections.Connection
	editingName        string
	editSnapshot       connections.Connection
	schemasInput       string
	saveAsSchemas      []string
	previewProfileName string
	fieldCursors       []int
	schemasCursor      int
	nameCursor         int
	spinnerFrame       *int
	nav                SectionNav
}

func newConnectionScreen(
	draft *ConnectionDraft,
	connStatus *ConnectionStatus,
	errMsg *string,
	store connections.ConnectionStore,
	saveConnections bool,
	getDefaultName func() string,
	setDefaultName func(string) error,
	spinnerFrame *int,
	entry SectionEntryMode,
) ScreenModel {
	cs := &connectionScreen{
		draft:           draft,
		connStatus:      connStatus,
		errMsg:          errMsg,
		store:           store,
		saveConnections: saveConnections,
		getDefaultName:  getDefaultName,
		setDefaultName:  setDefaultName,
		spinnerFrame:    spinnerFrame,
		fieldCursors:    make([]int, 6),
		fields: []connectionField{
			{label: "Host", value: &draft.Host},
			{label: "Port", value: &draft.Port},
			{label: "Database", value: &draft.Database},
			{label: "User", value: &draft.User},
			{label: "Password", value: &draft.Password, masked: true},
			{label: "SSLMODE", value: &draft.SSLMODE},
		},
	}
	cs.nav = NewSectionNav(connSectionCount)
	cs.refreshProfiles()
	cs.applySectionEntry(entry)
	return cs
}

func (c *connectionScreen) overviewSectionCount() int {
	if c.hasSavedList() {
		return connSectionCount
	}
	return 1
}

func (c *connectionScreen) applySectionEntry(entry SectionEntryMode) {
	c.nav.SectionCnt = c.overviewSectionCount()
	if entry == SectionEntryInside {
		if c.hasSavedList() && len(c.profiles) > 0 {
			c.panel = connPanelList
			c.nav.EnterInside(connSectionList)
			c.positionOnDefaultOrFirst()
			c.previewListProfile()
			return
		}
		c.panel = connPanelFields
		c.nav.EnterInside(connSectionFields)
		c.focus = 0
		c.resetCursorForFocus()
		return
	}
	c.panel = connPanelOverview
	c.nav.Level = SectionNavOverview
	c.nav.Section = 0
}

func (c *connectionScreen) refreshProfiles() {
	if c.store == nil {
		c.profiles = nil
		return
	}
	list, err := c.store.List()
	if err != nil {
		c.listErr = err.Error()
		c.profiles = nil
		return
	}
	c.listErr = ""
	c.profiles = list
	if c.listCursor >= len(c.profiles) {
		c.positionOnDefaultOrFirst()
	}
	if c.panel == connPanelList && len(c.profiles) > 0 {
		c.previewListProfile()
	}
}

func (c *connectionScreen) currentDefaultName() string {
	if c.getDefaultName == nil {
		return ""
	}
	return strings.TrimSpace(c.getDefaultName())
}

func (c *connectionScreen) positionOnDefaultOrFirst() {
	if len(c.profiles) == 0 {
		c.listCursor = 0
		return
	}
	if name := c.currentDefaultName(); name != "" {
		for i, prof := range c.profiles {
			if prof.Name == name {
				c.listCursor = i
				return
			}
		}
	}
	if c.listCursor >= len(c.profiles) {
		c.listCursor = 0
	}
}

func (c *connectionScreen) profileListMarker(name string) string {
	if name == c.currentDefaultName() {
		return "★ "
	}
	return "  "
}

func (c *connectionScreen) connecting() bool {
	return *c.connStatus == ConnStatusConnecting
}

func (c *connectionScreen) hasSavedList() bool {
	return c.saveConnections && c.store != nil
}

// showsSavedProfileList is true when saved profile rows should be visible on the
// connection screen (not hidden behind overview drill-in or fields-only layout).
func (c *connectionScreen) showsSavedProfileList() bool {
	if !c.hasSavedList() {
		return false
	}
	switch c.panel {
	case connPanelSaveAs, connPanelRename, connPanelEdit:
		return false
	default:
		return true
	}
}

func (c *connectionScreen) onTextFieldsPanel() bool {
	return c.panel == connPanelFields || c.panel == connPanelEdit
}

func (c *connectionScreen) inOverview() bool {
	return c.panel == connPanelOverview
}

func (c *connectionScreen) shouldDeferEsc() bool {
	switch c.panel {
	case connPanelSaveAs, connPanelRename, connPanelEdit:
		return true
	case connPanelFields, connPanelList:
		return c.nav.InInside()
	default:
		return false
	}
}

func (c *connectionScreen) enterOverviewSection() {
	switch c.nav.Section {
	case connSectionFields:
		c.panel = connPanelFields
		c.focus = 0
		c.resetCursorForFocus()
	case connSectionList:
		c.panel = connPanelList
		c.listCursor = 0
		c.refreshProfiles()
		c.previewListProfile()
	}
}

func (c *connectionScreen) clearProfilePreview() {
	c.previewProfileName = ""
}

func (c *connectionScreen) previewListProfile() {
	if len(c.profiles) == 0 {
		c.clearProfilePreview()
		return
	}
	prof := c.profiles[c.listCursor]
	*c.draft = draftFromConnection(prof)
	c.previewProfileName = prof.Name
}

func (c *connectionScreen) moveListCursor(delta int) {
	if len(c.profiles) == 0 {
		return
	}
	c.listCursor += delta
	if c.listCursor < 0 {
		c.listCursor = len(c.profiles) - 1
	}
	if c.listCursor >= len(c.profiles) {
		c.listCursor = 0
	}
	c.previewListProfile()
}

func (c *connectionScreen) returnToOverview() {
	c.panel = connPanelOverview
	c.nav.Exit()
}

func (c *connectionScreen) activeFieldCount() int {
	if c.panel == connPanelEdit {
		return len(c.fields) + 1
	}
	return len(c.fields)
}

func (c *connectionScreen) beginEditProfile() {
	if len(c.profiles) == 0 {
		return
	}
	prof := c.profiles[c.listCursor]
	c.editingName = prof.Name
	c.editSnapshot = prof
	*c.draft = draftFromConnection(prof)
	c.schemasInput = strings.Join(prof.Schemas, ", ")
	c.panel = connPanelEdit
	c.focus = 0
	c.resetFieldCursors()
	c.listErr = ""
}

func (c *connectionScreen) resetFieldCursors() {
	for i := range c.fieldCursors {
		c.fieldCursors[i] = 0
	}
	c.schemasCursor = 0
	c.nameCursor = 0
}

func (c *connectionScreen) resetCursorForFocus() {
	if c.focus >= 0 && c.focus < len(c.fieldCursors) {
		v := c.fields[c.focus].value
		c.fieldCursors[c.focus] = len(*v)
	}
	if c.panel == connPanelEdit && c.focus == len(c.fields) {
		c.schemasCursor = len(c.schemasInput)
	}
}

func (c *connectionScreen) saveEditProfile() tea.Cmd {
	schemas := parseCommaSchemas(c.schemasInput)
	prof := connectionFromDraft(*c.draft, c.editingName, schemas)
	if err := c.store.Put(prof); err != nil {
		c.listErr = err.Error()
		return nil
	}
	c.listErr = ""
	c.editingName = ""
	c.schemasInput = ""
	c.panel = connPanelList
	c.nav.EnterInside(connSectionList)
	c.refreshProfiles()
	return nil
}

func (c *connectionScreen) cancelEdit() {
	*c.draft = draftFromConnection(c.editSnapshot)
	c.editingName = ""
	c.schemasInput = ""
	c.listErr = ""
	c.panel = connPanelList
	c.nav.EnterInside(connSectionList)
}

func (c *connectionScreen) requestTestConnection() tea.Cmd {
	return func() tea.Msg {
		return testConnectionRequestedMsg{dsn: c.draft.DSN()}
	}
}

func (c *connectionScreen) beginSaveAs() {
	c.saveAsSchemas = nil
	if len(c.profiles) > 0 {
		c.previewListProfile()
		if schemas := profileSchemas(&c.profiles[c.listCursor]); len(schemas) > 0 {
			c.saveAsSchemas = append([]string(nil), schemas...)
		}
	}
	c.panel = connPanelSaveAs
	c.nameInput = ""
	c.nameCursor = 0
}

func (c *connectionScreen) handleListPanelKey(k tea.Key) (tea.Cmd, bool) {
	if c.panel != connPanelList {
		return nil, false
	}
	if len(k.Text) != 1 {
		return nil, false
	}
	switch strings.ToLower(k.Text) {
	case "t":
		if len(c.profiles) > 0 {
			c.previewListProfile()
		}
		return c.requestTestConnection(), true
	case "s":
		if c.hasSavedList() {
			c.beginSaveAs()
			return nil, true
		}
	case "r":
		if c.hasSavedList() && len(c.profiles) > 0 {
			c.panel = connPanelRename
			c.nameInput = c.profiles[c.listCursor].Name
			c.nameCursor = len(c.nameInput)
			return nil, true
		}
	case "d":
		if c.hasSavedList() && len(c.profiles) > 0 {
			name := c.profiles[c.listCursor].Name
			return func() tea.Msg { return requestDeleteProfileMsg{name: name} }, true
		}
	case "e":
		if c.hasSavedList() && len(c.profiles) > 0 {
			c.beginEditProfile()
			return nil, true
		}
	case "*":
		if c.hasSavedList() && len(c.profiles) > 0 && c.setDefaultName != nil {
			name := c.profiles[c.listCursor].Name
			if err := c.setDefaultName(name); err != nil {
				c.listErr = err.Error()
			} else {
				c.listErr = fmt.Sprintf("Default profile: %s", name)
			}
			return nil, true
		}
	}
	return nil, false
}

func (c *connectionScreen) Update(msg tea.Msg) tea.Cmd {
	if c.connecting() {
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	k := key.Key()

	if c.panel == connPanelSaveAs || c.panel == connPanelRename {
		return c.updateNamePrompt(k)
	}

	if c.panel == connPanelEdit && k.Code == tea.KeyEscape {
		c.cancelEdit()
		return nil
	}

	if c.nav.InInside() && (c.panel == connPanelFields || c.panel == connPanelList) && k.Code == tea.KeyEscape {
		c.returnToOverview()
		return nil
	}

	if c.inOverview() {
		switch k.String() {
		case "j":
			c.nav.MoveSection(1)
			return nil
		case "k":
			c.nav.MoveSection(-1)
			return nil
		}
		switch k.Code {
		case tea.KeyDown:
			c.nav.MoveSection(1)
			return nil
		case tea.KeyUp:
			c.nav.MoveSection(-1)
			return nil
		case tea.KeyEnter:
			c.nav.Enter()
			c.enterOverviewSection()
			return nil
		}
		return nil
	}

	if cmd, handled := c.handleListPanelKey(k); handled {
		return cmd
	}

	if c.onTextFieldsPanel() && k.String() == "ctrl+t" {
		return c.requestTestConnection()
	}

	switch k.Code {
	case tea.KeyTab:
		if c.panel == connPanelFields || c.panel == connPanelEdit {
			count := c.activeFieldCount()
			if k.Mod&tea.ModShift != 0 {
				c.focus--
				if c.focus < 0 {
					c.focus = count - 1
				}
			} else {
				c.focus = (c.focus + 1) % count
			}
			c.resetCursorForFocus()
			return nil
		}
		return nil
	case tea.KeyDown:
		if c.onTextFieldsPanel() {
			c.focus = (c.focus + 1) % c.activeFieldCount()
			c.resetCursorForFocus()
			return nil
		}
		if c.panel == connPanelList {
			c.moveListCursor(1)
			return nil
		}
	case tea.KeyUp:
		if c.onTextFieldsPanel() {
			c.focus--
			if c.focus < 0 {
				c.focus = c.activeFieldCount() - 1
			}
			c.resetCursorForFocus()
			return nil
		}
		if c.panel == connPanelList {
			c.moveListCursor(-1)
			return nil
		}
	case tea.KeyEnter:
		switch c.panel {
		case connPanelFields:
			return c.connectFromDraft()
		case connPanelEdit:
			return c.saveEditProfile()
		case connPanelList:
			if len(c.profiles) == 0 {
				return nil
			}
			prof := c.profiles[c.listCursor]
			c.pickedProfile = &prof
			*c.draft = draftFromConnection(prof)
			return c.connectFromProfile(prof)
		}
	}

	if c.onTextFieldsPanel() {
		if c.panel == connPanelEdit && c.focus == len(c.fields) {
			if handleFieldCursorKey(k, &c.schemasInput, &c.schemasCursor) {
				return nil
			}
		} else if c.focus >= 0 && c.focus < len(c.fields) {
			v := c.fields[c.focus].value
			cur := &c.fieldCursors[c.focus]
			if handleFieldCursorKey(k, v, cur) {
				if c.panel == connPanelFields {
					c.clearProfilePreview()
				}
				return nil
			}
		}
	}
	return nil
}

func (c *connectionScreen) connectFromDraft() tea.Cmd {
	c.listErr = ""
	if c.store != nil {
		sig := connectionFromDraft(*c.draft, "", nil).Signature()
		list, err := c.store.List()
		if err != nil {
			c.listErr = err.Error()
			return nil
		}
		prof, ok, err := connections.ResolveBySignature(list, sig, c.previewProfileName)
		if err != nil {
			c.listErr = err.Error()
			return nil
		}
		if ok {
			p := prof
			c.pickedProfile = &p
			*c.draft = draftFromConnection(prof)
			return c.connectFromProfile(prof)
		}
	}
	c.pickedProfile = nil
	return func() tea.Msg {
		return connectRequestedMsg{dsn: c.draft.DSN()}
	}
}

func (c *connectionScreen) connectFromProfile(prof connections.Connection) tea.Cmd {
	schemas := profileSchemas(&prof)
	dsn := prof.DSN()
	return func() tea.Msg {
		return connectRequestedMsg{dsn: dsn, schemas: schemas}
	}
}

func profileSchemas(prof *connections.Connection) []string {
	if prof == nil || len(prof.Schemas) == 0 {
		return nil
	}
	return append([]string(nil), prof.Schemas...)
}

func (c *connectionScreen) updateNamePrompt(k tea.Key) tea.Cmd {
	switch k.Code {
	case tea.KeyEscape:
		c.panel = connPanelList
		c.nameInput = ""
		c.saveAsSchemas = nil
		c.listErr = ""
		return nil
	case tea.KeyEnter:
		name := strings.TrimSpace(c.nameInput)
		if name == "" {
			c.listErr = "name is required"
			return nil
		}
		saveNote := ""
		switch c.panel {
		case connPanelSaveAs:
			prof := connectionFromDraft(*c.draft, name, c.saveAsSchemas)
			if err := c.store.Save(prof); err != nil {
				c.listErr = err.Error()
				return nil
			}
			if list, err := c.store.List(); err == nil {
				if aliases := connections.OtherNamesWithSignature(list, prof.Signature(), name); len(aliases) > 0 {
					saveNote = fmt.Sprintf("Saved — same credentials as %s", strings.Join(aliases, ", "))
				}
			}
		case connPanelRename:
			old := c.profiles[c.listCursor].Name
			if err := c.store.Rename(old, name); err != nil {
				c.listErr = err.Error()
				return nil
			}
			if old == c.currentDefaultName() && c.setDefaultName != nil {
				if err := c.setDefaultName(name); err != nil {
					c.listErr = err.Error()
					return nil
				}
			}
		}
		c.panel = connPanelList
		c.nameInput = ""
		c.saveAsSchemas = nil
		c.refreshProfiles()
		c.previewListProfile()
		c.listErr = saveNote
		return nil
	}
	if handleFieldCursorKey(k, &c.nameInput, &c.nameCursor) {
		return nil
	}
	return nil
}

func (c *connectionScreen) View(width, height int) string {
	var lines []string
	lines = append(lines, StyleHeader.Render("Connection"))
	lines = append(lines, StyleMuted.Render("1-5 menu · ? info · F1 keys"))
	lines = append(lines, "")

	if c.panel == connPanelSaveAs {
		lines = append(lines, StyleMuted.Render("Save as profile name:"))
		lines = append(lines, StyleAccent.Render(renderEditableField(c.nameInput, c.nameCursor, false, true)))
	} else if c.panel == connPanelRename {
		lines = append(lines, StyleMuted.Render("Rename profile to:"))
		lines = append(lines, StyleAccent.Render(renderEditableField(c.nameInput, c.nameCursor, false, true)))
	} else if c.inOverview() {
		hostSummary := c.draft.Host
		if hostSummary == "" {
			hostSummary = "(not set)"
		}
		lines = append(lines, overviewSectionRow(c.nav, connSectionFields, "Connection fields", hostSummary))
		if c.hasSavedList() {
			listSummary := "no profiles"
			if n := len(c.profiles); n > 0 {
				listSummary = fmt.Sprintf("%d profiles", n)
				if def := c.currentDefaultName(); def != "" {
					listSummary += fmt.Sprintf(" · default: %s", def)
				}
			}
			lines = append(lines, overviewSectionRow(c.nav, connSectionList, "Saved profiles", listSummary))
		}
		lines = append(lines, "")
		lines = append(lines, StyleMuted.Render("↑/↓ section · Enter open"))
	} else {
		if c.panel == connPanelEdit {
			lines = append(lines, StyleMuted.Render("Editing profile: "+c.editingName))
			lines = append(lines, "")
		}
		renderFields := func(editable bool) {
			for i, f := range c.fields {
				if c.panel == connPanelList || c.inOverview() {
					label := StyleMuted.Render(f.label + ":")
					display := *f.value
					if f.masked {
						display = maskedPasswordDisplay(display)
					}
					lines = append(lines, label+" "+StyleMuted.Render(display))
					continue
				}
				label := StyleMuted.Render(f.label + ":")
				focused := editable && i == c.focus
				display := renderEditableField(*f.value, c.fieldCursors[i], f.masked, focused)
				var value string
				if c.connecting() {
					if f.masked {
						display = maskedPasswordDisplay(*f.value)
					}
					value = StyleMuted.Render(display)
				} else if focused {
					value = StyleAccent.Render(display)
				} else if display == "" {
					value = StyleBase.Render("")
				} else {
					value = StyleBase.Render(display)
				}
				lines = append(lines, label+" "+value)
			}
			if c.panel == connPanelEdit {
				label := StyleMuted.Render("Schemas:")
				display := renderEditableField(c.schemasInput, c.schemasCursor, false, c.focus == len(c.fields))
				var value string
				if c.focus == len(c.fields) {
					value = StyleAccent.Render(display)
				} else {
					value = StyleBase.Render(display)
				}
				lines = append(lines, label+" "+value)
			}
		}
		renderFields(c.panel == connPanelFields || c.panel == connPanelEdit)
		if c.panel == connPanelList && len(c.profiles) > 0 {
			prof := c.profiles[c.listCursor]
			if len(prof.Schemas) > 0 {
				lines = append(lines, StyleMuted.Render("Schemas: "+strings.Join(prof.Schemas, ", ")))
			}
		}
	}

	if c.connecting() {
		frame := 0
		if c.spinnerFrame != nil {
			frame = *c.spinnerFrame
		}
		lines = append(lines, "")
		lines = append(lines, formatWalkSpinnerLines("Connecting…", frame)...)
	}

	if c.showsSavedProfileList() {
		lines = append(lines, "")
		lines = append(lines, StyleHeader.Render("Saved profiles"))
		if c.listErr != "" {
			lines = append(lines, StyleWarning.Render(c.listErr))
		}
		if len(c.profiles) == 0 {
			lines = append(lines, StyleMuted.Render("No saved profiles yet — enter credentials in Connection fields and connect with Enter to auto-save"))
		}
		for i, prof := range c.profiles {
			line := c.profileListMarker(prof.Name) + prof.Name + "  " + connections.DisplaySummary(prof)
			switch {
			case c.panel == connPanelList && i == c.listCursor:
				line = StyleAccent.Render("> " + line)
			default:
				line = StyleBase.Render("  " + line)
			}
			lines = append(lines, line)
		}
		switch {
		case c.panel == connPanelList && len(c.profiles) > 0:
			lines = append(lines, StyleMuted.Render("* set default · Enter connect"))
		case c.panel == connPanelOverview && len(c.profiles) > 0:
			lines = append(lines, StyleMuted.Render("Enter Saved profiles section to pick · e/s/r/d/t actions"))
		case c.panel == connPanelFields && len(c.profiles) > 0:
			lines = append(lines, StyleMuted.Render("Open Saved profiles section to pick a profile"))
		}
	}

	if *c.errMsg != "" {
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("Connection failed: "+*c.errMsg))
	}
	content := strings.Join(lines, "\n")
	return StyleBorder.Width(max(0, width-2)).Height(max(0, height-2)).Render(content)
}

func (c *connectionScreen) FocusIndex() int {
	return c.focus
}

func (c *connectionScreen) SetFocus(index int) {
	if index >= 0 && index < len(c.fields) {
		c.focus = index
		c.resetCursorForFocus()
	}
}

func (c *connectionScreen) onFieldCursorNavigation() bool {
	return c.onTextFieldsPanel() || c.panel == connPanelSaveAs || c.panel == connPanelRename
}

func (c *connectionScreen) PickedProfile() *connections.Connection {
	return c.pickedProfile
}

func (c *connectionScreen) ClearPickedProfile() {
	c.pickedProfile = nil
}
