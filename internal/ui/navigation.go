package ui

import (
	"sort"
	"strings"
	"time"

	"bypass/internal/search"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type entrySearchCache struct {
	db      *vault.Database
	key     string
	entries []vault.Entry
}

type sidebarRow struct {
	index int // -1: a non-selectable section header or spacer
	label string
	count bool
}

func (m *Model) recordItemUse() {
	if e := m.selected(); e != nil {
		if s := m.ws.ActiveSession(); s != nil {
			m.ws.RecordAccess(s.ID, e.UUID, m.now())
			m.searchCache = nil
		}
	}
}

func (m *Model) sidebarRows() []sidebarRow {
	items := m.navigation()
	var rows []sidebarRow
	sections := []string{"Views"}
	for _, item := range items {
		if item.kind == "tag" {
			sections = append(sections, "Tags")
			break
		}
	}
	sections = append(sections, "Groups")
	for _, section := range sections {
		if section != "Views" {
			rows = append(rows, sidebarRow{index: -1})
		}
		rows = append(rows, sidebarRow{index: -1, label: section})
		for i, item := range items {
			category := "Views"
			if item.kind == "tag" {
				category = "Tags"
			}
			if item.kind == "group" {
				category = "Groups"
			}
			if category == section {
				rows = append(rows, sidebarRow{index: i})
				if m.navRowHeight() == 2 {
					rows = append(rows, sidebarRow{index: i, count: true})
				}
			}
		}
	}
	return rows
}

func (m *Model) sidebarViewport(height int) ([]sidebarRow, int) {
	rows := m.sidebarRows()
	v := m.ws.ActiveView()
	if v == nil {
		return rows, 0
	}
	selected := 0
	for i, row := range rows {
		if row.index == v.NavIndex {
			selected = i
			break
		}
	}
	return rows, viewportOffset(selected, len(rows), max(1, height), v.NavOffset)
}

func (m *Model) allNavigation() []navItem {
	items := []navItem{{label: "All Entries", kind: "all", key: "all"}, {label: "Recent", kind: "recent", key: "recent"}, {label: "Bin", kind: "trash", key: "trash"}}
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return items
	}
	tags := map[string]bool{}
	for _, e := range s.DB.Entries {
		if !e.Recycled {
			for _, tag := range e.Tags {
				tag = strings.TrimSpace(tag)
				if tag != "" && !strings.EqualFold(tag, "Favorite") {
					tags[tag] = true
				}
			}
		}
	}
	var tagNames []string
	for tag := range tags {
		tagNames = append(tagNames, tag)
	}
	sort.Strings(tagNames)
	for _, tag := range tagNames {
		items = append(items, navItem{label: tag, kind: "tag", value: tag, key: "tag:" + tag})
	}
	byID := make(map[string]vault.Group)
	children := make(map[string][]vault.Group)
	for _, g := range s.DB.Groups {
		byID[g.UUID] = g
		children[g.ParentUUID] = append(children[g.ParentUUID], g)
	}
	visited := make(map[string]bool)
	var skipBranch func(vault.Group)
	skipBranch = func(g vault.Group) {
		if visited[g.UUID] {
			return
		}
		visited[g.UUID] = true
		for _, child := range children[g.UUID] {
			skipBranch(child)
		}
	}
	var visit func(vault.Group, int)
	visit = func(g vault.Group, depth int) {
		if visited[g.UUID] {
			return
		}
		if g.Recycled || g.UUID == s.DB.RecycleBinUUID || s.DB.RecycleBinEnabled && !s.DB.RecycleBinVerified && strings.EqualFold(g.Name, "Recycle Bin") {
			skipBranch(g)
			return
		}
		visited[g.UUID] = true
		items = append(items, navItem{label: g.Name, kind: "group", group: g.UUID, key: "group:" + g.UUID, depth: depth})
		for _, child := range children[g.UUID] {
			visit(child, depth+1)
		}
	}
	for _, g := range s.DB.Groups {
		if _, hasParent := byID[g.ParentUUID]; !hasParent {
			visit(g, 0)
		}
	}
	for _, g := range s.DB.Groups {
		visit(g, 0)
	}
	return items
}
func (m *Model) buildNavigation() []navItem {
	return m.allNavigation()
}
func (m *Model) navByKey(key string) navItem {
	for _, item := range m.allNavigation() {
		if item.key == key {
			return item
		}
	}
	return navItem{label: "All Entries", kind: "all", key: "all"}
}
func (m *Model) currentNav() navItem {
	v := m.ws.ActiveView()
	if v == nil {
		return navItem{kind: "all", key: "all"}
	}
	items := m.navigation()
	if len(items) == 0 {
		return navItem{kind: "none", key: "none"}
	}
	return items[min(max(0, v.NavIndex), len(items)-1)]
}
func (m *Model) selectNav(index int) {
	v := m.ws.ActiveView()
	items := m.navigation()
	if v == nil || len(items) == 0 {
		return
	}
	v.NavIndex = min(max(0, index), len(items)-1)
	item := items[v.NavIndex]
	v.NavKey = item.key
	v.GroupUUID = item.group
	v.SelectedUUID = ""
	v.EntryOffset = 0
	v.DetailOffset = 0
	v.DetailManual = false
	m.normalize()
}
func (m *Model) remapNav() {
	v := m.ws.ActiveView()
	if v == nil {
		return
	}
	items := m.navigation()
	if len(items) == 0 {
		v.NavIndex = 0
		return
	}
	if v.NavKey != "" {
		for i, item := range items {
			if item.key == v.NavKey {
				v.NavIndex = i
				return
			}
		}
		v.NavIndex = 0
	}
	v.NavIndex = min(max(0, v.NavIndex), len(items)-1)
	v.NavKey = items[v.NavIndex].key
	v.GroupUUID = items[v.NavIndex].group
}
func (m *Model) filterEntries(nav navItem, query string) []vault.Entry {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return nil
	}
	if nav.kind == "none" {
		return nil
	}
	cacheKey := nav.key + "\x00" + query
	if query != "" && m.searchCache != nil && m.searchCache.db == s.DB && m.searchCache.key == cacheKey {
		return m.searchCache.entries
	}
	searchable := map[string]bool{}
	for _, g := range s.DB.Groups {
		searchable[g.UUID] = g.SearchEnabled
	}
	var entries []vault.Entry
	for _, e := range s.DB.Entries {
		if (nav.kind == "trash") != e.Recycled {
			continue
		}
		switch nav.kind {
		case "group":
			if e.GroupUUID != nav.group {
				continue
			}
		case "tag":
			found := false
			for _, tag := range e.Tags {
				if tag == nav.value {
					found = true
				}
			}
			if !found {
				continue
			}
		case "tags":
			if len(e.Tags) == 0 {
				continue
			}
		case "accessed", "frequent":
			if s.Usage[e.UUID].Count == 0 {
				continue
			}
		}
		// Bin is an explicit user-selected view; keep its entries searchable
		// even when their former group was excluded from ordinary searches.
		if query != "" && nav.kind != "trash" && !searchable[e.GroupUUID] {
			continue
		}
		entries = append(entries, e)
	}
	if nav.kind == "trash" {
		for _, group := range s.DB.Groups {
			if group.Recycled && group.UUID != s.DB.RecycleBinUUID && group.ParentUUID == s.DB.RecycleBinUUID {
				entries = append(entries, vault.Entry{UUID: group.UUID, GroupUUID: group.ParentUUID, Recycled: true, OpaqueGroup: true, Fields: []vault.Field{{Name: "Title", Value: group.Name + "/"}}})
			}
		}
	}
	switch nav.kind {
	case "recent":
		last := func(e vault.Entry) time.Time {
			if used := s.Usage[e.UUID].Last; used.After(e.Modified) {
				return used
			}
			return e.Modified
		}
		sort.SliceStable(entries, func(i, j int) bool { return last(entries[i]).After(last(entries[j])) })
	case "accessed":
		sort.SliceStable(entries, func(i, j int) bool { return s.Usage[entries[i].UUID].Last.After(s.Usage[entries[j].UUID].Last) })
	case "frequent":
		sort.SliceStable(entries, func(i, j int) bool {
			a, b := s.Usage[entries[i].UUID], s.Usage[entries[j].UUID]
			if a.Count != b.Count {
				return a.Count > b.Count
			}
			return a.Last.After(b.Last)
		})
	}
	if query == "" {
		return entries
	}
	var docs []search.Document
	byID := map[string]vault.Entry{}
	for _, e := range entries {
		get := func(name string) string {
			if e.IsProtected(name) || name == "Password" || (s.DB.Unlocked() && name == "Notes") {
				return ""
			}
			return e.Get(name)
		}
		docs = append(docs, search.Document{ID: e.UUID, Title: get("Title"), Username: get("UserName"), URL: get("URL"), Notes: get("Notes"), Group: e.GroupPath, Tags: e.Tags})
		byID[e.UUID] = e
	}
	results, err := search.Find(query, docs)
	if err != nil {
		return nil
	}
	entries = nil
	for _, result := range results {
		entries = append(entries, byID[result.ID])
	}
	return entries
}

func (m *Model) refreshSearchCache() {
	s, v := m.ws.ActiveSession(), m.ws.ActiveView()
	if s == nil || s.DB == nil || v == nil || v.Query == "" || m.gate.Protected() {
		return
	}
	nav := m.currentNav()
	key := nav.key + "\x00" + v.Query
	if m.searchCache == nil || m.searchCache.db != s.DB || m.searchCache.key != key {
		m.searchCache = &entrySearchCache{db: s.DB, key: key, entries: m.filterEntries(nav, v.Query)}
	}
}

func (m *Model) beginSearch(global bool) tea.Cmd {
	v := m.ws.ActiveView()
	if v == nil {
		return nil
	}
	origin := *v
	m.searchOrigin = &origin
	m.searchError = ""
	m.hideReveal()
	if global {
		v.NavKey = "all"
		v.GroupUUID = ""
		m.remapNav()
	}
	// Slash always searches the central entry list. In narrow mode, changing
	// Focus also navigates the single visible pane to the entry list.
	v.Focus = 1
	m.searching = true
	m.previousQuery = v.Query
	m.query.set(m.previousQuery)
	return nil
}
func (m *Model) searchKey(k tea.KeyMsg) tea.Cmd {
	v := m.ws.ActiveView()
	if v == nil {
		return nil
	}
	switch k.String() {
	case "esc":
		if m.searchOrigin != nil {
			*v = *m.searchOrigin
		}
		m.endSearch()
		m.normalize()
	case "enter":
		if _, err := search.Find(m.query.value(), nil); err != nil {
			m.searchError = "Invalid search. Check fields and quotes."
			return nil
		}
		m.endSearch()
		m.remapNav()
		m.normalize()
	default:
		m.query.update(k)
		v.Query = m.query.value()
		m.searchError = ""
		m.remapNav()
		m.normalize()
	}
	return nil
}
func (m *Model) endSearch() {
	m.searching = false
	m.searchOrigin = nil
	m.query.clear()
	m.previousQuery = ""
	m.searchError = ""
}
func (m *Model) clearFilter() bool {
	v := m.ws.ActiveView()
	if v == nil {
		return false
	}
	if v.Query != "" {
		v.Query = ""
		m.searchCache = nil
		m.normalize()
		return true
	}
	return false
}

func (m *Model) highlightText(text, query, field string) string {
	text = safe(text)
	positions := search.Positions(query, field, text)
	if len(positions) == 0 {
		return text
	}
	marked := map[int]bool{}
	for _, pos := range positions {
		marked[pos] = true
	}
	var b strings.Builder
	for i, r := range []rune(text) {
		if marked[i] {
			b.WriteString(m.colors().inactive.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
