package ui

import (
	"strings"
	"testing"

	"bypass/internal/vault"
	"github.com/charmbracelet/x/ansi"
)

func TestModalBackdropDimsTabPillBackground(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.settings.Appearance = "compact"
	m.ws.Open(fixture("/tmp/modal-pill.kdbx"))
	m.normalize()
	tabLine := m.workspaceLines()[1]
	if !strings.Contains(tabLine, "\ue0b6") || !strings.Contains(tabLine, "Alpha") {
		t.Fatal("test setup did not render an active tab pill")
	}
	backdropLine := m.modalBackdrop()[1]
	if !strings.Contains(backdropLine, "\ue0b6") || !strings.Contains(ansi.Strip(backdropLine), "Alpha") {
		t.Fatal("modal backdrop erased the tab text")
	}
}

func TestDimANSIColorsScalesForegroundAndBackground(t *testing.T) {
	got := dimANSIColors("\x1b[38;2;100;200;50;48;2;220;180;90mX\x1b[0m", .6, .5, "")
	want := "\x1b[38;2;60;120;30;48;2;110;90;45mX\x1b[0m"
	if got != want {
		t.Fatalf("dimmed ANSI colors = %q, want %q", got, want)
	}
}

func TestModalSurfaceAndScrimUseSameBackgroundFactor(t *testing.T) {
	got := dimANSIColors("\x1b[48;2;40;42;54mX", .60, .58, "#282a36")
	want := "\x1b[48;2;31;32;42mX"
	if got != want {
		t.Fatalf("modal surface dim = %q, want it to match scrim %q", got, want)
	}
}

func TestModalDimsColoredPillsMoreThanSurface(t *testing.T) {
	got := dimANSIColors("\x1b[48;2;250;189;47mPill", .60, .58, "#282a36")
	want := "\x1b[48;2;145;109;27mPill"
	if got != want {
		t.Fatalf("colored modal pill shade = %q, want %q", got, want)
	}
}

func TestClassicDialogTitlesAreUppercaseAndHeaderRulesJoinFrame(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.settings.Appearance = "classic"
	for name, lines := range map[string][]string{
		"box":     m.box("Change Vault Credentials", []string{"body"}, ""),
		"overlay": m.overlay("History", []string{"body"}, "", 60, 0, 0),
	} {
		stripped := make([]string, len(lines))
		for i, line := range lines {
			stripped[i] = ansi.Strip(line)
		}
		joined := strings.Join(stripped, "\n")
		var title string
		if name == "box" {
			title = "CHANGE VAULT CREDENTIALS"
		} else {
			title = "HISTORY"
		}
		titleRow := -1
		for i, line := range stripped {
			if strings.Contains(line, title) {
				titleRow = i
				break
			}
		}
		if titleRow < 0 {
			t.Fatalf("%s dialog title not uppercase: %q", name, joined)
		}
		headerLine := strings.TrimSpace(stripped[titleRow+1])
		header := []rune(headerLine)
		if len(header) < 2 || header[0] != '├' || header[len(header)-1] != '┤' {
			t.Fatalf("%s header rule does not join the frame: %q", name, headerLine)
		}
	}
}

func TestDialogFootersAreRightAlignedWithoutSeparator(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	for name, lines := range map[string][]string{
		"box":     m.box("Test Dialog", []string{"body"}, "c copy"),
		"overlay": m.overlay("Test Dialog", []string{"body"}, "c copy", 60, -1, -1),
	} {
		var stripped []string
		for _, line := range lines {
			stripped = append(stripped, ansi.Strip(line))
		}
		footerRow := -1
		for i, line := range stripped {
			if strings.Contains(line, "c copy") {
				footerRow = i
				break
			}
		}
		if footerRow < 1 {
			t.Fatalf("%s footer was not rendered: %q", name, strings.Join(stripped, "\n"))
		}
		line := stripped[footerRow]
		left, right := strings.Index(line, "│"), strings.LastIndex(line, "│")
		start := strings.Index(line, "c copy")
		if left < 0 || right <= start || start-left <= right-(start+len("c copy")) {
			t.Fatalf("%s footer is not right-aligned: %q", name, line)
		}
		previous := stripped[footerRow-1]
		prevLeft, prevRight := strings.Index(previous, "│"), strings.LastIndex(previous, "│")
		if prevLeft >= 0 && prevRight > prevLeft && strings.Contains(previous[prevLeft+1:prevRight], "─") {
			t.Fatalf("%s has a horizontal separator above its footer: %q", name, previous)
		}
	}
}

func TestMoveDialogUsesEntryTitleAndOmitsFilterAndDuplicateTitle(t *testing.T) {
	m, _ := operationModel(t)
	m.chooseDestination(m.selected())
	text := ansi.Strip(strings.Join(m.dialogLines(), "\n"))
	if !strings.Contains(text, "MOVE ALPHA") {
		t.Fatalf("move dialog title does not identify the entry: %q", text)
	}
	if strings.Contains(text, "type to filter") || len(m.dialog.message) != 0 {
		t.Fatalf("move dialog retained a filter control or duplicate item-title body: %q", text)
	}
	if len(m.dialog.filteredGroups()) != len(m.dialog.groups) {
		t.Fatal("move destination list should not be filtered")
	}
}

func TestRestoreDialogUsesEntryTitleAndConnectedClassicHeader(t *testing.T) {
	m, _ := operationModel(t)
	m.settings.Appearance = "classic"
	e := m.selected()
	e.Recycled = true
	m.chooseDestination(e)
	lines := m.dialogLines()
	var stripped []string
	for _, line := range lines {
		stripped = append(stripped, ansi.Strip(line))
	}
	joined := strings.Join(stripped, "\n")
	if !strings.Contains(joined, "RESTORE ALPHA") {
		t.Fatalf("restore dialog title does not identify the entry: %q", joined)
	}
	if len(stripped) < 2 {
		t.Fatalf("restore dialog is missing its title header: %q", joined)
	}
	joinedHeader := false
	for _, line := range stripped {
		if strings.Contains(line, "├") && strings.Contains(line, "┤") {
			joinedHeader = true
			break
		}
	}
	if !joinedHeader {
		t.Fatalf("restore dialog header rule does not join the frame: %q", joined)
	}
}

func TestTinyTerminalKeepsDraftWithoutAcceptingHiddenInput(t *testing.T) {
	m, _ := operationModel(t)
	m.key(key("n"))
	m.editor.inputs[editTitle].set("Keep draft")
	m.width, m.height = 20, 5
	m.key(key("x"))
	m.key(key("q"))
	if m.editor == nil || m.editor.inputs[editTitle].value() != "Keep draft" || m.dialog != nil {
		t.Fatal("tiny state changed the hidden draft or opened a hidden confirmation")
	}
	m.width, m.height = 120, 35
	if !m.editor.dirty() {
		t.Fatal("resizing lost the draft")
	}
}

func TestRecentDestinationsOmitInvalidGroupsAndClearOnLock(t *testing.T) {
	db := &vault.Database{Groups: []vault.Group{{UUID: "a"}, {UUID: "b"}, {UUID: "trash", Recycled: true}}}
	groups := destinationGroups(db, []string{"missing", "trash", "b", "b"})
	if len(groups) != 2 || groups[0].UUID != "b" || groups[1].UUID != "a" {
		t.Fatalf("invalid recent destination order: %#v", groups)
	}
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	s.Destinations = []string{"private-group"}
	m.lockSession(s.ID)
	if len(s.Destinations) != 0 {
		t.Fatal("full lock kept destination history")
	}
}

func TestTinyTerminalDoesNotAcceptHiddenPINInput(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	m.gate.Protect()
	m.width, m.height = 20, 5
	m.key(key("1234"))
	m.key(key("enter"))
	if !m.gate.Protected() || m.pinInput.value() != "" {
		t.Fatal("hidden PIN form accepted input")
	}
	m.width, m.height = 120, 35
	m.key(key("1234"))
	m.key(key("enter"))
	if m.gate.Protected() {
		t.Fatal("resizing did not restore the PIN form")
	}
}
