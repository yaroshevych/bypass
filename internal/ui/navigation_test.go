package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSidebarOmitsEmptyTagsSectionAndFavoriteTag(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/sidebar-tags.kdbx")
	db.Entries[0].Tags = []string{"Favorite"}
	m.ws.Open(db)
	m.normalize()
	for _, item := range m.navigation() {
		if item.kind == "tag" {
			t.Fatalf("special Favorite tag appeared in tag navigation: %+v", item)
		}
	}
	for _, row := range m.sidebarRows() {
		if row.index < 0 && row.label == "Tags" {
			t.Fatal("empty Tags section was rendered")
		}
	}

	m.ws.ActiveSession().DB.Entries[0].Tags = []string{"Favorite", "Work"}
	foundWork, foundTagsSection := false, false
	for _, item := range m.navigation() {
		if item.kind == "tag" {
			if item.label == "Favorite" {
				t.Fatal("Favorite appeared as a tag despite its dedicated view")
			}
			foundWork = foundWork || item.label == "Work"
		}
	}
	for _, row := range m.sidebarRows() {
		foundTagsSection = foundTagsSection || row.index < 0 && row.label == "Tags"
	}
	if !foundWork || !foundTagsSection {
		t.Fatalf("regular tags missing: tag=%v section=%v", foundWork, foundTagsSection)
	}
}

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

func TestJumpRequiresEntryAndOnlyTargetsFields(t *testing.T) {
	m, _ := operationModel(t)
	v := m.ws.ActiveView()
	v.SelectedUUID = ""
	var jump *command
	commands := m.commands()
	for i := range commands {
		if commands[i].id == "jump" {
			jump = &commands[i]
			break
		}
	}
	if jump == nil || jump.group != "Entry" || jump.label != "Jump" || jump.reason != "Select an entry first" {
		t.Fatalf("Jump command without selection = %#v", jump)
	}
	m.key(key("J"))
	if m.jump {
		t.Fatal("Jump opened without a selected entry")
	}

	v.SelectedUUID = "a"
	m.key(key("J"))
	if !m.jump {
		t.Fatal("Jump did not open with a selected entry")
	}
	lines, handled := m.advancedScreen()
	if !handled {
		t.Fatal("Jump field chooser was not rendered")
	}
	text := strings.Join(lines, "\n")
	for _, field := range []string{"Password", "Username", "URL", "Notes"} {
		if !strings.Contains(text, field) {
			t.Errorf("Jump field chooser omits %q: %s", field, text)
		}
	}
	for _, paneTarget := range []string{"g Groups", "i Entries", "d Details"} {
		if strings.Contains(text, paneTarget) {
			t.Errorf("Jump field chooser includes pane target %q: %s", paneTarget, text)
		}
	}
	initialFocus := v.Focus
	m.jumpKey(key("i"))
	if !m.jump || v.Focus != initialFocus {
		t.Fatal("former pane target changed focus instead of leaving Jump open")
	}
}
