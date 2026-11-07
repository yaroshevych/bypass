package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestThemeCatalogAndFallback(t *testing.T) {
	want := []string{"Macchiato", "Mocha", "Frappe", "Latte", "Tokyo Night", "Gruvbox", "Dracula", "Nord", "Vesper", "Omarchy"}
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

func TestSemanticTextAndDividerStylesUseThemeSurface(t *testing.T) {
	styles := stylesForTheme("Latte")
	for name, style := range map[string]lipgloss.Style{
		"primary": styles.primary, "secondary": styles.secondary, "accent": styles.accent,
		"error": styles.error, "divider": styles.divider,
	} {
		if style.GetBackground() == nil {
			t.Errorf("%s style has no theme background", name)
		}
	}
}

func TestRestoreThemeResetsKeepsSurfaceAfterNestedStyles(t *testing.T) {
	input := "\x1b[38;2;1;2;3mX\x1b[39mY\x1b[49mZ\x1b[0m"
	want := "\x1b[38;2;1;2;3mX\x1b[38;2;248;248;242mY\x1b[48;2;40;42;54mZ\x1b[0;38;2;248;248;242;48;2;40;42;54m"
	if got := restoreThemeResets(input, "#f8f8f2", "#282a36"); got != want {
		t.Fatalf("theme resets were not restored: %q", got)
	}
}

func Test256ThemesMaintainContrast(t *testing.T) {
	for _, name := range themeNames() {
		c := colors256(trueColorsForTheme(name))
		for role, pair := range map[string]struct {
			fg, bg  string
			minimum float64
		}{
			"primary": {c.primary, c.background, 4.5}, "secondary": {c.secondary, c.background, 3},
			"divider": {c.divider, c.background, 3}, "accent": {c.accent, c.background, 4.5},
			"error": {c.error, c.background, 4.5}, "selection": {c.selectedForeground, c.selectedBackground, 4.5},
			"inactive": {c.primary, c.inactiveBackground, 4.5},
		} {
			if got := contrast256(parseThemeRGB(pair.fg), parseThemeRGB(pair.bg)); got < pair.minimum {
				t.Errorf("%s %s contrast %.2f < %.2f", name, role, got, pair.minimum)
			}
		}
	}
}

func TestCompactEdgesFollowColorProfile(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, tc := range []struct {
		profile termenv.Profile
		want    string
	}{{termenv.ANSI256, "█"}, {termenv.ANSI, "█"}, {termenv.TrueColor, "\ue0b6"}} {
		lipgloss.SetColorProfile(tc.profile)
		m := New(Options{})
		m.settings.Appearance = "compact"
		m.ws.Open(fixture("/tmp/edges.kdbx"))
		m.width, m.height = 120, 35
		if view := m.View(); !strings.Contains(view, tc.want) {
			t.Errorf("profile %v missing edge %q", tc.profile, tc.want)
		}
	}
}

func TestRestoreThemeResetsUsesTerminalPalette(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)
	for _, name := range themeNames() {
		colors := colorsForTheme(name)
		// Both nested styles and restored surfaces must emit the same palette entry.
		style := stylesForTheme(name).primary.Render("x")
		sequence := "\x1b[" + termenv.ANSI256.Color(string(paletteColor(colors.background))).Sequence(true) + "m"
		if !strings.Contains(style, strings.TrimPrefix(sequence, "\x1b[")) {
			t.Errorf("%s style missing background %q: %q", name, sequence, style)
		}
		got := restoreThemeResets("\x1b[49m", colors.primary, colors.background)
		want := "\x1b[" + termenv.ANSI256.Color(string(paletteColor(colors.background))).Sequence(true) + "m"
		if got != want {
			t.Errorf("%s background reset = %q, want %q", name, got, want)
		}
	}
}

func TestRestoreThemeResetsLeavesRGBChannelsUntouched(t *testing.T) {
	input := "\x1b[38;2;49;0;54;48;2;40;49;54mX"
	if got := restoreThemeResets(input, "#f8f8f2", "#282a36"); got != input {
		t.Fatalf("RGB channels mistaken for SGR resets: %q", got)
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
	for _, label := range []string{"demo-vault.kdbx", "Password", "Open", "enter"} {
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
	for _, title := range []string{"Alpha", "Beta"} {
		x, _ := renderedTextPosition(t, before, title)
		nextX, _ := renderedTextPosition(t, after, title)
		if x != 1 || nextX != x {
			t.Fatalf("%s moved horizontally when selection changed", title)
		}
	}
	before = m.navLines(27, 20, true)
	m.selectNav(1)
	after = m.navLines(27, 20, true)
	for _, title := range []string{"All Entries", "Favourites"} {
		for _, rows := range [][]string{before, after} {
			x, _ := renderedTextPosition(t, rows, title)
			if x != 1 {
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
