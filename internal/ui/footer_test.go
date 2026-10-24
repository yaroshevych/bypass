package ui

import (
	"strings"
	"testing"

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
			if !strings.HasPrefix(strings.TrimLeft(line, "\ue0b6 "), "bypass") || !strings.Contains(line, "/ filter · Ctrl+P commands") {
				t.Fatal("footer lost its app label or global actions")
			}
			if !strings.HasSuffix(line, "c copy · v reveal · e edit") || ansi.StringWidth(line) != m.width {
				t.Fatal("contextual shortcuts are not right-aligned")
			}
		}
		m.width = 48
		line := ansi.Strip(m.footerLine(m.footerText(), m.footerContext()))
		if !strings.Contains(line, "/ filter · Ctrl+P commands") || ansi.StringWidth(line) != 48 {
			t.Fatal("small footer overlaps or loses global actions")
		}
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
	if !strings.Contains(line, m.notice) || !strings.Contains(line, "/ filter · Ctrl+P commands") {
		t.Fatal("footer clipped its success notice or global actions")
	}
	if !strings.HasSuffix(line, "c · v · e") {
		t.Fatal("footer did not shorten and right-align contextual keys")
	}
}
