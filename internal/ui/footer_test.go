package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterKeepsGlobalActionsLeftAndContextAtRight(t *testing.T) {
	for _, mode := range []string{"classic", "compact"} {
		m := New(Options{})
		m.width, m.height = 120, 35
		m.settings.Appearance = mode
		m.ws.Open(fixture("/tmp/footer.kdbx"))
		m.normalize()
		m.ws.ActiveView().Focus = 2
		for _, notice := range []string{"", " · " + strings.Repeat("long status ", 20)} {
			line := ansi.Strip(m.footerLine(m.footerText()+notice, m.footerContext()))
			if !strings.HasPrefix(strings.TrimLeft(line, "\ue0b6 "), "bypass") || !strings.Contains(line, "/ filter · ctrl+p commands") {
				t.Fatal("footer lost its app label or global actions")
			}
			if !strings.HasSuffix(line, "c copy · v reveal · e edit") || ansi.StringWidth(line) != m.width {
				t.Fatal("contextual shortcuts are not right-aligned")
			}
		}
		m.width = 48
		line := ansi.Strip(m.footerLine(m.footerText(), m.footerContext()))
		if !strings.Contains(line, "/ filter · ctrl+p commands") || ansi.StringWidth(line) != 48 {
			t.Fatal("small footer overlaps or loses global actions")
		}
	}
}

func TestDefaultFooterPadsSlashAfterBadge(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	line := ansi.Strip(m.footerLine(m.footerText()))
	if !strings.Contains(line, "bypass  / filter · ctrl+p commands") {
		t.Fatalf("default footer is missing its slash inset: %q", line)
	}
}

func TestLongSuccessNoticeRemainsReadableBesideGlobalActions(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.ws.Open(fixture("/tmp/footer-notice.kdbx"))
	m.normalize()
	m.ws.ActiveView().Focus = 2
	m.notice = "Credentials changed and verified. Encrypted backup created."
	rows := m.workspaceLines()
	line := ansi.Strip(rows[len(rows)-1])
	if !strings.Contains(line, m.notice) || !strings.Contains(line, "/ filter · ctrl+p commands") {
		t.Fatal("footer clipped its success notice or global actions")
	}
	if !strings.HasSuffix(line, "c · v · e") {
		t.Fatal("footer did not shorten and right-align contextual keys")
	}
}

func TestSearchFooterShowsQueryAndSearchActions(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.ws.Open(fixture("/tmp/footer-search.kdbx"))
	m.normalize()
	m.searching = true
	m.query.set("test search")
	line := ansi.Strip(m.searchFooterLine(m.footerText(), m.footerContext(), false))
	if !strings.Contains(line, "bypass  / test search  enter apply · esc cancel") || strings.Contains(line, "/ filter ·") {
		t.Fatalf("search input footer is incorrect: %q", line)
	}

	m.searching = false
	m.query.clear()
	m.ws.ActiveView().Query = "test search"
	line = ansi.Strip(m.searchFooterLine(m.footerText(), m.footerContext(), true))
	if !strings.Contains(line, "/ test search  esc cancel") || strings.Contains(line, "/ filter ·") {
		t.Fatalf("applied search footer is incorrect: %q", line)
	}

	m.ws.ActiveView().Query = ""
	m.searching = true
	m.query.clear()
	line = ansi.Strip(m.searchFooterLine(m.footerText(), m.footerContext(), false))
	if !strings.Contains(line, "/ filter") || strings.Contains(line, "/ filter items") || strings.Contains(line, "/ Filter") {
		t.Fatalf("empty search footer lacks its placeholder: %q", line)
	}
	styled := m.searchFooterLine(m.footerText(), m.footerContext(), false)
	if lipgloss.Width(styled) != m.width || !strings.Contains(ansi.Strip(styled), "/ filter") {
		t.Fatalf("empty search placeholder cursor is missing or misaligned: %q", styled)
	}
}

func TestCompactSearchFooterKeepsItsOuterGap(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.settings.Appearance = "compact"
	m.ws.Open(fixture("/tmp/footer-compact-search.kdbx"))
	m.normalize()
	m.searching = true
	m.query.set("term")
	line := ansi.Strip(m.searchFooterLine(m.footerText(), m.footerContext(), false))
	if !strings.Contains(line, "\ue0b4 \ue0b6/ term") || !strings.Contains(line, " \ue0b4 enter apply") {
		t.Fatalf("compact search field shifted left of its outer gap: %q", line)
	}
}

func TestCompactFilterPlaceholderOmitsLeftAndKeepsRightPadding(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.settings.Appearance = "compact"
	m.ws.Open(fixture("/tmp/footer-compact-placeholder.kdbx"))
	m.normalize()
	m.searching = true
	line := ansi.Strip(m.searchFooterLine(m.footerText(), m.footerContext(), false))
	if !strings.Contains(line, "\ue0b6/ filter \ue0b4") {
		t.Fatalf("compact placeholder lost its right-side padding: %q", line)
	}
}
