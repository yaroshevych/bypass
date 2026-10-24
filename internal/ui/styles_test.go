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
	if !strings.Contains(compact, "◖") || !strings.Contains(compact, "◗") {
		t.Fatal("compact appearance is missing selected pills")
	}
	m.settings.ASCII = true
	ascii := m.View()
	if !strings.Contains(ascii, "[") || strings.Contains(ascii, "◖") || strings.Contains(ascii, "◗") {
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

func TestMinimumUnlockKeepsFilenameAndEssentialControls(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 14
	m.path = "/a/very/long/directory/with/many/components/demo-vault.kdbx"
	text := ansi.Strip(strings.Join(m.unlockLines(), "\n"))
	for _, label := range []string{"demo-vault.kdbx", "Password:", "Key file:", "Hardware slot:", "Password auth:", "Esc cancel"} {
		if !strings.Contains(text, label) {
			t.Fatalf("minimum-size unlock lost %q:\n%s", label, text)
		}
	}
}
