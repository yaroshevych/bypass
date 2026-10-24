package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"bypass/internal/platform"
	"bypass/internal/search"
	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
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
	add("close-vault", "File", "Close Vault", "", closeReason, "", func() tea.Cmd {
		return m.leaveEditor(func() tea.Cmd { m.ws.CloseVault(); m.normalize(); m.syncPrivacyState(); return m.clearClipboard() })
	})
	add("lock", "Vault", "Lock Vault", "", needVault, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(false) }) })
	allReason := ""
	if !m.anyUnlocked() {
		allReason = "No unlocked vaults"
	}
	add("lock-all", "Vault", "Lock All Vaults", "Ctrl+L", allReason, "", func() tea.Cmd { return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(true) }) })
	add("reload", "Vault", "Reload Vault", "Ctrl+R", needVault, "", func() tea.Cmd { return m.requestReload(s.ID) })
	readLabel := "Enable Read Only"
	if s != nil && s.ReadOnly {
		readLabel = "Enable Writes"
	} else {
		readLabel = "Read Only"
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
	add("copy-focused", "Field", "Copy", "c", focusedCopyReason, "", func() tea.Cmd { return m.copyField("") })
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
	add("totp", "Field", "TOTP", "", totpReason, "", func() tea.Cmd { return m.leaveEditor(m.openTOTP) })
	add("attachments", "Entry", "Attachments…", "", attachmentReason, "", func() tea.Cmd { return m.leaveEditor(m.openAttachments) })
	add("history-view", "Entry", "Entry History…", "", historyReason, "", func() tea.Cmd { return m.leaveEditor(m.openHistory) })
	add("grand-view", "Field", "Large View", "G", grandReason, "", m.openGrandView)
	add("reveal", "Field", "Reveal", "v", needEntry, "", func() tea.Cmd { return m.toggleReveal() })
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
	add("jump", "View", "Jump to Pane / Field", "J", needVault, "", func() tea.Cmd { m.jump = true; return nil })
	add("zoom", "View", "Zoom / Restore Focused Pane", "z", needVault, "", func() tea.Cmd { v.Zoom = !v.Zoom; return nil })
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
	add("theme", "Settings", "Theme", "", "", "themes", nil)
	add("appearance", "Settings", "Layout", "", "", "appearance", nil)
	add("ascii", "Settings", "Toggle ASCII UI", "", "", "", func() tea.Cmd { m.settings.ASCII = !m.settings.ASCII; return m.persistPreferences() })
	add("set-pin", "Privacy", "Change PIN", "", allReason, "", func() tea.Cmd { m.configurePIN(false); return nil })
	pinReason := allReason
	if pinReason == "" && !m.gate.Configured() {
		pinReason = "Set a session PIN first"
	}
	add("protect", "Vault", "Lock with PIN", "Ctrl+K", pinReason, "", m.protectScreen)
	add("help", "Help", "Shortcuts", "?", "", "", func() tea.Cmd { m.help = true; m.helpOffset = 0; m.helpQuery.clear(); return nil })
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
	if p.query.value() != "" {
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
	}
	if p.scope == "actions" {
		choices = orderCommandChoices(choices)
	}
	return choices
}
func (m *Model) paletteRows(choices []command) ([]paletteRow, int) {
	p := m.palette
	c := m.colors()
	var rows []paletteRow
	selectedRow := 0
	group := ""
	for i, choice := range choices {
		section := choice.group
		if p != nil && p.scope == "actions" {
			section = commandSection(choice.group)
		}
		if section != group {
			rows = append(rows, paletteRow{text: c.muted.Bold(true).Render(section), choice: -1})
			group = section
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

func orderCommandChoices(choices []command) []command {
	ordered := make([]command, 0, len(choices))
	for _, section := range []string{"VAULTS", "VIEW", "ENTRIES", "SECRETS", "SETTINGS"} {
		var bucket []command
		for _, choice := range choices {
			if commandSection(choice.group) == section {
				bucket = append(bucket, choice)
			}
		}
		sort.SliceStable(bucket, func(i, j int) bool {
			return commandPriority(bucket[i].id) < commandPriority(bucket[j].id)
		})
		ordered = append(ordered, bucket...)
	}
	return ordered
}

func commandPriority(id string) int {
	order := []string{
		// VAULTS: open/close first, then setup, security and maintenance.
		"protect", "open", "close-vault", "new-vault", "lock-all", "lock", "reload",
		"database-info", "database-settings", "credentials", "read-only", "quit",
		// VIEW: tab lifecycle and navigation, then search and layout controls.
		"duplicate", "close-view", "previous-tab", "next-tab",
		"search", "jump", "zoom",
		// ENTRIES: create and edit first, then organize and review.
		"new", "edit", "group", "move", "restore", "delete", "history-view",
		"attachments", "save", "cancel-edit",
		// SECRETS: copy common credentials first, then field tools.
		"copy-focused", "reveal", "generator", "grand-view", "totp",
		// SETTINGS: appearance first, then privacy and help.
		"theme", "appearance", "ascii", "set-pin", "protect", "help",
	}
	for rank, candidate := range order {
		if candidate == id {
			return rank
		}
	}
	return len(order)
}

func commandSection(group string) string {
	switch group {
	case "File", "App", "Vault":
		return "VAULTS"
	case "Tabs", "View":
		return "VIEW"
	case "Entry", "Editor":
		return "ENTRIES"
	case "Field":
		return "SECRETS"
	case "Settings", "Privacy", "Help":
		return "SETTINGS"
	default:
		return group
	}
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
	title := map[string]string{"actions": "Commands", "themes": "Theme", "appearance": "Choose Appearance", "files": "Open Vault", "destination": "Move / Restore Entry"}[p.scope]
	if p.context {
		title = "Item Actions"
	}
	if len(p.stack) > 0 && p.scope != "themes" {
		title = "Commands › " + title
	}
	body := []string{""}
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

func (m *Model) paletteLines() []string {
	title, body, footer := m.paletteBody()
	choices := m.paletteChoices()
	count := paletteCountLabel(m.palette.scope, len(choices))
	styles := m.colors()
	if !strings.EqualFold(m.settings.Appearance, "compact") {
		width := max(1, min(86, m.width-4)-4)
		header := dialogrender.FilterHeader(title, safe(m.palette.query.value()), m.palette.query.cursor, "type to filter", count, width, false, m.settings.ASCII, filterDialogStyles(styles))
		return m.overlay(title, body, footer, min(86, m.width-4), m.palette.x, m.palette.y, header)
	}
	header := dialogrender.FilterHeader(title, safe(m.palette.query.value()), m.palette.query.cursor, "type to filter", count, 0, true, m.settings.ASCII, filterDialogStyles(styles))
	return m.overlay(title, body, footer, min(86, m.width-2), m.palette.x, m.palette.y, header)
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
	}
	if count == 1 {
		label = singular
	}
	return fmt.Sprintf("%d %s", count, label)
}

func filterDialogStyles(c *styleSet) dialogrender.FilterStyles {
	return dialogrender.FilterStyles{
		Text: c.base, Placeholder: c.muted, Fill: c.inactive, Caret: c.active,
		Count: c.muted, Accent: c.accent, Divider: c.lineStyle, Base: c.base,
	}
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
	return lines
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
