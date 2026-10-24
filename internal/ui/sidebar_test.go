package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSidebarSectionsCountsAndHeaderMouseTargets(t *testing.T) {
	for _, mode := range []string{"compact", "classic"} {
		t.Run(mode, func(t *testing.T) {
			m := New(Options{})
			db := fixture("/tmp/sidebar.kdbx")
			db.Entries[0].Tags = []string{"work"}
			m.ws.Open(db)
			m.width, m.height = 120, 35
			m.settings.Appearance = mode
			m.normalize()
			pane := m.layout().Panes[0]
			lines := m.navLines(pane.Width, pane.Height, true)
			for _, header := range []string{"Views", "Tags", "Groups"} {
				found := false
				for i, line := range lines {
					text := strings.TrimSpace(ansi.Strip(line))
					if text == strings.ToUpper(header) || strings.HasPrefix(text, strings.ToUpper(header)+" ") {
						before := m.ws.ActiveView().NavKey
						m.pointer(tea.MouseMsg{X: 3, Y: pane.Y + i, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
						if m.ws.ActiveView().NavKey != before {
							t.Fatal("section header selected an item")
						}
						found = true
					}
				}
				if !found {
					t.Fatalf("missing sidebar section %s", header)
				}
			}
			if mode == "compact" {
				row := []rune(ansi.Strip(lines[1]))
				if row[pane.Width-3] != '2' {
					t.Fatal("compact count is not right-aligned inside the pill inset")
				}
			} else if !strings.Contains(ansi.Strip(lines[2]), "2 entries") {
				t.Fatal("classic mode lost its second-line count")
			}
			for i, line := range lines {
				if strings.Contains(ansi.Strip(line), "Empty group") {
					m.pointer(tea.MouseMsg{X: 5, Y: pane.Y + i, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
					if m.currentNav().group != "empty" {
						t.Fatal("group click was offset by section headers")
					}
					return
				}
			}
			t.Fatal("nested group missing")
		})
	}
}

func TestRecentCombinesEditsCopyAndRevealWithoutChangingVaultTimes(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/recent.kdbx")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	db.Entries[0].Modified = now.Add(-time.Hour)
	db.Entries[1].Modified = now.Add(-time.Minute)
	m.now = func() time.Time { return now }
	m.ws.Open(db)
	m.normalize()
	recent := m.navByKey("recent")
	if m.entriesFor(recent, "")[0].UUID != "b" {
		t.Fatal("Recent did not prefer latest edit")
	}
	m.ws.ActiveView().SelectedUUID = "a"
	m.copyField("UserName")
	if m.entriesFor(recent, "")[0].UUID != "a" {
		t.Fatal("copy did not update Recent")
	}
	now = now.Add(time.Second)
	m.ws.ActiveView().SelectedUUID = "b"
	m.toggleReveal()
	if m.entriesFor(recent, "")[0].UUID != "b" {
		t.Fatal("reveal did not update Recent")
	}
	if !db.Entries[0].Modified.Equal(now.Add(-time.Hour - time.Second)) {
		t.Fatal("item use changed vault modification metadata")
	}
	m.Close()
}
