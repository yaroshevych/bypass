package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestThemeCatalogAndFallback(t *testing.T) {
	want := []string{"Macchiato", "Mocha", "Frappe", "Latte", "Tokyo Night", "Gruvbox", "Dracula", "Nord", "Vesper"}
	got := themeNames()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("theme catalog = %v", got)
	}
	if colorsForTheme("unknown") != colorsForTheme("macchiato") || colorsForTheme("NORD") != colorsForTheme("Nord") || colorsForTheme("tokyo-night") != colorsForTheme("Tokyo Night") {
		t.Fatal("theme lookup did not preserve its default and case-insensitive behavior")
	}
	if ansi.Strip(stylesForTheme("Nord").accent.Render("accent")) != "accent" {
		t.Fatal("theme style changed visible content")
	}
	if ansi.Strip(defaultStyles.rule(4)) != "────" {
		t.Fatal("default fallback rule width changed")
	}
}

func TestCompactClassicAndASCIIFallback(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/appearance.kdbx"))
	m.width, m.height = 120, 35
	m.ws.ActiveView().NavIndex = 0
	classic := m.View()
	m.settings.Appearance = "compact"
	compact := m.View()
	if classic == compact {
		t.Fatal("compact appearance did not change rendering")
	}
	if !strings.Contains(compact, "") || !strings.Contains(compact, "") {
		t.Fatal("compact appearance is missing selected pills")
	}
	m.settings.ASCII = true
	ascii := m.View()
	if !strings.Contains(ascii, "[") || strings.Contains(ascii, "") || strings.Contains(ascii, "") {
		t.Fatal("ASCII mode did not replace compact glyphs")
	}
}

func TestViewStripsTerminalControlsFromDatabaseText(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/appearance.kdbx")
	db.Entries[0].Fields[0].Value = "safe\x1b[2Jcontent"
	m.ws.Open(db)
	m.width, m.height = 120, 35
	text := m.View()
	if strings.Contains(text, "\x1b[2J") {
		t.Fatal("database content injected a terminal control sequence")
	}
}

func TestSelectedEntryDoesNotReceiveSearchMatchHighlight(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/selected-search-highlight.kdbx"))
	m.width, m.height = 120, 35
	m.normalize()
	v := m.ws.ActiveView()
	v.Focus = 1
	v.SelectedUUID = "a"
	withoutQuery := ansi.Strip(m.entryLines(80, 10, true)[1])
	v.Query = "a"
	withQuery := ansi.Strip(m.entryLines(80, 10, true)[1])
	if withQuery != withoutQuery {
		t.Fatalf("selected entry changed when its match was highlighted:\nbefore %q\nafter  %q", withoutQuery, withQuery)
	}
}

func TestClassicFooterHasNoSeparatorAndSidebarHasNoContextHint(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/classic-footer.kdbx"))
	m.width, m.height = 120, 35
	m.normalize()
	m.ws.ActiveView().Focus = 0
	lines := m.workspaceLines()
	if strings.Contains(ansi.Strip(lines[len(lines)-2]), "─") || strings.Contains(ansi.Strip(lines[len(lines)-2]), "┴") {
		t.Fatal("classic footer still has a horizontal separator")
	}
	if got := m.footerContext(); got != "" {
		t.Fatalf("sidebar footer context = %q, want none", got)
	}
	if m.layout().BodyHeight != m.height-5 {
		t.Fatalf("classic body height = %d, want %d after removing footer rule", m.layout().BodyHeight, m.height-5)
	}
}

func TestMinimumUnlockKeepsFilenameAndEssentialControls(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 14
	m.path = "/a/very/long/directory/with/many/components/demo-vault.kdbx"
	text := ansi.Strip(strings.Join(m.unlockLines(), "\n"))
	for _, label := range []string{"demo-vault.kdbx", "Password:", "Key file:", "Hardware slot:", "Password auth:", "tab next"} {
		if !strings.Contains(text, label) {
			t.Fatalf("minimum-size unlock lost %q:\n%s", label, text)
		}
	}
	if strings.Contains(text, "esc cancel") {
		t.Fatalf("minimum-size unlock repeated the common Esc shortcut:\n%s", text)
	}
}

func TestCompactSelectionKeepsEntryAndViewTextAligned(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/alignment.kdbx"))
	m.width, m.height = 120, 35
	m.settings.Appearance = "compact"
	m.normalize()
	before := m.entryLines(48, 10, true)
	m.ws.ActiveView().SelectedUUID = "b"
	after := m.entryLines(48, 10, true)
	for i, title := range []string{"Alpha", "Beta"} {
		a := []rune(ansi.Strip(before[i+1]))
		b := []rune(ansi.Strip(after[i+1]))
		if string(a[2:2+len(title)]) != title || string(b[2:2+len(title)]) != title {
			t.Fatalf("%s moved horizontally when selection changed", title)
		}
	}
	before = m.navLines(27, 20, true)
	m.selectNav(1)
	after = m.navLines(27, 20, true)
	for i, title := range []string{"All Entries", "Favourites"} {
		for _, rows := range [][]string{before, after} {
			runes := []rune(ansi.Strip(rows[i+1]))
			if string(runes[2:2+len(title)]) != title {
				t.Fatalf("%s lost its fixed compact inset", title)
			}
		}
	}
	if m.layout().BodyY != 2 || m.layout().BodyHeight != 32 {
		t.Fatal("compact panes did not reclaim header/footer separator rows")
	}
	m.selectNav(0)
	if !strings.Contains(ansi.Strip(m.View()), "├") {
		t.Fatal("details divider does not join the pane border")
	}
}
