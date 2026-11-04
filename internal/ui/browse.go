package ui

import (
	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	"bypass/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) pointer(msg tea.MouseMsg) tea.Cmd {
	m.tabHovered = false
	if layer := m.activeLayer(); layer != layerWorkspace {
		if pointer := m.handlersFor(layer).pointer; pointer != nil {
			return pointer(msg)
		}
		return nil
	}
	if msg.Action == tea.MouseActionMotion && m.dragging == 0 {
		if !m.help && m.screen == browse && m.ws.ActiveSession() != nil {
			m.hoverTab, m.tabHovered = m.tabAt(msg.X, msg.Y)
		}
		return nil
	}
	if !m.help && !m.writing && m.screen == browse && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if index, hit := m.tabAt(msg.X, msg.Y); hit {
			return m.leaveEditor(func() tea.Cmd {
				if index == len(m.visibleTabIndexes()) {
					return m.duplicateTab()
				} else {
					m.switchTabPosition(index)
				}
				m.normalize()
				return nil
			})
		}
	}
	m.hideReveal()

	if m.writing {
		return nil
	}
	if m.resizePointer(msg) {
		return nil
	}
	if m.editor != nil {
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return nil
		}
		var target *paneRect
		for _, pane := range m.layout().Panes {
			if pane.Focus == 2 {
				copy := pane
				target = &copy
				break
			}
		}
		if target == nil || msg.X < target.X || msg.X >= target.X+target.Width || msg.Y < target.Y || msg.Y >= target.Y+target.Height {
			return m.leaveEditor(func() tea.Cmd { return m.pointer(msg) })
		}
		switch m.editor.pointer(msg.Y-target.Y, target.Width, target.Height, m.compactAppearance(), m.settings.ASCII) {
		case editorSave:
			return m.saveEditor()
		case editorCancel:
			return m.leaveEditor(func() tea.Cmd { return nil })
		case editorGenerate:
			m.openGenerator()
		case editorMove:
			return m.moveEditorEntry()
		case editorFavorite:
			m.toggleEditorFavorite()
		}

		return nil
	}
	if m.help {
		return nil
	}
	if msg.Action != tea.MouseActionPress {
		m.mouse(msg)
		return nil
	}
	if msg.Button == tea.MouseButtonRight {
		if m.screen != browse || m.ws.ActiveSession() == nil {
			return nil
		}
		copy := msg
		copy.Button = tea.MouseButtonLeft
		m.mouse(copy)
		m.openPalette("actions", true)
		if m.palette != nil {
			m.palette.x = msg.X
			m.palette.y = msg.Y
		}
		return nil
	}
	if msg.Button != tea.MouseButtonLeft {
		m.mouse(msg)
		return nil
	}
	switch m.screen {
	case unlock:
		if m.busy {
			return nil
		}
		opts := m.unlockOptions()
		targets := make([]int, len(opts.Body))
		for i := range targets {
			targets[i] = -1
			if i == 2 || i == 3 {
				targets[i] = 0
			}
		}
		switch dialogrender.HitTest(opts, msg.X, msg.Y, targets).Target {
		case 0:
			m.inputFocus = 0
		case 1:
			return m.startOpen()
		case 2:
			return m.unlockKey(tea.KeyMsg{Type: tea.KeyEscape})
		}
		return nil
	case chooseFile:
		recentCount := m.pickerRecentCount()
		opts := m.dialogOptions()
		opts.Width, opts.BodyRows, opts.StatusBar = min(m.width-6, 60), m.pickerBodyRows(), m.pickerStatusBar()
		opts.Body, opts.Error = nil, safe(m.err)
		body, targets := listContent(m.pickerContent())
		opts.Body = body
		target := dialogrender.HitTest(opts, msg.X, msg.Y, targets).Target
		if target == 0 {
			m.pickerEditing, m.pickerRecent = true, false
		} else if target > 0 && target <= recentCount {
			m.pickerEditing, m.pickerRecent = false, true
			m.pickerRecentIndex = target - 1
		} else if target > recentCount {
			m.fileIndex = target - 1 - recentCount
			m.pickerEditing, m.pickerRecent = false, false
		}
		return nil
	}
	if m.ws.ActiveSession() == nil {
		return m.welcomePointer(msg)
	}
	m.mouse(msg)
	return nil
}

func (m *Model) welcomePointer(msg tea.MouseMsg) tea.Cmd {
	opts, targets := m.welcomeOptions()
	target := dialogrender.HitTest(opts, msg.X, msg.Y, targets).Target
	if target < 0 {
		return nil
	}
	if target < m.landingRecentCount() {
		m.landingFocus, m.recentIndex = target, target
		return nil
	}
	if target == m.landingRecentCount() {
		m.landingFocus = m.landingRecentCount()
		return m.openPicker()
	}
	if target == m.landingRecentCount()+1 && m.canCreateVault() {
		m.landingFocus = m.landingRecentCount() + 1
		return m.openCreateVault()
	}
	return nil
}

func (m *Model) canAddTab() bool {
	s := m.ws.ActiveSession()
	return s != nil && s.DB != nil && len(m.ws.Views) < workspace.MaxTabs
}

func (m *Model) switchTabPosition(position int) bool {
	indexes := m.visibleTabIndexes()
	if position < 0 || position >= len(indexes) {
		return false
	}
	return m.ws.Switch(indexes[position])
}

func (m *Model) moveTab(delta int) bool {
	indexes := m.visibleTabIndexes()
	if len(indexes) == 0 {
		return false
	}
	position := 0
	for i, viewIndex := range indexes {
		if viewIndex == m.ws.Active {
			position = i
			break
		}
	}
	return m.switchTabPosition((position + len(indexes) + delta%len(indexes)) % len(indexes))
}

func (m *Model) duplicateTab() tea.Cmd {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return nil
	}
	if !m.ws.Duplicate() && len(m.ws.Views) >= workspace.MaxTabs {
		m.setNotice("max 9 tabs · close one to add another", false)
	}
	m.normalize()
	return nil
}

func (m *Model) tabAt(x, y int) (int, bool) {
	row := 2
	if m.compactAppearance() {
		row = 1
	}
	if y == row {
		for _, span := range m.tabSpans() {
			if x >= span.Start && x < span.End {
				return span.Index, true
			}
		}
	}
	return 0, false
}

func (m *Model) fileViewport() (start, capacity int) {
	available := max(0, m.pickerBodyRows()-m.pickerFixedRows())
	if m.pickerStatusBar() != "" {
		available--
	}
	if m.err != "" {
		opts := m.dialogOptions()
		opts.Width, opts.Error = min(m.width-6, 60), safe(m.err)
		rows, _ := dialogrender.PrepareBody(opts)
		available -= len(rows)
	}
	capacity = max(0, available)
	start = viewportOffset(m.fileIndex, len(m.files), max(1, capacity), m.fileOffset)
	return start, capacity
}

func (m *Model) pickerBodyRows() int {
	// Keep the picker sized for up to five Recent rows and fifteen Browse rows.
	return min(26, m.dialogContentCapacity(9, 1))
}

func (m *Model) pickerRecentCount() int {
	limit := 5
	if m.height > 0 {
		limit = min(limit, max(0, m.height-16))
	}
	return min(limit, len(m.recent))
}

func (m *Model) pickerStatusBar() string {
	if m.screen == chooseFile && m.pickerRecent {
		return m.recentRemovalStatusBar()
	}
	return ""
}

func (m *Model) pickerFixedRows() int {
	if count := m.pickerRecentCount(); count > 0 {
		return count + 7
	}
	return 6
}

func (m *Model) pickerPathRow() int {
	if m.pickerRecentCount() > 0 {
		return m.pickerRecentCount() + 4
	}
	return 3
}

func (m *Model) destinationPointer(msg tea.MouseMsg) tea.Cmd {
	d := m.dialog
	if d == nil || d.kind != "destination" {
		return nil
	}
	opts, targets := m.dialogContent()
	actionBase := len(d.filteredGroups())
	target := dialogrender.HitTest(opts, msg.X, msg.Y, targets).Target
	if target >= 0 && target < actionBase {
		d.selected, d.destinationFocus = target, 0
	} else if target >= actionBase {
		d.destinationFocus = target - actionBase + 1
		return m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

func (m *Model) navigation() []navItem { return m.buildNavigation() }

func (m *Model) entriesFor(nav navItem, query string) []vault.Entry {
	return m.filterEntries(nav, query)
}

func (m *Model) entries() []vault.Entry {
	v := m.ws.ActiveView()
	if v == nil {
		return nil
	}
	return m.entriesFor(m.currentNav(), v.Query)
}

func (m *Model) selected() *vault.Entry {
	v := m.ws.ActiveView()
	if v == nil {
		return nil
	}
	for _, e := range m.entries() {
		if e.UUID == v.SelectedUUID {
			return &e
		}
	}
	return nil
}

func (m *Model) normalize() {
	v := m.ws.ActiveView()
	if v == nil || m.ws.ActiveSession() == nil || m.ws.ActiveSession().DB == nil {
		return
	}
	m.remapNav()
	entries := m.entries()
	for _, e := range entries {
		if e.UUID == v.SelectedUUID {
			return
		}
	}
	v.SelectedUUID = ""
	v.EntryOffset = 0
	v.DetailOffset = 0
	if len(entries) > 0 {
		v.SelectedUUID = entries[0].UUID
	}
}

func (m *Model) move(delta int) {
	v := m.ws.ActiveView()
	if v == nil {
		return
	}
	switch v.Focus {
	case 0:
		m.selectNav(v.NavIndex + delta)
	case 1:
		entries := m.entries()
		if len(entries) == 0 {
			return
		}
		index := 0
		for i, e := range entries {
			if e.UUID == v.SelectedUUID {
				index = i
				break
			}
		}
		index = min(max(0, index+delta), len(entries)-1)
		v.SelectedUUID = entries[index].UUID
		v.DetailOffset = 0
	case 2:
		if m.scrollFocusedField(delta) {
			return
		}
		v.DetailManual = false
		v.FieldIndex = min(max(0, v.FieldIndex+delta), max(0, len(m.fieldNames())-1))
	}
}

// scrollFocusedField keeps a long field readable before moving to another field.
func (m *Model) scrollFocusedField(delta int) bool {
	if delta != 1 && delta != -1 {
		return false
	}
	v := m.ws.ActiveView()
	layout := m.layout()
	height, width := max(1, layout.BodyHeight), m.width
	if layout.Breadcrumb {
		height--
	}
	for _, pane := range layout.Panes {
		if pane.Focus == 2 {
			height, width = pane.Height, pane.Width
			break
		}
	}
	lines, positions, _ := m.detailViewportLayout(width, height)
	if v.FieldIndex < 0 || v.FieldIndex >= len(positions) {
		return false
	}
	start := positions[v.FieldIndex]
	end := len(lines)
	if v.FieldIndex+1 < len(positions) {
		end = positions[v.FieldIndex+1] - 2
	}
	if delta > 0 && end > v.DetailOffset+height {
		v.DetailManual = true
		v.DetailOffset++
		return true
	}
	anchor := max(0, start+2-height)
	if v.FieldIndex == 0 {
		anchor = 0
	}
	if delta < 0 && v.DetailManual && v.DetailOffset > anchor {
		v.DetailOffset--
		return true
	}
	return false
}

func (m *Model) mouse(msg tea.MouseMsg) {
	if m.screen != browse || m.searching || m.help || m.gate.Protected() {
		return
	}
	v := m.ws.ActiveView()
	session := m.ws.ActiveSession()
	if v == nil || session == nil || session.DB == nil {
		return
	}
	var target *paneRect
	for _, pane := range m.layout().Panes {
		if msg.X >= pane.X && msg.X < pane.X+pane.Width && msg.Y >= pane.Y && msg.Y < pane.Y+pane.Height {
			copy := pane
			target = &copy
			break
		}
	}
	if target == nil {
		return
	}
	pane := *target
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		v.Focus = pane.Focus
		delta := 1
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -1
		}
		if pane.Focus == 2 {
			v.DetailManual = true
			v.DetailOffset = max(0, v.DetailOffset+delta*3)
		} else {
			m.move(delta)
		}
		return
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return
	}
	v.Focus = pane.Focus
	switch pane.Focus {
	case 0:
		row := msg.Y - pane.Y
		rows, start := m.sidebarViewport(pane.Height)
		if index := start + row; index >= 0 && index < len(rows) && rows[index].index >= 0 {
			m.selectNav(rows[index].index)
		}
	case 1:
		index := msg.Y - pane.Y - 1 + v.EntryOffset
		entries := m.entries()
		if index >= 0 && index < len(entries) {
			v.SelectedUUID = entries[index].UUID
			v.DetailOffset = 0
			v.FieldIndex = 0
			v.DetailManual = false
		}
	case 2:
		_, positions, scrollbar := m.detailViewportLayout(pane.Width, pane.Height)
		if scrollbar && msg.X == pane.X+pane.Width-1 {
			return
		}
		v.DetailManual = false
		row := msg.Y - pane.Y + v.DetailOffset
		for i, start := range positions {
			if row >= start {
				v.FieldIndex = i
			}
		}
	}
}

func (m *Model) restoreGroup() {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return
	}
	for i := range m.ws.Views {
		v := &m.ws.Views[i]
		if v.SessionID != s.ID {
			continue
		}
		if v.NavKey == "" && v.NavIndex >= 4 && v.GroupUUID != "" {
			v.NavKey = "group:" + v.GroupUUID
		}
	}
	m.remapNav()
}

func (m *Model) adjustViewport() {
	if m.screen == chooseFile {
		start, _ := m.fileViewport()
		m.fileOffset = start
	}
	if d := m.dialog; d != nil && d.kind == "destination" {
		groups := d.filteredGroups()
		start, _ := m.destinationViewport(groups)
		d.offset = start
	}
	v := m.ws.ActiveView()
	session := m.ws.ActiveSession()
	if v == nil || session == nil || session.DB == nil {
		return
	}
	m.remapNav()
	layout := m.layout()
	height := max(1, layout.BodyHeight)
	if layout.Breadcrumb {
		height--
	}
	navHeight, listHeight, detailHeight, detailWidth := height, height, height, m.width
	for _, pane := range layout.Panes {
		switch pane.Focus {
		case 0:
			navHeight = pane.Height
		case 1:
			listHeight = pane.Height
		case 2:
			detailHeight = pane.Height
			detailWidth = pane.Width
		}
	}
	_, v.NavOffset = m.sidebarViewport(navHeight)
	entries := m.entries()
	index := 0
	for i, e := range entries {
		if e.UUID == v.SelectedUUID {
			index = i
			break
		}
	}
	v.EntryOffset = viewportOffset(index, len(entries), max(1, listHeight-1), v.EntryOffset)
	lines, positions, _ := m.detailViewportLayout(detailWidth, detailHeight)
	if v.Focus == 2 && len(positions) > 0 && !v.DetailManual {
		v.FieldIndex = min(max(0, v.FieldIndex), len(positions)-1)
		if v.FieldIndex == 0 {
			v.DetailOffset = 0
		} else {
			row := positions[v.FieldIndex]
			if row < v.DetailOffset {
				v.DetailOffset = row
			}
			if row+1 >= v.DetailOffset+detailHeight {
				v.DetailOffset = max(0, row+2-detailHeight)
			}
		}
	}
	v.DetailOffset = min(max(0, v.DetailOffset), max(0, len(lines)-detailHeight))
}

func visibleField(e vault.Entry, name string) string {
	if e.IsProtected(name) || name == "Password" {
		return "••••••••"
	}
	return safe(e.Get(name))
}
