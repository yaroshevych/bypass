package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"bypass/internal/platform"
	"bypass/internal/search"
	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type paletteFrame struct {
	scope, query     string
	selected, offset int
}
type paletteState struct {
	scope            string
	query            input
	selected, offset int
	stack            []paletteFrame
	original         platform.Settings
	originalStyles   styleSet
	preview          bool
	context          bool
	x, y             int
	err, entryUUID   string
}
type command struct {
	id, group, label, key, reason, next, value string
	run                                        func() tea.Cmd
}
type paletteRow struct {
	text   string
	choice int
}

func (m *Model) writeReason() string {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return "Unlock a vault first"
	}
	if s.ReadOnly {
		return "Vault is read only"
	}
	if s.Stale {
		return "Reload changed vault first"
	}
	if m.settings.BackupDir == "" {
		return "Backup location unavailable"
	}
	return ""
}
func (m *Model) commands() []command {
	s := m.ws.ActiveSession()
	v := m.ws.ActiveView()
	entry := m.selected()
	auth := s != nil && s.DB != nil
	needVault := ""
	if !auth {
		needVault = "Unlock a vault first"
	}
	needEntry := needVault
	if needEntry == "" && entry == nil {
		needEntry = "Select an entry first"
	}
	writeReason := m.writeReason()
	editReason := writeReason
	if editReason == "" {
		editReason = needEntry
	}
	if m.editor != nil {
		editReason = "Already editing an entry"
	}
	var commands []command
	add := func(id, group, label, key, reason, next string, run func() tea.Cmd) {
		commands = append(commands, command{id: id, group: group, label: label, key: key, reason: reason, next: next, run: run})
	}
	invoke := func(key string) func() tea.Cmd {
		return func() tea.Cmd {
			cmd, _ := m.operationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			return cmd
		}
	}
	createReason := ""
	if _, ok := m.backend.(vaultCreator); !ok {
		createReason = "Backend cannot create vaults"
	}
	if m.readOnly {
		createReason = "Restart without --read-only to create a vault"
	}
	if reason := m.nativeReason("db-create"); reason != "" {
		createReason = reason
	}
	add("new-vault", "File", "New Vault…", "N", createReason, "", func() tea.Cmd { return m.leaveEditor(m.openCreateVault) })
	add("open", "File", "Open Vault…", "Ctrl+O", "", "files", nil)
	add("quit", "App", "Quit bypass", "q", "", "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { m.Close(); return tea.Quit }) })
	closeReason := ""
	if v == nil {
		closeReason = "No open view"
	}
	add("close-view", "Tabs", "Close Tab", "Ctrl+W", closeReason, "", func() tea.Cmd {
		return m.leaveEditor(func() tea.Cmd { m.ws.CloseView(); m.normalize(); m.syncPrivacyState(); return m.clearClipboard() })
	})
	add("close-vault", "File", "Close Current Vault", "", closeReason, "", func() tea.Cmd {
		return m.leaveEditor(func() tea.Cmd { m.ws.CloseVault(); m.normalize(); m.syncPrivacyState(); return m.clearClipboard() })
	})
	add("lock", "Vault", "Lock Current Vault", "", needVault, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(false) }) })
	allReason := ""
	if !m.anyUnlocked() {
		allReason = "No unlocked vaults"
	}
	add("lock-all", "Vault", "Lock All Vaults", "Ctrl+L", allReason, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(true) }) })
	add("reload", "Vault", "Reload Vault", "Ctrl+R", needVault, "", func() tea.Cmd { return m.requestReload(s.ID) })
	readLabel := "Enable Read Only"
	if s != nil && s.ReadOnly {
		readLabel = "Enable Writes"
	}
	infoReason := needVault
	if _, ok := m.backend.(databaseInspector); !ok {
		infoReason = "Database information unavailable from this backend"
	}
	if reason := m.nativeReason("db-info"); reason != "" {
		infoReason = reason
	}
	add("database-info", "Vault", "Database Info", "", infoReason, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return m.openDatabaseInfo(false) }) })
	add("database-settings", "Vault", "Database Settings", "", infoReason, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return m.openDatabaseInfo(true) }) })
	credentialsReason := writeReason
	if _, ok := m.backend.(credentialChanger); !ok {
		credentialsReason = "Credential changes unavailable from this backend"
	}
	if s != nil && s.Credentials.YubiKey != "" {
		credentialsReason = "Hardware-factor credential changes unsupported"
	}
	if reason := m.nativeReason("db-edit"); reason != "" {
		credentialsReason = reason
	}
	add("credentials", "Vault", "Change Vault Credentials…", "", credentialsReason, "", func() tea.Cmd { return m.leaveEditor(m.openCredentials) })
	add("read-only", "Vault", readLabel, "R", needVault, "", invoke("R"))
	add("new", "Entry", "New Entry", "n", writeReason, "", func() tea.Cmd { return m.leaveEditor(invoke("n")) })
	add("edit", "Entry", "Edit Entry", "e", editReason, "", invoke("e"))
	add("group", "Entry", "New Group", "g", writeReason, "", func() tea.Cmd { return m.leaveEditor(invoke("g")) })
	moveReason := writeReason
	if moveReason == "" {
		moveReason = needEntry
	}
	if m.editor != nil {
		moveReason = "Finish editing first"
	}
	add("move", "Entry", "Move Entry…", "m", moveReason, "destination", nil)
	restoreReason := moveReason
	if restoreReason == "" && !entry.Recycled {
		restoreReason = "Select a recycled entry"
	}
	add("restore", "Entry", "Restore Entry…", "r", restoreReason, "destination", nil)
	deleteReason := writeReason
	if deleteReason == "" && m.currentNav().kind != "group" {
		deleteReason = needEntry
	}
	if m.editor != nil {
		deleteReason = "Finish editing first"
	}
	add("delete", "Entry", "Delete Selected Entry / Group", "d", deleteReason, "", func() tea.Cmd { m.confirmDelete(); return nil })
	focusedCopyReason := needEntry
	if m.editor != nil {
		focusedCopyReason = "Finish editing before copying stored fields"
	}
	add("copy-focused", "Field", "Copy Focused Field", "c", focusedCopyReason, "", func() tea.Cmd { return m.copyField("") })
	for _, field := range []struct{ name, label string }{{"Title", "Title"}, {"UserName", "Username"}, {"Password", "Password"}, {"Email", "Email"}, {"URL", "URL"}, {"Notes", "Notes"}} {
		name, label := field.name, field.label
		reason := needEntry
		if name == "Email" && entry != nil {
			present := false
			for _, f := range entry.Fields {
				if f.Name == name {
					present = true
				}
			}
			if !present {
				reason = "Entry has no Email field"
			}
		}
		if m.editor != nil {
			reason = "Finish editing before copying stored fields"
		}
		add("copy:"+name, "Field", "Copy "+label, "", reason, "", func() tea.Cmd { return m.copyField(name) })
	}
	grandReason := needEntry
	if m.editor != nil {
		grandReason = "Finish editing before viewing stored fields"
	}
	totpReason, historyReason := needEntry, needEntry
	if entry != nil && !entry.HasTOTP {
		totpReason = "No TOTP configured for this entry"
	}
	if entry != nil && len(entry.History) == 0 {
		historyReason = "No history snapshots for this entry"
	}
	if reason := m.nativeReason("show"); reason != "" {
		totpReason = reason
	}
	attachmentReason := needEntry
	if reason := m.nativeReason("attachment-import", "attachment-export", "attachment-rm"); reason != "" {
		attachmentReason = reason
	}
	add("totp", "Field", "Show / Copy TOTP", "", totpReason, "", func() tea.Cmd { return m.leaveEditor(m.openTOTP) })
	add("attachments", "Entry", "Attachments…", "", attachmentReason, "", func() tea.Cmd { return m.leaveEditor(m.openAttachments) })
	add("history-view", "Entry", "Entry History…", "", historyReason, "", func() tea.Cmd { return m.leaveEditor(m.openHistory) })
	add("grand-view", "Field", "Grand View — Indexed Field", "G", grandReason, "", m.openGrandView)
	add("reveal", "Field", "Reveal / Hide Focused Field", "v", needEntry, "", func() tea.Cmd { return m.toggleReveal() })
	add("generator", "Field", "Password Generator", "Ctrl+G", "", "", func() tea.Cmd { m.openGenerator(); return nil })
	saveReason := writeReason
	if m.editor == nil {
		saveReason = "No entry draft"
	}
	add("save", "Editor", "Save Entry Draft", "Ctrl+S", saveReason, "", m.saveEditor)
	cancelReason := ""
	if m.editor == nil {
		cancelReason = "No entry draft"
	}
	add("cancel-edit", "Editor", "Cancel Editing", "Esc", cancelReason, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return nil }) })
	add("search", "View", "Search All Entries", "Ctrl+F", needVault, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return m.beginSearch(true) }) })
	add("history", "View", "Search History…", "", needVault, "history", nil)
	add("clear-history", "View", "Clear Search History", "", needVault, "", func() tea.Cmd {
		m.ws.ClearSearches(s.ID)
		if strings.HasPrefix(m.currentNav().key, "history:") {
			v.NavKey = "history-list"
			v.Query = ""
		}
		m.remapNav()
		m.normalize()
		return nil
	})
	add("clear-filters", "View", "Clear All Filters", "", needVault, "", func() tea.Cmd {
		v.Query = ""
		v.NavFilter = ""
		v.DetailFilter = ""
		m.remapNav()
		m.normalize()
		return nil
	})
	add("jump", "View", "Jump to Pane / Field", "J", needVault, "", func() tea.Cmd { m.jump = true; return nil })
	add("zoom", "View", "Zoom / Restore Focused Pane", "z", needVault, "", func() tea.Cmd { v.Zoom = !v.Zoom; return nil })
	add("wider", "View", "Widen Focused Pane", "", needVault, "", func() tea.Cmd { m.resizeFocused(2); return nil })
	add("narrower", "View", "Narrow Focused Pane", "", needVault, "", func() tea.Cmd { m.resizeFocused(-2); return nil })
	newTabReason := closeReason
	if s == nil || s.DB == nil {
		newTabReason = needVault
	}
	add("duplicate", "Tabs", "New Tab", "t", newTabReason, "", func() tea.Cmd { return m.leaveEditor(m.duplicateTab) })
	add("previous-tab", "Tabs", "Previous Tab", "[", closeReason, "", func() tea.Cmd {
		return m.leaveEditor(func() tea.Cmd {
			if m.moveTab(-1) {
				m.normalize()
			}
			return nil
		})
	})
	add("next-tab", "Tabs", "Next Tab", "]", closeReason, "", func() tea.Cmd {
		return m.leaveEditor(func() tea.Cmd {
			if m.moveTab(1) {
				m.normalize()
			}
			return nil
		})
	})
	for position, viewIndex := range m.visibleTabIndexes() {
		position, viewIndex := position, viewIndex
		view := m.ws.Views[viewIndex]
		session := m.ws.Sessions[view.SessionID]
		add("tab:"+view.ID, "Tabs", fmt.Sprintf("Switch to %d: %s", position+1, vaultLabel(session.Path)), fmt.Sprintf("%d", position+1), "", "", func() tea.Cmd {
			return m.leaveEditor(func() tea.Cmd { m.switchTabPosition(position); m.normalize(); return nil })
		})
	}
	add("theme", "Settings", "Choose Theme…", "", "", "themes", nil)
	add("appearance", "Settings", "Choose Compact / Classic…", "", "", "appearance", nil)
	add("ascii", "Settings", "Toggle ASCII UI", "", "", "", func() tea.Cmd { m.settings.ASCII = !m.settings.ASCII; return m.persistPreferences() })
	add("set-pin", "Privacy", "Set / Change Session PIN…", "", allReason, "", func() tea.Cmd { m.configurePIN(false); return nil })
	pinReason := allReason
	if pinReason == "" && !m.gate.Configured() {
		pinReason = "Set a session PIN first"
	}
	add("protect", "Privacy", "Protect Screen with PIN", "Ctrl+K", pinReason, "", m.protectScreen)
	clearPINReason := allReason
	if clearPINReason == "" && !m.gate.Configured() {
		clearPINReason = "No session PIN configured"
	}
	add("clear-pin", "Privacy", "Remove Session PIN", "", clearPINReason, "", func() tea.Cmd { m.gate.Clear(); m.setNotice("Session PIN removed.", false); return nil })
	add("help", "Help", "Keyboard Shortcuts", "?", "", "", func() tea.Cmd { m.help = true; m.helpOffset = 0; m.helpQuery.clear(); return nil })
	return commands
}

func (m *Model) openPalette(scope string, context bool) {
	m.clearExtras()
	m.clearLifecycle()
	m.hideReveal()
	m.help = false
	m.palette = &paletteState{scope: scope, original: m.settings, originalStyles: m.styles, context: context, x: -1, y: -1}
	m.refreshPalette()
}
func (m *Model) closePalette(commit bool) {
	p := m.palette
	if p == nil {
		return
	}
	if p.preview && !commit {
		m.settings = p.original
		m.styles = p.originalStyles
	}
	p.query.clear()
	for i := range p.stack {
		p.stack[i] = paletteFrame{}
	}
	p.stack = nil
	m.palette = nil
}
func (m *Model) pushPalette(scope string) {
	p := m.palette
	if p == nil {
		return
	}
	p.stack = append(p.stack, paletteFrame{p.scope, p.query.value(), p.selected, p.offset})
	p.query.clear()
	p.scope = scope
	p.selected = 0
	p.offset = 0
	p.err = ""
	if scope == "destination" {
		if e := m.selected(); e != nil {
			p.entryUUID = e.UUID
		}
	}
	m.refreshPalette()
	m.previewChoice()
}
func (m *Model) paletteBack() {
	p := m.palette
	if p == nil {
		return
	}
	if len(p.stack) == 0 {
		m.closePalette(false)
		return
	}
	if p.preview {
		m.settings = p.original
		m.styles = p.originalStyles
		p.preview = false
	}
	f := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	p.scope = f.scope
	p.query.set(f.query)
	p.selected = f.selected
	p.offset = f.offset
	p.err = ""
	m.refreshPalette()
}
func (m *Model) paletteChoices() []command {
	p := m.palette
	if p == nil {
		return nil
	}
	var choices []command
	switch p.scope {
	case "themes":
		for _, label := range themeNames() {
			id := strings.ReplaceAll(strings.ToLower(label), " ", "-")
			choices = append(choices, command{id: "theme:" + id, group: "Themes", label: label, value: id})
		}
	case "appearance":
		choices = []command{{id: "compact", group: "Appearance", label: "Compact — pills, tighter rows", value: "compact"}, {id: "classic", group: "Appearance", label: "Classic — dividers, roomier rows", value: "classic"}}
	case "files":
		choices = append(choices, command{id: "browse", group: "Files", label: "Browse Files…", run: func() tea.Cmd { return m.leaveEditor(m.openPicker) }})
		for _, path := range m.recent {
			path := path
			choices = append(choices, command{id: path, group: "Recent Vaults", label: vaultLabel(path) + " · " + safe(filepath.Dir(path)), run: func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { m.beginUnlock(path); return nil }) }})
		}
	case "history":
		if s := m.ws.ActiveSession(); s != nil {
			for _, record := range s.Searches {
				record := record
				choices = append(choices, command{id: record.ID, group: "Recent Searches", label: record.Query + " · " + record.NavKey, run: func() tea.Cmd {
					v := m.ws.ActiveView()
					v.NavFilter = ""
					v.NavKey = record.NavKey
					v.GroupUUID = record.GroupUUID
					v.Query = record.Query
					v.Focus = 1
					m.ws.RecordSearch(s.ID, record.Query, record.NavKey, record.GroupUUID)
					m.remapNav()
					m.normalize()
					return nil
				}})
			}
		}
	case "destination":
		if s := m.ws.ActiveSession(); s != nil && s.DB != nil {
			for _, g := range destinationGroups(s.DB, s.Destinations) {
				if g.Recycled {
					continue
				}
				g := g
				reason := ""
				for _, e := range s.DB.Entries {
					if e.UUID == p.entryUUID && e.GroupUUID == g.UUID {
						reason = "Already in this group"
					}
				}
				entryID := p.entryUUID
				choices = append(choices, command{id: g.UUID, group: "Destination", label: groupLabel(g.Path), reason: reason, run: func() tea.Cmd {
					return m.apply(vault.Change{Kind: vault.MoveEntry, EntryUUID: entryID, GroupUUID: g.UUID})
				}})
			}
		}
	default:
		choices = m.commands()
		if p.context {
			var scoped []command
			for _, choice := range choices {
				if choice.group == "Entry" || choice.group == "Field" || choice.group == "Editor" {
					scoped = append(scoped, choice)
				}
			}
			choices = scoped
		}
	}
	if p.query.value() == "" {
		return choices
	}
	var docs []search.Document
	byID := map[string]command{}
	for _, choice := range choices {
		docs = append(docs, search.Document{ID: choice.id, Title: choice.label, Username: choice.group, Notes: choice.key})
		byID[choice.id] = choice
	}
	results, err := search.Find(p.query.value(), docs)
	if err != nil {
		return nil
	}
	choices = nil
	for _, result := range results {
		choices = append(choices, byID[result.ID])
	}
	return choices
}
func (m *Model) paletteRows(choices []command) ([]paletteRow, int) {
	p := m.palette
	c := m.colors()
	var rows []paletteRow
	group := ""
	selectedRow := 0
	for i, choice := range choices {
		if choice.group != group {
			rows = append(rows, paletteRow{text: c.muted.Bold(true).Render(choice.group), choice: -1})
			group = choice.group
		}
		text := "  " + m.highlightText(choice.label, p.query.value(), "title")
		if choice.key != "" {
			text += "  " + c.muted.Render(choice.key)
		}
		if choice.reason != "" {
			text = c.muted.Render("  " + safe(choice.label) + " · " + safe(choice.reason))
		}
		if i == p.selected {
			text = c.active.Render("› " + safe(choice.label))
			if choice.reason != "" {
				text += " " + c.muted.Render(choice.reason)
			}
			selectedRow = len(rows)
		}
		rows = append(rows, paletteRow{text: text, choice: i})
	}
	return rows, selectedRow
}
func (m *Model) refreshPalette() {
	p := m.palette
	if p == nil {
		return
	}
	choices := m.paletteChoices()
	p.selected = min(max(0, p.selected), max(0, len(choices)-1))
	rows, selected := m.paletteRows(choices)
	p.offset = viewportOffset(selected, len(rows), max(1, m.height-12), p.offset)
}
func (m *Model) previewChoice() {
	p := m.palette
	if p == nil {
		return
	}
	choices := m.paletteChoices()
	if len(choices) == 0 {
		return
	}
	choice := choices[p.selected]
	if p.scope == "themes" {
		m.settings.Theme = choice.value
		m.styles = stylesForTheme(choice.value)
		p.preview = true
	}
	if p.scope == "appearance" {
		m.settings.Appearance = choice.value
		p.preview = true
	}
}
func (m *Model) executePalette() tea.Cmd {
	p := m.palette
	choices := m.paletteChoices()
	if p == nil || len(choices) == 0 {
		return nil
	}
	choice := choices[p.selected]
	if choice.reason != "" {
		p.err = choice.reason
		return nil
	}
	if choice.next != "" {
		m.pushPalette(choice.next)
		return nil
	}
	if p.scope == "themes" || p.scope == "appearance" {
		m.previewChoice()
		m.closePalette(true)
		return m.persistPreferences()
	}
	var origin *paletteState
	if p.scope == "files" {
		origin = p
	}
	m.closePalette(false)
	var cmd tea.Cmd
	if choice.run != nil {
		cmd = choice.run()
	}
	if origin != nil && m.screen == chooseFile {
		m.pickerReturn = &paletteState{scope: "files", original: m.settings, originalStyles: m.styles, x: -1, y: -1}
	}
	return cmd
}
func (m *Model) paletteKey(k tea.KeyMsg) tea.Cmd {
	p := m.palette
	switch k.String() {
	case "ctrl+c":
		m.closePalette(false)
		return m.leaveEditor(func() tea.Cmd { m.Close(); return tea.Quit })
	case "ctrl+k":
		return m.protectScreen()
	case "esc":
		m.paletteBack()
		return nil
	case "backspace":
		if p.query.value() == "" && len(p.stack) > 0 {
			m.paletteBack()
			return nil
		}
		p.query.update(k)
		p.selected = 0
	case "up", "shift+tab":
		p.selected = max(0, p.selected-1)
	case "down", "tab":
		p.selected = min(max(0, len(m.paletteChoices())-1), p.selected+1)
	case "enter":
		return m.executePalette()
	default:
		p.query.update(k)
		p.selected = 0
	}
	p.err = ""
	m.refreshPalette()
	m.previewChoice()
	return nil
}
func (m *Model) paletteBody() (string, []string, string) {
	p := m.palette
	choices := m.paletteChoices()
	rows, _ := m.paletteRows(choices)
	title := map[string]string{"actions": "Commands", "themes": "Theme", "appearance": "Choose Appearance", "files": "Open Vault", "history": "Search History", "destination": "Move / Restore Entry"}[p.scope]
	if p.context {
		title = "Item Actions"
	}
	if len(p.stack) > 0 && p.scope != "themes" {
		title = "Commands › " + title
	}
	searchWidth := max(1, min(86, m.width-4)-4)
	count := paletteCountLabel(p.scope, len(choices))
	var body []string
	if strings.EqualFold(m.settings.Appearance, "compact") {
		body = []string{paletteFilterRow(p.query, count, searchWidth, m.colors()), ""}
	} else {
		body = []string{""}
	}
	capacity := max(1, m.height-12)
	start := min(p.offset, max(0, len(rows)-1))
	for i := start; i < len(rows) && i < start+capacity; i++ {
		body = append(body, rows[i].text)
	}
	if len(choices) == 0 {
		body = append(body, m.colors().muted.Render("No matching actions"))
	}
	if p.err != "" {
		body = append(body, m.colors().errorStyle.Render(p.err))
	}
	footer := ""
	return title, body, footer
}

func paletteFilterRow(query input, count string, width int, c *styleSet) string {
	field := paletteFilterInput(query, c)
	fieldWidth := lipgloss.Width(field)
	count = c.muted.Render(count)
	countWidth := lipgloss.Width(count)
	if width <= 0 {
		return ""
	}
	fieldBudget := max(1, width-countWidth-1)
	if fieldWidth > fieldBudget {
		field = ansi.Truncate(field, fieldBudget, "…")
		fieldWidth = lipgloss.Width(field)
	}
	gap := max(1, width-fieldWidth-countWidth)
	return field + c.base.Render(strings.Repeat(" ", gap)) + count
}

func paletteFilterInput(query input, c *styleSet) string {
	placeholder := "type to filter"
	var field string
	if query.value() == "" {
		field = c.base.Render(" ") + c.active.Render("t") + c.muted.Render(placeholder[1:]+" ")
	} else {
		runes := []rune(safe(query.value()))
		cursor := min(query.cursor, len(runes))
		prefix, suffix := string(runes[:cursor]), string(runes[cursor:])
		caret := " "
		if suffix != "" {
			caret = string([]rune(suffix)[0])
			suffix = string([]rune(suffix)[1:])
		}
		field = c.base.Render(" "+prefix) + c.active.Render(caret) + c.base.Render(suffix+" ")
	}
	minimum := lipgloss.Width(c.base.Render(" " + placeholder + " "))
	fieldWidth := lipgloss.Width(field)
	if fieldWidth < minimum {
		field += c.base.Render(strings.Repeat(" ", minimum-fieldWidth))
	}
	return field
}

func (m *Model) paletteLines() []string {
	title, body, footer := m.paletteBody()
	if !strings.EqualFold(m.settings.Appearance, "compact") {
		choices := m.paletteChoices()
		count := paletteCountLabel(m.palette.scope, len(choices))
		width := max(1, min(86, m.width-4)-4)
		header := paletteClassicHeader(title, m.palette.query, count, width, m.settings.ASCII, m.colors())
		divider := lipgloss.Width(strings.ToUpper(title)) + 3
		return m.overlay(title, body, footer, min(86, m.width-4), m.palette.x, m.palette.y, dialogrender.Header{Content: header, Divider: divider})
	}
	return m.overlay(title, body, footer, min(86, m.width-4), m.palette.x, m.palette.y)
}

func paletteCountLabel(scope string, count int) string {
	label := "commands"
	singular := "command"
	switch scope {
	case "themes":
		label, singular = "themes", "theme"
	case "appearance":
		label, singular = "appearances", "appearance"
	case "files":
		label, singular = "files", "file"
	case "history":
		label, singular = "searches", "search"
	}
	if count == 1 {
		label = singular
	}
	return fmt.Sprintf("%d %s", count, label)
}

func paletteClassicHeader(title string, query input, count string, width int, ascii bool, c *styleSet) string {
	label := c.accent.Render(strings.ToUpper(title))
	dividerText := " │"
	if ascii {
		dividerText = " | "
	}
	divider := c.lineStyle.Render(dividerText)
	filterWidth := max(1, width-lipgloss.Width(label)-lipgloss.Width(divider))
	return label + divider + paletteFilterRow(query, count, filterWidth, c)
}
func (m *Model) palettePointer(msg tea.MouseMsg) tea.Cmd {
	p := m.palette
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	if msg.Button == tea.MouseButtonWheelUp {
		p.selected = max(0, p.selected-1)
		m.refreshPalette()
		m.previewChoice()
		return nil
	}
	if msg.Button == tea.MouseButtonWheelDown {
		p.selected = min(max(0, len(m.paletteChoices())-1), p.selected+1)
		m.refreshPalette()
		m.previewChoice()
		return nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return nil
	}
	_, body, _ := m.paletteBody()
	width := min(86, m.width-4)
	height := min(len(body), max(1, m.height-8)) + 5
	x, y := p.x, p.y
	if x < 0 {
		x = (m.width - width) / 2
	}
	x = min(max(0, x), max(0, m.width-width))
	if y < 0 {
		y = (m.height - height) / 2
	}
	y = min(max(0, y), max(0, m.height-height))
	if msg.X < x || msg.X >= x+width || msg.Y < y || msg.Y >= y+height {
		m.closePalette(false)
		return nil
	}
	rows, _ := m.paletteRows(m.paletteChoices())
	index := msg.Y - y - 5 + p.offset
	if index >= 0 && index < len(rows) && rows[index].choice >= 0 {
		p.selected = rows[index].choice
		return m.executePalette()
	}
	return nil
}

func (m *Model) helpActionLines() []string {
	var lines []string
	query := strings.ToLower(m.helpQuery.value())
	for _, line := range []string{"↑↓ / j k       Navigate lists / fields", "←→ / h l       Change pane", "Tab / Shift+Tab Fields / panes", "/              Filter focused pane", "1–9            Switch view", "[ / ]          Resize pane"} {
		if query == "" || strings.Contains(strings.ToLower(line), query) {
			lines = append(lines, line)
		}
	}
	for _, command := range m.commands() {
		if command.key == "" {
			continue
		}
		line := fmt.Sprintf("%-14s %s", command.key, command.label)
		if command.reason != "" {
			line += " (" + command.reason + ")"
		}
		if query == "" || strings.Contains(strings.ToLower(line), query) {
			lines = append(lines, line)
		}
	}
	return append([]string{m.colors().accent.Render("Search shortcuts: " + m.helpQuery.display(false) + "▏"), ""}, lines...)
}

// Recent destinations precede the remaining hierarchy; deleted and recycled groups are omitted.
func destinationGroups(db *vault.Database, recent []string) []vault.Group {
	var groups []vault.Group
	seen := map[string]bool{}
	for _, id := range recent {
		for _, g := range db.Groups {
			if g.UUID == id && !g.Recycled && !seen[id] {
				groups = append(groups, g)
				seen[id] = true
			}
		}
	}
	for _, g := range db.Groups {
		if !g.Recycled && !seen[g.UUID] {
			groups = append(groups, g)
		}
	}
	return groups
}
