package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSearchHistoryRepeatApplyAndRemove(t *testing.T) {
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
	session := m.ws.ActiveSession()
	if len(session.Searches) != 1 || session.Searches[0].Query != "Alpha" {
		t.Fatalf("first accepted query was not recorded: %#v", session.Searches)
	}
	searchID := session.Searches[0].ID
	enterGlobalSearch()
	if len(session.Searches) != 1 || session.Searches[0].ID != searchID {
		t.Fatalf("repeated query created a duplicate history record: %#v", session.Searches)
	}

	v.Query = ""
	v.Focus = 0
	m.openPalette("history", false)
	if got := m.paletteChoices(); len(got) != 1 || got[0].id != searchID {
		t.Fatalf("history palette choices = %#v", got)
	}
	m.executePalette()
	if v.Query != "Alpha" || v.Focus != 1 || v.NavKey != "all" {
		t.Fatalf("replaying search did not restore its query and view: %#v", *v)
	}
	if len(session.Searches) != 1 || session.Searches[0].ID != searchID {
		t.Fatalf("replaying search duplicated history: %#v", session.Searches)
	}

	v.Focus = 0
	for _, cmd := range m.commands() {
		if cmd.id == "clear-history" {
			cmd.run()
			break
		}
	}
	if len(session.Searches) != 0 {
		t.Fatalf("removing history did not return to the history list: searches=%#v view=%#v", session.Searches, *v)
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
