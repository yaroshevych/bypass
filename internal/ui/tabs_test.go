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
	m.ws.CloseView()
	m.duplicateTab()
	if len(m.ws.Views) != 9 {
		t.Fatal("closing a tab did not release capacity")
	}
}
