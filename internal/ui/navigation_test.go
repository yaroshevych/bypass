package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAcceptedSearchFiltersWithoutSearchHistory(t *testing.T) {
	m, _ := operationModel(t)
	v := m.ws.ActiveView()
	v.Focus = 1
	enterGlobalSearch := func() {
		t.Helper()
		m.key(tea.KeyMsg{Type: tea.KeyCtrlF})
		m.key(tea.KeyMsg{Type: tea.KeyCtrlU})
		m.key(key("Alpha"))
		m.key(key("enter"))
	}
	enterGlobalSearch()
	enterGlobalSearch()
	if v.Query != "Alpha" || v.Focus != 1 || v.NavKey != "all" {
		t.Fatalf("accepted search did not leave its filter applied: %#v", *v)
	}
	for _, cmd := range m.commands() {
		if strings.Contains(strings.ToLower(cmd.label), "search history") {
			t.Fatalf("search-history command remains: %#v", cmd)
		}
	}
	for _, cmd := range m.commands() {
		if cmd.id == "history" || cmd.id == "clear-history" {
			t.Fatalf("search-history command remains: %#v", cmd)
		}
	}
}

func TestBinSearchIncludesEntriesFromSearchDisabledGroups(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/bin-search.kdbx")
	db.Groups[0].SearchEnabled = false
	db.Entries[0].Recycled = true
	db.Entries[0].Fields[1].Value = "Mira Wilson"
	m.ws.Open(db)
	m.normalize()
	v := m.ws.ActiveView()
	v.NavKey = "trash"
	m.remapNav()
	v.Query = "mir"
	entries := m.entries()
	if len(entries) != 1 || entries[0].UUID != "a" {
		t.Fatalf("Bin search for username returned %#v", entries)
	}
}

func TestSlashAlwaysSearchesTheEntryList(t *testing.T) {
	for _, tt := range []struct {
		name  string
		width int
	}{
		{name: "wide", width: 140},
		{name: "narrow", width: 70},
	} {
		for _, initialFocus := range []int{0, 1, 2} {
			focusName := []string{"sidebar", "entries", "details"}[initialFocus]
			t.Run(tt.name+"/"+focusName, func(t *testing.T) {
				m, _ := operationModel(t)
				m.width, m.height = tt.width, 30
				v := m.ws.ActiveView()
				v.Focus = initialFocus
				m.key(key("/"))
				if !m.searching || m.searchColumn != 1 || v.Focus != 1 {
					t.Fatalf("slash search state = searching %v, column %d, focus %d", m.searching, m.searchColumn, v.Focus)
				}
				if tt.name == "narrow" && m.layout().Panes[0].Focus != 1 {
					t.Fatalf("narrow layout did not navigate to the entry list: %+v", m.layout())
				}
				m.key(key("esc"))
				if m.searching || v.Focus != initialFocus {
					t.Fatalf("cancel did not restore the original focus %d: %#v", initialFocus, *v)
				}
			})
		}
	}
}

func TestJumpToPasswordDoesNotRevealIt(t *testing.T) {
	m, _ := operationModel(t)
	v := m.ws.ActiveView()
	v.Focus = 1
	v.SelectedUUID = "a"
	m.key(key("J"))
	m.key(key("p"))
	if v.Focus != 2 || m.focusedField() != "Password" {
		t.Fatalf("jump focus = %d/%q, want details/password", v.Focus, m.focusedField())
	}
	if m.revealed("a", "Password") || len(m.revealValue) != 0 {
		t.Fatal("jumping to Password revealed or retained its secret")
	}
	view := m.View()
	if !strings.Contains(view, "••••••••") || strings.Contains(view, "PASSWORD-DO-NOT-RENDER") {
		t.Fatal("password details were not masked after the jump")
	}
}
