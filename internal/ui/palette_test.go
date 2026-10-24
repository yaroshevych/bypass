package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestPaletteThemePreviewCancelRestoresStylesAndViewFilters(t *testing.T) {
	m, _ := operationModel(t)
	v := m.ws.ActiveView()
	v.Focus = 2
	v.Query = "Alpha"
	v.NavFilter = "Root"
	v.DetailFilter = "Password"
	beforeView := *v
	beforeSettings := m.settings
	beforeStyles := m.styles

	m.openPalette("actions", false)
	m.palette.query.set("Choose Theme")
	m.refreshPalette()
	if got := m.paletteChoices(); len(got) != 1 || got[0].next != "themes" {
		t.Fatalf("theme command choices = %#v", got)
	}
	m.executePalette()
	m.palette.query.set("Nord")
	m.refreshPalette()
	m.previewChoice()
	if m.settings.Theme != "nord" || reflect.DeepEqual(m.styles, beforeStyles) {
		t.Fatal("theme selection did not preview the requested theme")
	}

	m.key(key("esc"))
	if m.palette == nil || m.palette.scope != "actions" {
		t.Fatal("Escape did not return to the parent palette")
	}
	if m.settings != beforeSettings || !reflect.DeepEqual(m.styles, beforeStyles) {
		t.Fatal("canceling theme preview did not restore original settings and styles")
	}
	m.key(key("esc"))
	if m.palette != nil {
		t.Fatal("Escape did not close the parent palette")
	}
	if *m.ws.ActiveView() != beforeView {
		t.Fatal("palette preview changed active view focus or filters")
	}
}

func TestCtrlLLocksAllAndCurrentVaultRemainsInPalette(t *testing.T) {
	m, _ := operationModel(t)
	first := m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/lock-all-second.kdbx"))
	m.normalize()
	second := m.ws.ActiveSession()
	if first.ID == second.ID || first.DB == nil || second.DB == nil {
		t.Fatal("test requires two unlocked vaults")
	}
	header := ansi.Strip(m.workspaceLines()[0])
	if !strings.Contains(header, "2 entries · ctrl+l lock") {
		t.Fatalf("header lock hint = %q", header)
	}
	commands := m.commands()
	var current, all *command
	for i := range commands {
		cmd := &commands[i]
		if cmd.id == "lock" {
			current = cmd
		}
		if cmd.id == "lock-all" {
			all = cmd
		}
	}
	if current == nil || current.key != "" || all == nil || all.key != "Ctrl+L" {
		t.Fatalf("lock command bindings: current=%+v all=%+v", current, all)
	}
	m.key(key("ctrl+l"))
	if first.DB != nil || second.DB != nil {
		t.Fatal("Ctrl+L did not lock all open vaults")
	}

	m.ws.Open(fixture("/tmp/lock-current-first.kdbx"))
	m.normalize()
	first = m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/lock-current-second.kdbx"))
	m.normalize()
	second = m.ws.ActiveSession()
	var lockCurrent func() tea.Cmd
	for _, cmd := range m.commands() {
		if cmd.id == "lock" {
			lockCurrent = cmd.run
			break
		}
	}
	if lockCurrent == nil {
		t.Fatal("Lock Current Vault is missing from the command palette")
	}
	if cmd := lockCurrent(); cmd != nil {
		cmd()
	}
	if first.DB == nil || second.DB != nil {
		t.Fatal("command palette did not lock only the current vault")
	}
}
