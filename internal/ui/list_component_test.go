package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestListComponentAlignsClassicHotkeysAndKeepsGroupedRows(t *testing.T) {
	m := New(Options{})
	m.width = 32
	rows := []listComponentRow{
		{kind: listComponentHeader, item: -1, text: "VAULTS", bold: true},
		{kind: listComponentItem, item: 0, text: "Open Vault", right: "o"},
	}
	got := m.renderListComponent(rows, listComponentOptions{width: 32})
	if len(got) != 2 || got[0].item != -1 || got[1].item != 0 {
		t.Fatalf("grouped rows lost their selection mapping: %#v", got)
	}
	line := ansi.Strip(got[1].text)
	if lipgloss.Width(line) != 32 || !strings.HasSuffix(line, "o") {
		t.Fatalf("hotkey is not aligned to the classic row's right edge: %q", line)
	}
	if strings.TrimSpace(ansi.Strip(got[0].text)) != "VAULTS" {
		t.Fatalf("palette group heading gained unwanted row padding: %q", got[0].text)
	}
}

func TestListComponentRendersSidebarCountDifferentlyByLayout(t *testing.T) {
	m := New(Options{})
	rows := []listComponentRow{{kind: listComponentItem, item: 3, text: "Favorites", count: "2", selected: true, selectedText: "Favorites"}}
	compact := m.renderListComponent(rows, listComponentOptions{width: 24, height: 1, compact: true, focused: true, sidebar: true})
	if len(compact) != 1 || compact[0].item != 3 || !strings.Contains(ansi.Strip(compact[0].text), "2") {
		t.Fatalf("compact sidebar count missing: %#v", compact)
	}

	classic := m.renderListComponent([]listComponentRow{{kind: listComponentItem, item: 3, text: "  Favorites"}, {kind: listComponentItem, item: 3, text: "  2 entries", countLine: true}}, listComponentOptions{width: 24, height: 2, sidebar: true})
	if len(classic) != 2 || !strings.Contains(ansi.Strip(classic[1].text), "2 entries") {
		t.Fatalf("classic sidebar count did not occupy its secondary line: %#v", classic)
	}
}
