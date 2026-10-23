package ui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"keepassui/internal/vault"
)

func (m *Model) pointer(msg tea.MouseMsg) tea.Cmd {
	if m.help || m.width < 65 || m.height < 16 {
		return nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
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
		capacity := max(1, m.height-16)
		start := max(0, m.fileIndex-capacity+1)
		visible := min(len(m.files)-start, capacity)
		bodyLen := 3 + max(1, visible)
		if m.err != "" {
			bodyLen += 2
		}
		top := max(0, (m.height-(min(bodyLen, max(1, m.height-8))+4))/2)
		index := msg.Y - top - 5 + start
		if index >= start && index < len(m.files) && index < start+visible {
			m.fileIndex = index
			m.inputs[0].set(m.files[index].path)
		}
		return nil
	}
	if m.ws.ActiveSession() == nil {
		top := max(1, (m.height-18)/2)
		if msg.Y == top+5 {
			return m.openPicker()
		}
		index := msg.Y - top - 10
		if index >= 0 && index < min(5, len(m.recent)) {
			m.recentIndex = index
			m.beginUnlock(m.recent[index])
		}
		return nil
	}
	if msg.Y == 2 {
		x := 0
		for i, label := range m.tabLabels() {
			width := lipgloss.Width(label)
			if msg.X >= x && msg.X < x+width {
				m.ws.Switch(i)
				return nil
			}
			x += width + 1
		}
		return nil
	}
	m.mouse(msg)
	return nil
}

func (m *Model) navigation() []navItem {
	items := []navItem{{label: "All Entries", kind: "all"}, {label: "Favorites", kind: "favorites"}, {label: "Recently Modified", kind: "recent"}, {label: "Recycle Bin", kind: "trash"}}
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return items
	}
	for _, g := range s.DB.Groups {
		if g.Recycled {
			continue
		}
		depth := strings.Count(g.Path, "/")
		items = append(items, navItem{label: g.Name, group: g.UUID, depth: depth, kind: "group"})
	}
	return items
}

func (m *Model) entriesFor(nav navItem, query string) []vault.Entry {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return nil
	}
	var entries []vault.Entry
	query = strings.ToLower(query)
	for _, e := range s.DB.Entries {
		if (nav.kind == "trash") != e.Recycled {
			continue
		}
		if nav.kind == "group" && e.GroupUUID != nav.group {
			continue
		}
		if nav.kind == "favorites" {
			found := false
			for _, tag := range e.Tags {
				if strings.EqualFold(tag, "Favorite") {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if query != "" {
			searchable := true
			for _, g := range s.DB.Groups {
				if g.UUID == e.GroupUUID {
					searchable = g.SearchEnabled
					break
				}
			}
			if !searchable {
				continue
			}
			matched := false
			for _, name := range []string{"Title", "UserName", "URL", "Notes"} {
				if !e.IsProtected(name) && strings.Contains(strings.ToLower(e.Get(name)), query) {
					matched = true
					break
				}
			}
			if strings.Contains(strings.ToLower(strings.Join(e.Tags, " ")), query) {
				matched = true
			}
			if !matched {
				continue
			}
		}
		entries = append(entries, e)
	}
	if nav.kind == "recent" {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Modified.After(entries[j].Modified) })
	}
	return entries
}

func (m *Model) entries() []vault.Entry {
	v := m.ws.ActiveView()
	if v == nil {
		return nil
	}
	items := m.navigation()
	index := min(max(0, v.NavIndex), len(items)-1)
	return m.entriesFor(items[index], v.Query)
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
	if v == nil {
		return
	}
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
		v.NavIndex = min(max(0, v.NavIndex+delta), len(m.navigation())-1)
		v.GroupUUID = m.navigation()[v.NavIndex].group
		v.SelectedUUID = ""
		v.EntryOffset = 0
		v.DetailOffset = 0
		m.normalize()
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
		v.DetailOffset = max(0, v.DetailOffset+delta)
	}
}

func (m *Model) mouse(msg tea.MouseMsg) {
	if m.screen != browse || m.searching || m.help || m.width < 65 || m.height < 16 {
		return
	}
	v := m.ws.ActiveView()
	s := m.ws.ActiveSession()
	if v == nil || s == nil || s.DB == nil {
		return
	}
	lw, mw, _ := m.paneWidths()
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		if msg.Button == tea.MouseButtonWheelUp {
			m.move(-1)
		} else {
			m.move(1)
		}
		return
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return
	}
	if msg.Y < 5 || msg.Y >= m.height-2 {
		return
	}
	switch {
	case msg.X < lw:
		v.Focus = 0
		index := (msg.Y - 5) / 2
		// Sidebar viewport is derived from current selection, matching renderer.
		visible := max(1, (m.height-8)/2)
		start := max(0, v.NavIndex-visible+1)
		index += start
		if index < len(m.navigation()) {
			v.NavIndex = index
			v.GroupUUID = m.navigation()[index].group
			v.SelectedUUID = ""
			v.DetailOffset = 0
			m.normalize()
		}
	case msg.X < lw+mw+1:
		v.Focus = 1
		index := msg.Y - 5 + v.EntryOffset
		entries := m.entries()
		if index >= 0 && index < len(entries) {
			v.SelectedUUID = entries[index].UUID
			v.DetailOffset = 0
		}
	default:
		v.Focus = 2
	}
}

func (m *Model) restoreGroup() {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return
	}
	items := m.navigation()
	for i := range m.ws.Views {
		v := &m.ws.Views[i]
		if v.SessionID != s.ID {
			continue
		}
		if v.NavIndex < 4 {
			continue
		}
		v.NavIndex = 0
		for j, item := range items {
			if item.kind == "group" && item.group == v.GroupUUID {
				v.NavIndex = j
				break
			}
		}
	}
}

func (m *Model) adjustViewport() {
	v := m.ws.ActiveView()
	s := m.ws.ActiveSession()
	if v == nil || s == nil || s.DB == nil {
		return
	}
	entries := m.entries()
	selected := 0
	for i, e := range entries {
		if e.UUID == v.SelectedUUID {
			selected = i
			break
		}
	}
	capacity := max(1, m.height-7)
	if selected < v.EntryOffset {
		v.EntryOffset = selected
	}
	if selected >= v.EntryOffset+capacity {
		v.EntryOffset = selected - capacity + 1
	}
	v.EntryOffset = min(v.EntryOffset, max(0, len(entries)-capacity))
	_, _, rw := m.paneWidths()
	v.DetailOffset = min(max(0, v.DetailOffset), max(0, len(m.detailContent(rw))-max(1, m.height-6)))
}

func visibleField(e vault.Entry, name string) string {
	if e.IsProtected(name) || name == "Password" {
		return "••••••••"
	}
	return safe(e.Get(name))
}
