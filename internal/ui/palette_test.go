package ui

import (
	"reflect"
	"strings"
	"testing"

	dialogrender "bypass/internal/ui/dialog"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestPaletteThemePreviewCancelRestoresStylesAndViewFilters(t *testing.T) {
	m, _ := operationModel(t)
	v := m.ws.ActiveView()
	v.Focus = 2
	v.Query = "Alpha"
	v.NavFilter = "Root"
	v.DetailFilter = "Password"
	beforeView := *v
	beforeSettings := m.settings
	beforeStyles := m.styles

	m.openPalette("actions", false)
	m.palette.query.set("Theme")
	m.refreshPalette()
	if got := m.paletteChoices(); len(got) != 1 || got[0].next != "themes" {
		t.Fatalf("theme command choices = %#v", got)
	}
	m.executePalette()
	m.palette.query.set("Nord")
	m.refreshPalette()
	m.previewChoice()
	if m.settings.Theme != "nord" || reflect.DeepEqual(m.styles, beforeStyles) {
		t.Fatal("theme selection did not preview the requested theme")
	}

	m.key(key("esc"))
	if m.palette == nil || m.palette.scope != "actions" {
		t.Fatal("Escape did not return to the parent palette")
	}
	if m.settings != beforeSettings || !reflect.DeepEqual(m.styles, beforeStyles) {
		t.Fatal("canceling theme preview did not restore original settings and styles")
	}
	m.key(key("esc"))
	if m.palette != nil {
		t.Fatal("Escape did not close the parent palette")
	}
	if *m.ws.ActiveView() != beforeView {
		t.Fatal("palette preview changed active view focus or filters")
	}
}

func TestShiftTOpensThemePaletteWithoutCreatingTab(t *testing.T) {
	m, _ := operationModel(t)
	viewCount := len(m.ws.Views)
	m.key(key("T"))
	if m.palette == nil || m.palette.scope != "themes" {
		t.Fatal("Shift+T did not open the Themes dialog")
	}
	if len(m.ws.Views) != viewCount {
		t.Fatal("Shift+T created a tab")
	}
	for _, command := range m.commands() {
		if command.id == "theme" && command.key != "Shift+T" {
			t.Fatalf("Theme shortcut = %q, want Shift+T", command.key)
		}
	}
}

func TestThemesPaletteOmitsRedundantGroupHeading(t *testing.T) {
	m, _ := operationModel(t)
	m.settings.Appearance = "classic"
	m.openPalette("themes", false)
	rows, _ := m.paletteRows(m.paletteChoices())
	for _, row := range rows {
		if row.choice == -1 {
			t.Fatalf("Themes picker contains a group heading: %q", ansi.Strip(row.text))
		}
		if row.choice >= 0 {
			plain := ansi.Strip(row.text)
			if !strings.HasPrefix(plain, " ") || !strings.HasSuffix(plain, " ") {
				t.Fatalf("classic dialog item is missing one-cell side padding: %q", plain)
			}
		}
	}
}

func TestCommandPaletteBundlesCommandsIntoFiveSections(t *testing.T) {
	m, _ := operationModel(t)
	m.openPalette("actions", false)
	choices := m.paletteChoices()
	rows, _ := m.paletteRows(choices)
	wantSections := []string{"VAULTS", "VIEW", "ENTRIES", "SECRETS", "SETTINGS"}
	var sections []string
	for _, row := range rows {
		if row.choice == -1 {
			sections = append(sections, strings.TrimSpace(strings.TrimRight(ansi.Strip(row.text), "─- ")))
		}
	}
	if !reflect.DeepEqual(sections, wantSections) {
		t.Fatalf("command palette sections = %#v, want %#v", sections, wantSections)
	}
	wantStarts := map[string][]string{
		"VAULTS":   {"protect", "open", "close-vault"},
		"VIEW":     {"duplicate", "close-view", "previous-tab"},
		"ENTRIES":  {"new", "edit", "group"},
		"SECRETS":  {"copy-focused", "reveal", "generator"},
		"SETTINGS": {"theme", "appearance", "ascii"},
	}
	for section, want := range wantStarts {
		var got []string
		for _, choice := range choices {
			if commandSection(choice.group) == section && len(got) < len(want) {
				got = append(got, choice.id)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s command order starts %v, want %v", section, got, want)
		}
	}
	var vaults []string
	for _, choice := range choices {
		if commandSection(choice.group) == "VAULTS" {
			vaults = append(vaults, choice.id)
		}
	}
	lockAt, pinAt := -1, -1
	for i, id := range vaults {
		if id == "lock" {
			lockAt = i
		}
		if id == "protect" {
			pinAt = i
		}
	}
	if pinAt != 0 || lockAt < 0 {
		t.Fatalf("Lock with PIN is not first in VAULTS: %v", vaults)
	}
	var secrets []string
	for _, choice := range choices {
		if commandSection(choice.group) == "SECRETS" {
			secrets = append(secrets, choice.id)
		}
	}
	if len(secrets) == 0 || secrets[len(secrets)-1] == "totp" {
		t.Fatalf("unsupported TOTP command remains in SECRETS: %v", secrets)
	}
	for _, choice := range choices {
		if strings.HasPrefix(choice.id, "tab:") || strings.HasPrefix(choice.label, "Switch to ") {
			t.Fatalf("numbered tab-switch command remains in palette: %#v", choice)
		}
		switch choice.id {
		case "clear-filters":
			t.Fatalf("Clear All Filters command remains in palette: %#v", choice)
		case "clear-pin":
			t.Fatalf("Remove Session PIN command remains in palette: %#v", choice)
		case "wider", "narrower":
			t.Fatalf("pane resize command remains in palette: %#v", choice)
		case "copy:Title", "copy:UserName", "copy:Password", "copy:Email", "copy:URL", "copy:Notes":
			t.Fatalf("field-specific copy command remains in palette: %#v", choice)
		}
		wantLabel := map[string]string{
			"copy-focused": "Copy", "reveal": "Reveal", "grand-view": "Large View",
			"theme": "Theme", "appearance": "Layout", "set-pin": "Change PIN", "protect": "Lock with PIN", "jump": "Jump",
			"read-only": "Read Only",
			"totp":      "TOTP", "help": "Shortcuts", "close-vault": "Close Vault", "lock": "Lock Vault",
			"search": "Search", "zoom": "Zoom", "delete": "Delete", "history-view": "History",
		}[choice.id]
		if wantLabel != "" && choice.label != wantLabel {
			t.Errorf("command %q label = %q, want %q", choice.id, choice.label, wantLabel)
		}
		if choice.id == "jump" && commandSection(choice.group) != "ENTRIES" {
			t.Errorf("Jump is grouped under %s, want ENTRIES", commandSection(choice.group))
		}
	}
	boundary := -1
	for i := 1; i < len(choices); i++ {
		if commandSection(choices[i-1].group) != commandSection(choices[i].group) {
			boundary = i
			break
		}
	}
	if boundary < 1 {
		t.Fatal("command choices have no section boundary")
	}
	m.palette.selected = boundary - 1
	m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
	choices = m.paletteChoices()
	rows, selectedRow := m.paletteRows(choices)
	if m.palette.selected != boundary || rows[selectedRow].choice != boundary {
		t.Fatalf("down moved selection out of sync: selected=%d row=%#v", m.palette.selected, rows[selectedRow])
	}
	m.paletteKey(tea.KeyMsg{Type: tea.KeyUp})
	choices = m.paletteChoices()
	rows, selectedRow = m.paletteRows(choices)
	if m.palette.selected != boundary-1 || rows[selectedRow].choice != boundary-1 {
		t.Fatalf("up moved selection out of sync: selected=%d row=%#v", m.palette.selected, rows[selectedRow])
	}

	section := ""
	for _, row := range rows {
		if row.choice == -2 {
			continue
		}
		if row.choice == -1 {
			section = strings.TrimSpace(strings.TrimRight(ansi.Strip(row.text), "─- "))
			continue
		}
		choice := choices[row.choice]
		if got, want := commandSection(choice.group), section; got != want {
			t.Errorf("command %q is under %q, want %q", choice.label, section, got)
		}
	}
}

func TestOpenVaultCommandUsesCtrlOPicker(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.openPalette("actions", false)
	choices := m.paletteChoices()
	openIndex := -1
	for i, choice := range choices {
		if choice.id == "open" {
			openIndex = i
			if choice.next != "" || choice.run == nil {
				t.Fatalf("Open Vault still opens a nested palette: %#v", choice)
			}
		}
	}
	if openIndex < 0 {
		t.Fatal("Open Vault command is missing")
	}
	m.palette.selected = openIndex
	cmd := m.executePalette()
	if m.palette != nil || m.screen != chooseFile || cmd == nil {
		t.Fatalf("Open Vault did not open the Ctrl+O picker: palette=%v screen=%v cmd=%v", m.palette, m.screen, cmd != nil)
	}
}

func TestCommandPaletteRendersShortcutsInLowercase(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.settings.Appearance = "classic"
	m.openPalette("actions", false)
	choices := m.paletteChoices()
	rows, _ := m.paletteRows(choices)
	want := map[string][2]string{
		"open":  {"ctrl+o", "Ctrl+O"},
		"theme": {"shift+t", "Shift+T"},
	}
	for _, row := range rows {
		if row.choice < 0 {
			continue
		}
		choice := choices[row.choice]
		checks, ok := want[choice.id]
		if !ok {
			continue
		}
		plain := ansi.Strip(row.text)
		if !strings.Contains(plain, checks[0]) || strings.Contains(plain, checks[1]) {
			t.Errorf("%s shortcut was not rendered lowercase: %q", choice.id, plain)
		}
		delete(want, choice.id)
	}
	if len(want) > 0 {
		t.Fatalf("did not check shortcuts for commands: %v", want)
	}
}

func TestPaletteDisabledReasonIsRightAlignedAndFirstGroupHasNoLeadingBlank(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 72, 24
	m.settings.Appearance = "classic"
	m.palette = &paletteState{scope: "actions", selected: 1}
	choices := []command{
		{group: "Vault", label: "Lock with PIN", reason: "Set a session PIN first"},
		{group: "Vault", label: "Open Vault", key: "ctrl+o"},
	}
	rows, _ := m.paletteRows(choices)
	checkedHeader := false
	for _, row := range rows {
		if row.choice == -1 && strings.Contains(ansi.Strip(row.text), "VAULTS") {
			checkedHeader = true
			if !strings.HasPrefix(ansi.Strip(row.text), " VAULTS") {
				t.Fatalf("classic group heading has incorrect left inset: %q", ansi.Strip(row.text))
			}
		}
		if row.choice == 0 {
			line := ansi.Strip(row.text)
			if lipgloss.Width(line) != m.paletteListWidth(choices) || !strings.HasSuffix(strings.TrimRight(line, " "), "Set a session PIN first") {
				t.Fatalf("disabled reason is not right-aligned: %q", line)
			}
		}
	}
	if !checkedHeader {
		t.Fatal("classic VAULTS group heading is missing")
	}
	_, body, _ := m.paletteBody()
	if len(body) == 0 || strings.TrimSpace(ansi.Strip(body[0])) == "" {
		t.Fatalf("first command group has an unexpected leading blank: %#v", body)
	}
}

func TestPaletteFilterPlaceholderHasStableHighlightedWidthAndRightAlignedCount(t *testing.T) {
	styles := stylesForTheme("macchiato")
	placeholderInput := input{}
	shortInput := input{}
	shortInput.set("a")
	longInput := input{}
	longInput.set("a significantly longer filter query")

	filterStyles := filterDialogStyles(&styles)
	placeholder := dialogrender.FilterInput(placeholderInput.value(), placeholderInput.cursor, "type to filter", 0, false, filterStyles)
	short := dialogrender.FilterInput(shortInput.value(), shortInput.cursor, "type to filter", 0, false, filterStyles)
	long := dialogrender.FilterInput(longInput.value(), longInput.cursor, "type to filter", 0, false, filterStyles)
	if lipgloss.Width(placeholder) != lipgloss.Width(short) {
		t.Fatalf("short active filter width %d differs from placeholder width %d", lipgloss.Width(short), lipgloss.Width(placeholder))
	}
	if lipgloss.Width(long) <= lipgloss.Width(placeholder) {
		t.Fatalf("long active filter did not expand: %d <= %d", lipgloss.Width(long), lipgloss.Width(placeholder))
	}
	if !strings.Contains(ansi.Strip(placeholder), "type to filter") {
		t.Fatalf("empty palette filter placeholder missing: %q", ansi.Strip(placeholder))
	}
	for _, query := range []input{placeholderInput, shortInput, longInput} {
		row := ansi.Strip(dialogrender.FilterRow(dialogrender.FilterInput(query.value(), query.cursor, "type to filter", 0, false, filterStyles), styles.secondary.Render("12 results"), 60, styles.primary))
		if lipgloss.Width(row) != 60 || !strings.HasSuffix(row, "12 results") {
			t.Fatalf("filter count is not right-aligned in a fixed-width row: width=%d row=%q", lipgloss.Width(row), row)
		}
	}
}

func TestClassicPaletteSharesHeaderRowWithFilterAndCount(t *testing.T) {
	styles := stylesForTheme("macchiato")
	header := dialogrender.FilterHeader("Commands", "", 0, "type to filter", "18 commands", 82, false, false, filterDialogStyles(&styles))
	stripped := ansi.Strip(header.Content)
	if lipgloss.Width(stripped) != 82 {
		t.Fatalf("classic palette header width = %d, want 82", lipgloss.Width(stripped))
	}
	if !strings.HasPrefix(stripped, "COMMANDS │ ") || !strings.Contains(stripped, "type to filter") || !strings.HasSuffix(stripped, "18 commands") {
		t.Fatalf("classic palette header did not align title, filter and count: %q", stripped)
	}
}

func TestCompactPaletteHeaderShowsSecondaryFilterPillAndPlainCount(t *testing.T) {
	m, _ := operationModel(t)
	m.width, m.height = 100, 30
	m.settings.Appearance = "compact"
	m.openPalette("actions", false)
	lines := m.paletteLines()
	count := paletteCountLabel("actions", len(m.paletteChoices()))
	var header string
	for _, line := range lines {
		row := ansi.Strip(line)
		if strings.Contains(row, "COMMANDS") {
			header = row
			break
		}
	}
	filterStart := strings.Index(header, "type to filter")
	filterPill := header[max(0, filterStart):]
	if filterStart < 0 || !strings.Contains(header, count) || strings.Contains(filterPill, "\ue0b6") || !strings.Contains(filterPill, "\ue0b4") {
		t.Fatalf("compact palette header is missing its filter pill or count: %q", header)
	}
	titleCap := strings.Index(header, "\ue0b4")
	if titleCap < 0 || header[titleCap+len("\ue0b4"):filterStart] != " " {
		t.Fatalf("compact filter spacing differs from the tab-bar + item: %q", header)
	}
	for _, line := range lines {
		row := ansi.Strip(line)
		if strings.Contains(row, "type to filter") && !strings.Contains(row, "COMMANDS") {
			t.Fatalf("compact palette filter rendered outside the header: %q", row)
		}
	}
	styles := m.colors()
	filterStyles := filterDialogStyles(styles)
	empty := dialogrender.FilterInput("", 0, "type to filter", 29, true, filterStyles)
	short := input{}
	short.set("x")
	long := input{}
	long.set(strings.Repeat("x", 40))
	if lipgloss.Width(empty) != lipgloss.Width(dialogrender.FilterInput(short.value(), short.cursor, "type to filter", 29, true, filterStyles)) {
		t.Fatal("short compact filter changed the placeholder pill width")
	}
	if got := lipgloss.Width(dialogrender.FilterInput(long.value(), long.cursor, "type to filter", 29, true, filterStyles)) + 1; got != 30 {
		t.Fatalf("compact filter pill width = %d, want 30", got)
	}
	if long.value() != strings.Repeat("x", 40) {
		t.Fatal("display width cap changed the actual filter query")
	}
}

func TestShortcutHelpUsesReusableFilteredDialogHeader(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			m := New(Options{})
			t.Cleanup(m.Close)
			m.width, m.height = 100, 30
			m.settings.Appearance = appearance
			lines := m.helpLines()
			var emptyHeader string
			for _, line := range lines {
				row := ansi.Strip(line)
				if strings.Contains(row, "KEYBOARD SHORTCUTS") {
					emptyHeader = row
					break
				}
			}
			if !strings.Contains(emptyHeader, "type to filter") || !strings.Contains(emptyHeader, "shortcuts") && appearance == "classic" {
				t.Fatalf("%s shortcuts header missing its filter/count: %q", appearance, emptyHeader)
			}
			m.helpQuery.set("lock")
			lines = m.helpLines()
			var filteredHeader string
			for _, line := range lines {
				row := ansi.Strip(line)
				if strings.Contains(row, "KEYBOARD SHORTCUTS") {
					filteredHeader = row
					break
				}
			}
			if !strings.Contains(filteredHeader, "lock") || strings.Contains(filteredHeader, "type to filter") {
				t.Fatalf("%s shortcuts filter did not become active: %q", appearance, filteredHeader)
			}
			for _, line := range lines {
				if strings.Contains(ansi.Strip(line), "Search shortcuts:") {
					t.Fatal("legacy shortcuts search row remained in dialog body")
				}
			}
		})
	}
}

func TestCtrlLLocksAllAndCurrentVaultRemainsInPalette(t *testing.T) {
	m, _ := operationModel(t)
	first := m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/lock-all-second.kdbx"))
	m.normalize()
	second := m.ws.ActiveSession()
	if first.ID == second.ID || first.DB == nil || second.DB == nil {
		t.Fatal("test requires two unlocked vaults")
	}
	header := ansi.Strip(m.workspaceLines()[0])
	if !strings.Contains(header, "2 entries · ctrl+l lock") {
		t.Fatalf("header lock hint = %q", header)
	}
	commands := m.commands()
	var current, all *command
	for i := range commands {
		cmd := &commands[i]
		if cmd.id == "lock" {
			current = cmd
		}
		if cmd.id == "lock-all" {
			all = cmd
		}
	}
	if current == nil || current.key != "" || all == nil || all.key != "Ctrl+L" {
		t.Fatalf("lock command bindings: current=%+v all=%+v", current, all)
	}
	m.key(key("ctrl+l"))
	if first.DB != nil || second.DB != nil {
		t.Fatal("Ctrl+L did not lock all open vaults")
	}

	m.ws.Open(fixture("/tmp/lock-current-first.kdbx"))
	m.normalize()
	first = m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/lock-current-second.kdbx"))
	m.normalize()
	second = m.ws.ActiveSession()
	var lockCurrent func() tea.Cmd
	for _, cmd := range m.commands() {
		if cmd.id == "lock" {
			lockCurrent = cmd.run
			break
		}
	}
	if lockCurrent == nil {
		t.Fatal("Lock Vault is missing from the command palette")
	}
	if cmd := lockCurrent(); cmd != nil {
		cmd()
	}
	if first.DB == nil || second.DB != nil {
		t.Fatal("command palette did not lock only the current vault")
	}
}
