package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"keepassui/internal/vault"
)

func (m *Model) pointer(msg tea.MouseMsg) tea.Cmd {
	if m.layout().Mode == "tiny" {
		return nil
	}
	if m.gate.Protected() {
		return nil
	}
	if m.dialog != nil {
		m.dragging = 0
		if m.dialog.kind == "destination" && !m.writing && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.destinationPointer(msg)
		}
		return nil
	}
	if m.lifecycle != nil {
		return m.lifecyclePointer(msg)
	}
	if cmd, handled := m.extrasPointer(msg); handled {
		return cmd
	}
	if m.grand {
		return m.grandPointer(msg)
	}
	if m.palette != nil {
		return m.palettePointer(msg)
	}
	if m.jump {
		return nil
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
		switch m.editor.pointer(msg.Y-target.Y, target.Width, target.Height) {
		case editorSave:
			return m.saveEditor()
		case editorCancel:
			return m.leaveEditor(func() tea.Cmd { return nil })
		case editorGenerate:
			m.openGenerator()
		}

		return nil
	}
	if m.help || m.width < 48 || m.height < 14 {
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
		bodyLen := 6
		if m.err != "" {
			bodyLen++
		}
		top := max(0, (m.height-(bodyLen+4))/2)
		index := msg.Y - top - 4
		if index >= 0 && index < 4 {
			m.inputFocus = index
			if index == 3 {
				m.noPassword = !m.noPassword
			}
		}
		return nil
	case chooseFile:
		start, capacity := m.fileViewport()
		visible := min(len(m.files)-start, capacity)
		bodyLen := 3 + max(1, visible)
		if m.err != "" {
			bodyLen += 2
		}
		top := max(0, (m.height-(min(bodyLen, max(1, m.height-8))+4))/2)
		if msg.Y == top+3 {
			m.pickerEditing = true
			return nil
		}
		boxWidth := min(m.width-6, 82)
		left := max(0, (m.width-boxWidth)/2)
		if msg.X < left || msg.X >= left+boxWidth {
			return nil
		}
		index := msg.Y - top - 5 + start
		if index >= start && index < len(m.files) && index < start+visible {
			m.fileIndex = index
			m.pickerEditing = false
		}
		return nil
	}
	if m.ws.ActiveSession() == nil {
		top := max(1, (m.height-18)/2)
		if msg.Y == top+5 {
			return m.openPicker()
		}
		extra := 0
		if m.canCreateVault() {
			if msg.Y == top+8 {
				return m.openCreateVault()
			}
			extra = 1
		}
		index := msg.Y - top - 10 - extra
		if index >= 0 && index < min(5, len(m.recent)) {
			m.recentIndex = index
			m.beginUnlock(m.recent[index])
		}
		return nil
	}
	if msg.Y == 2 {
		for _, span := range m.tabSpans() {
			if msg.X >= span.Start && msg.X < span.End {
				m.ws.Switch(span.Index)
				m.normalize()
				return nil
			}
		}
		return nil
	}
	m.mouse(msg)
	return nil
}

func (m *Model) fileViewport() (start, capacity int) {
	capacity = max(1, m.height-16)
	start = viewportOffset(m.fileIndex, len(m.files), capacity, m.fileOffset)
	return start, capacity
}

func (m *Model) destinationPointer(msg tea.MouseMsg) {
	d := m.dialog
	if d == nil || d.kind != "destination" {
		return
	}
	groups := d.filteredGroups()
	start, capacity := d.destinationViewport(m.height, groups)
	if len(groups) == 0 {
		return
	}
	boxWidth := min(m.width-6, 82)
	left := max(0, (m.width-boxWidth)/2)
	if msg.X < left || msg.X >= left+boxWidth {
		return
	}
	lines := m.dialogLines()
	top := 0
	for top < len(lines) && ansi.Strip(lines[top]) == "" {
		top++
	}
	firstGroupRow := top + 2 + len(d.message) + 3
	index := msg.Y - firstGroupRow + start
	visible := min(capacity, len(groups)-start)
	if index >= start && index < start+visible {
		d.selected = index
	}
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
		v.DetailManual = false
		v.FieldIndex = min(max(0, v.FieldIndex+delta), max(0, len(m.fieldNames())-1))
	}
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
		row := msg.Y - pane.Y - 1
		if row < 0 {
			return
		}
		m.selectNav(v.NavOffset + row/m.navRowHeight())
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
		v.DetailManual = false
		_, positions := m.detailLayout(pane.Width)
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
		start, _ := d.destinationViewport(m.height, groups)
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
	navCapacity := max(1, (navHeight-1)/m.navRowHeight())
	v.NavOffset = viewportOffset(v.NavIndex, len(m.navigation()), navCapacity, v.NavOffset)
	entries := m.entries()
	index := 0
	for i, e := range entries {
		if e.UUID == v.SelectedUUID {
			index = i
			break
		}
	}
	v.EntryOffset = viewportOffset(index, len(entries), max(1, listHeight-1), v.EntryOffset)
	lines, positions := m.detailLayout(detailWidth)
	if v.Focus == 2 && len(positions) > 0 && !v.DetailManual {
		v.FieldIndex = min(max(0, v.FieldIndex), len(positions)-1)
		row := positions[v.FieldIndex]
		if row < v.DetailOffset {
			v.DetailOffset = row
		}
		if row+1 >= v.DetailOffset+detailHeight {
			v.DetailOffset = max(0, row+2-detailHeight)
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
