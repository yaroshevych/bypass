package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestPaletteFilterPlaceholderHasStableHighlightedWidthAndRightAlignedCount(t *testing.T) {
	styles := stylesForTheme("macchiato")
	placeholderInput := input{}
	shortInput := input{}
	shortInput.set("a")
	longInput := input{}
	longInput.set("a significantly longer filter query")

	placeholder := paletteFilterInput(placeholderInput, &styles)
	short := paletteFilterInput(shortInput, &styles)
	long := paletteFilterInput(longInput, &styles)
	if lipgloss.Width(placeholder) != lipgloss.Width(short) {
		t.Fatalf("short active filter width %d differs from placeholder width %d", lipgloss.Width(short), lipgloss.Width(placeholder))
	}
	if lipgloss.Width(long) <= lipgloss.Width(placeholder) {
		t.Fatalf("long active filter did not expand: %d <= %d", lipgloss.Width(long), lipgloss.Width(placeholder))
	}
	if !strings.Contains(ansi.Strip(placeholder), "type to filter") {
		t.Fatalf("empty palette filter placeholder missing: %q", ansi.Strip(placeholder))
	}
	for _, query := range []input{placeholderInput, shortInput, longInput} {
		row := ansi.Strip(paletteFilterRow(query, "12 results", 60, &styles))
		if lipgloss.Width(row) != 60 || !strings.HasSuffix(row, "12 results") {
			t.Fatalf("filter count is not right-aligned in a fixed-width row: width=%d row=%q", lipgloss.Width(row), row)
		}
	}
}

func TestClassicPaletteSharesHeaderRowWithFilterAndCount(t *testing.T) {
	styles := stylesForTheme("macchiato")
	header := paletteClassicHeader("Commands", input{}, "18 commands", 82, false, &styles)
	stripped := ansi.Strip(header)
	if lipgloss.Width(stripped) != 82 {
		t.Fatalf("classic palette header width = %d, want 82", lipgloss.Width(stripped))
	}
	if !strings.HasPrefix(stripped, "COMMANDS │ ") || !strings.Contains(stripped, "type to filter") || !strings.HasSuffix(stripped, "18 commands") {
		t.Fatalf("classic palette header did not align title, filter and count: %q", stripped)
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
