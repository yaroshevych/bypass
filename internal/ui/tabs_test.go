package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTabPlusHoverAndPerViewItemTitles(t *testing.T) {
	for _, mode := range []string{"classic", "compact"} {
		t.Run(mode, func(t *testing.T) {
			m := New(Options{})
			m.ws.Open(fixture("/tmp/tabs.kdbx"))
			m.width, m.height = 120, 35
			m.settings.Appearance = mode
			m.normalize()
			row := 2
			if mode == "compact" {
				row = 1
			}
			spans := m.tabSpans()
			plus := spans[len(spans)-1]
			m.pointer(tea.MouseMsg{X: plus.Start, Y: row, Action: tea.MouseActionMotion})
			if !m.tabHovered || m.hoverTab != 1 || len(m.ws.Views) != 1 {
				t.Fatal("hover did not highlight + without opening a tab")
			}
			m.pointer(tea.MouseMsg{X: plus.Start, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if len(m.ws.Views) != 2 || m.ws.Active != 1 {
				t.Fatal("+ did not create and activate a new tab")
			}
			m.ws.ActiveView().SelectedUUID = "b"
			labels := m.tabLabels()
			if !strings.Contains(labels[0], "Alpha") || !strings.Contains(labels[1], "Beta") {
				t.Fatalf("tab titles are not per-view item titles: %v", labels)
			}
			first := m.tabSpans()[0]
			m.pointer(tea.MouseMsg{X: first.Start, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.ws.Active != 0 {
				t.Fatal("tab click did not activate first tab")
			}
			m.pointer(tea.MouseMsg{X: first.Start, Y: row, Action: tea.MouseActionMotion})
			m.pointer(tea.MouseMsg{X: 0, Y: row + 2, Action: tea.MouseActionMotion})
			if m.tabHovered {
				t.Fatal("tab hover persisted after leaving the strip")
			}
			if !strings.Contains(ansi.Strip(m.workspaceLines()[row]), "+") {
				t.Fatal("+ is missing from tab strip")
			}
		})
	}
}

func TestClassicTabDividersJoinHorizontalRules(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/tab-rule-joins.kdbx"))
	m.width, m.height = 120, 35
	m.normalize()
	m.duplicateTab()
	m.duplicateTab()
	lines := m.workspaceLines()
	spans := m.tabSpans()
	top, tabs, bottom := []rune(ansi.Strip(lines[1])), []rune(ansi.Strip(lines[2])), []rune(ansi.Strip(lines[3]))
	for i := 0; i+1 < len(spans); i++ {
		x := spans[i].End
		if top[x] != '┬' || tabs[x] != '│' || bottom[x] != '┴' {
			t.Fatalf("tab divider at %d does not join the rules: top=%q tabs=%q bottom=%q", x, top[x], tabs[x], bottom[x])
		}
	}
}

func TestClassicTabsUseUppercaseBoldTextWithoutSelectionPill(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/classic-tabs-uppercase.kdbx"))
	m.width, m.height = 120, 35
	m.normalize()
	m.duplicateTab()
	m.ws.Views[0].SelectedUUID = "a"
	m.ws.Views[1].SelectedUUID = "b"
	rendered := m.workspaceLines()[2]
	line := ansi.Strip(rendered)
	if !strings.Contains(line, "ALPHA") || !strings.Contains(line, "BETA") {
		t.Fatalf("classic tabs should render uppercase item titles: %q", line)
	}
	runes := []rune(line)
	for _, span := range m.tabSpans()[:len(m.ws.Views)] {
		if runes[span.Start] != ' ' || runes[span.End-1] != ' ' {
			t.Fatalf("classic tab %d should have one balanced outer space: %q", span.Index, string(runes[span.Start:span.End]))
		}
	}
}

func TestNewTabUsesTShortcutAndPaletteLabel(t *testing.T) {
	m, _ := operationModel(t)
	var foundNew, foundClose, foundPrevious, foundNext bool
	for _, command := range m.commands() {
		if command.id == "duplicate" {
			foundNew = command.label == "New Tab" && command.key == "t"
		}
		if command.id == "close-view" {
			foundClose = command.label == "Close Tab" && command.key == "Ctrl+W"
		}
		if command.id == "previous-tab" {
			foundPrevious = command.key == "["
		}
		if command.id == "next-tab" {
			foundNext = command.key == "]"
		}
	}
	if !foundNew || !foundClose || !foundPrevious || !foundNext {
		t.Fatalf("tab palette commands have incorrect names or shortcuts: New Tab=%v, Close Tab=%v, previous=%v, next=%v", foundNew, foundClose, foundPrevious, foundNext)
	}
	m.key(key("t"))
	if len(m.ws.Views) != 2 || m.ws.Active != 1 {
		t.Fatal("t did not create and activate a new tab")
	}
	m.key(key("["))
	if m.ws.Active != 0 {
		t.Fatal("[ did not switch to the previous tab")
	}
	m.key(key("]"))
	if m.ws.Active != 1 {
		t.Fatal("] did not switch to the next tab")
	}
}

func TestNewTabIsUnavailableWhileVaultIsLocked(t *testing.T) {
	m, _ := operationModel(t)
	m.duplicateTab()
	m.lockCurrent(false)
	before := len(m.ws.Views)
	m.key(key("t"))
	if len(m.ws.Views) != before {
		t.Fatal("t created a tab while the active vault was locked")
	}
	for _, span := range m.tabSpans() {
		if span.Index == len(m.ws.Views) {
			t.Fatal("+ tab was visible while the active vault was locked")
		}
	}
	var disabled bool
	for _, command := range m.commands() {
		if command.id == "duplicate" {
			disabled = command.reason != ""
		}
	}
	if !disabled {
		t.Fatal("New Tab remained enabled in the command palette while locked")
	}
	m.key(key("ctrl+w"))
	if len(m.ws.Views) != before-1 {
		t.Fatal("Ctrl+W could not close a tab while the vault was locked")
	}
}

func TestLockedVaultTabsCollapseAndRestorePerVaultViewSelection(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	pathA, pathB := "/tmp/vault-a.kdbx", "/tmp/vault-b.kdbx"
	m.ws.Open(fixture(pathA))
	m.normalize()
	m.duplicateTab()
	m.ws.Views[1].SelectedUUID = "b"
	m.ws.Switch(1) // Remember Beta as the active view in vault A.
	m.ws.Open(fixture(pathB))
	m.normalize()
	m.duplicateTab()
	m.duplicateTab()
	m.ws.Switch(4)
	m.normalize()
	if len(m.ws.Views) != 5 {
		t.Fatalf("setup created %d underlying views, want 5", len(m.ws.Views))
	}
	m.lockCurrent(true)
	if got := len(m.tabLabels()); got != 2 {
		t.Fatalf("locked tabs = %d, want one per vault", got)
	}
	lockedLabels := strings.Join(m.tabLabels(), "")
	if !strings.Contains(lockedLabels, "vault-a.kdbx") || !strings.Contains(lockedLabels, "vault-b.kdbx") {
		t.Fatalf("locked tabs should show vault names: %q", lockedLabels)
	}
	if !m.ws.Open(fixture(pathA)) {
		t.Fatal("unlock refresh did not reuse vault A's locked session")
	}
	if m.ws.Active != 1 {
		t.Fatalf("unlock restored active view %d, want the previously active vault A view 1", m.ws.Active)
	}
	if got := len(m.tabLabels()); got != 3 {
		t.Fatalf("unlocking vault A should restore its two tabs while vault B stays collapsed: got %d", got)
	}
	if !strings.Contains(strings.Join(m.tabLabels(), ""), "Beta") {
		t.Fatal("unlock did not restore the previously active item tab title")
	}
}

func TestTabCreationStopsAtNineAndResumesAfterClose(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/limit.kdbx"))
	m.width, m.height = 120, 35
	m.normalize()
	for i := 1; i < 9; i++ {
		m.duplicateTab()
	}
	m.duplicateTab()
	if len(m.ws.Views) != 9 || m.ws.ActiveSession() == nil || !strings.Contains(m.notice, "Maximum 9 tabs") {
		t.Fatal("tab limit did not preserve the active workspace")
	}
	for _, span := range m.tabSpans() {
		if span.Index == len(m.ws.Views) {
			t.Fatal("+ tab should be hidden at the nine-tab limit")
		}
	}
	spans := m.tabSpans()
	lastTab := spans[len(spans)-1]
	tabLine := []rune(ansi.Strip(m.workspaceLines()[2]))
	if lastTab.End >= len(tabLine) || tabLine[lastTab.End] != '│' {
		t.Fatalf("last tab should have a right divider: tab=%+v line=%q", lastTab, string(tabLine))
	}
	lines := m.workspaceLines()
	topRule := []rune(ansi.Strip(lines[1]))
	bottomRule := []rune(ansi.Strip(lines[3]))
	if topRule[lastTab.End] != '┬' || bottomRule[lastTab.End] != '┴' {
		t.Fatalf("right divider should join both horizontal rules: top=%q bottom=%q", topRule[lastTab.End], bottomRule[lastTab.End])
	}
	m.ws.CloseView()
	spans = m.tabSpans()
	if spans[len(spans)-1].Index != len(m.ws.Views) {
		t.Fatal("+ tab should return after closing a tab")
	}
	m.duplicateTab()
	if len(m.ws.Views) != 9 {
		t.Fatal("closing a tab did not release capacity")
	}
}
