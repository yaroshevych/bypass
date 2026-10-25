package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestDisplayVaultPathUsesTildeOnlyWithinHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	path := filepath.Join(home, "vaults", "personal.kdbx")
	if got, want := displayVaultPath(path, home), filepath.Join("~", "vaults", "personal.kdbx"); got != want {
		t.Fatalf("home path = %q, want %q", got, want)
	}
	if got := displayVaultPath(home, home); got != "~" {
		t.Fatalf("home directory path = %q, want ~", got)
	}
	outside := home + "-other" + string(filepath.Separator) + "vault.kdbx"
	if got := displayVaultPath(outside, home); got != outside {
		t.Fatalf("path outside home was rewritten: %q", got)
	}
}

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

func TestFooterShowsReadOnlyForManualAndExternalChange(t *testing.T) {
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	for _, state := range []struct {
		name     string
		readOnly bool
		stale    bool
	}{
		{name: "manual", readOnly: true},
		{name: "external", stale: true},
	} {
		t.Run(state.name, func(t *testing.T) {
			s.ReadOnly, s.Stale = state.readOnly, state.stale
			if got := m.footerText(); !strings.Contains(got, "/ filter · ctrl+p commands · read only") {
				t.Fatalf("footer = %q, want read-only indicator", got)
			}
		})
	}
}

func TestLockedVaultFooterRightAlignsTabAndQuitActions(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.ws.Open(fixture("/tmp/footer-locked.kdbx"))
	m.lockCurrent(false)
	rows := m.workspaceLines()
	view := strings.Join(rows, "\n")
	if !strings.Contains(view, "Vault locked") || !strings.Contains(view, "/tmp/footer-locked.kdbx") || strings.Contains(view, "1–9 switch vault views") {
		t.Fatalf("locked view should show its full path without the tab-switch hint: %q", view)
	}
	pathRow := -1
	unlockInstruction := false
	for i, row := range rows {
		stripped := ansi.Strip(row)
		if strings.Contains(stripped, "/tmp/footer-locked.kdbx") {
			pathRow = i
		}
		if stripped == "  enter to unlock" {
			unlockInstruction = true
		}
	}
	if !unlockInstruction {
		t.Fatal("locked view should show the concise 'enter to unlock' instruction")
	}
	if pathRow < 0 || pathRow+1 >= len(rows) || strings.TrimSpace(ansi.Strip(rows[pathRow+1])) != "" {
		t.Fatal("locked view should leave one blank row after the vault path")
	}
	for i, row := range rows {
		if strings.Contains(ansi.Strip(row), "enter to unlock") && (i+1 >= len(rows) || !strings.Contains(ansi.Strip(rows[i+1]), "ctrl+o open another vault")) {
			t.Fatal("locked view should not leave a blank row before the Ctrl+O hint")
		}
	}
	line := ansi.Strip(rows[m.height-1])
	if !strings.HasSuffix(line, "enter unlock · ctrl+w close tab · q quit") {
		t.Fatalf("locked footer actions are not placed as expected: %q", line)
	}
	if ansi.StringWidth(line) != m.width {
		t.Fatalf("locked footer width = %d, want %d", ansi.StringWidth(line), m.width)
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
	if !strings.Contains(line, "\ue0b4  / term") || strings.Contains(line, "\ue0b4  \ue0b6") || !strings.Contains(line, " \ue0b4 enter apply") {
		t.Fatalf("compact search field is not joined to the app pill without shifting its text: %q", line)
	}
	if got := ansi.StringWidth(line[:strings.Index(line, "/ term")]); got != 12 {
		t.Fatalf("compact search text moved to column %d; want its existing column 12", got)
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
	if !strings.Contains(line, "/ filter \ue0b4") {
		t.Fatalf("compact placeholder lost its right-side padding: %q", line)
	}
}
