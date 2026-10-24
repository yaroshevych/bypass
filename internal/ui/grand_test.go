package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func focusGrandPassword(m *Model) {
	v := m.ws.ActiveView()
	v.Focus = 2
	v.FieldIndex = 1 // UserName, then Password.
}

func setGrandPassword(t *testing.T, m *Model, value string) {
	t.Helper()
	for i := range m.ws.ActiveSession().DB.Entries[0].Fields {
		if m.ws.ActiveSession().DB.Entries[0].Fields[i].Name == "Password" {
			m.ws.ActiveSession().DB.Entries[0].Fields[i].Value = value
			return
		}
	}
	t.Fatal("fixture has no Password field")
}

func TestGrandViewShowsCharactersAndOneBasedIndexesThenZeroesOnClose(t *testing.T) {
	m, _ := operationModel(t)
	focusGrandPassword(m)
	setGrandPassword(t, m, "abc")
	m.key(key("G"))
	if !m.grand || string(m.revealValue) != "abc" {
		t.Fatalf("Grand View did not open on the focused field: grand=%v value=%q", m.grand, m.revealValue)
	}
	view := ansi.Strip(strings.Join(m.grandLines(), "\n"))
	if !strings.Contains(view, "3 characters · positions start at 1") || !strings.Contains(view, "a") || !strings.Contains(view, "b") || !strings.Contains(view, "c") {
		t.Fatalf("Grand View is missing its field values or position legend:\n%s", view)
	}
	if !regexp.MustCompile(`\b1\s+2\s+3\b`).MatchString(view) {
		t.Fatalf("Grand View does not show 1-based character indexes:\n%s", view)
	}

	backing := m.revealValue
	m.key(key("esc"))
	if m.grand || len(m.revealValue) != 0 {
		t.Fatal("closing Grand View retained its reveal state")
	}
	for _, b := range backing {
		if b != 0 {
			t.Fatal("closing Grand View did not zero its reveal buffer")
		}
	}
	if strings.Contains(m.View(), "abc") {
		t.Fatal("closed Grand View continued to show the password")
	}
}

func TestGrandViewUnicodeAndControlCharactersAreSafeAndRuneIndexed(t *testing.T) {
	m, _ := operationModel(t)
	focusGrandPassword(m)
	value := "A界e\u0301\t\x1b[2J"
	setGrandPassword(t, m, value)
	m.key(key("G"))
	plain := ansi.Strip(strings.Join(m.grandLines(), "\n"))
	wantRunes := len([]rune(value))
	if !strings.Contains(plain, fmt.Sprintf("%d characters", wantRunes)) {
		t.Fatalf("Grand View did not report %d runes:\n%s", wantRunes, plain)
	}
	for _, want := range []string{"A", "界", "◌\u0301", `\t`, "U+001B"} {
		if !strings.Contains(plain, want) {
			t.Errorf("Grand View missing safe rendering %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "\x1b[2J") {
		t.Fatal("stored control sequence was emitted as a terminal escape")
	}
	if got := grandCharacter('\x1b', false); got != "U+001B" {
		t.Fatalf("ESC character label = %q, want U+001B", got)
	}
	if got := grandCharacter('\u202e', false); got != "U+202E" {
		t.Fatalf("format control label = %q, want U+202E", got)
	}
	for _, r := range plain {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("rendered content contains control rune U+%04X", r)
		}
	}
}

func TestGrandViewScrollsToFinalCharacters(t *testing.T) {
	m, _ := operationModel(t)
	focusGrandPassword(m)
	value := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	setGrandPassword(t, m, value)
	m.width, m.height = 48, 14
	m.key(key("G"))
	if columns, visible, total := m.grandGeometry(); columns != 10 || visible != 1 || total != 4 || m.grandOffset != 0 {
		t.Fatalf("small Grand View geometry = %d columns, %d visible rows, %d total rows, offset %d", columns, visible, total, m.grandOffset)
	}
	m.key(tea.KeyMsg{Type: tea.KeyEnd})
	view := ansi.Strip(strings.Join(m.grandLines(), "\n"))
	if m.grandOffset != 3 || !strings.Contains(view, "36") || !strings.Contains(view, "Z") {
		t.Fatalf("Grand View did not reach the final indexed character:\n%s", view)
	}
}

func TestGrandViewResolvesReferencesAndDiscardsLateResults(t *testing.T) {
	m, _ := operationModel(t)
	focusGrandPassword(m)
	setGrandPassword(t, m, "{REF:P@I:synthetic}")
	cmd := m.key(key("G"))
	if cmd == nil || !m.grand || m.revealCancel == nil {
		t.Fatal("reference Grand View did not start asynchronous resolution")
	}
	m.Update(cmd())
	if got := string(m.revealValue); got != "RESOLVED-SECRET" || !m.grand {
		t.Fatalf("resolved Grand View value = %q, grand=%v", got, m.grand)
	}

	m.key(key("esc"))
	late := m.key(key("G"))
	if late == nil || !m.grand {
		t.Fatal("second reference resolution did not start")
	}
	m.key(key("esc"))
	m.Update(late())
	if m.grand || len(m.revealValue) != 0 || strings.Contains(m.View(), "RESOLVED-SECRET") {
		t.Fatal("late reference result reopened Grand View or retained its secret")
	}
}

func TestPINAndFullLockCloseGrandView(t *testing.T) {
	t.Run("PIN protection", func(t *testing.T) {
		m, _ := operationModel(t)
		focusGrandPassword(m)
		setGrandPassword(t, m, "abc")
		m.key(key("G"))
		backing := m.revealValue
		if err := m.gate.Configure([]byte("1234")); err != nil {
			t.Fatal(err)
		}
		m.key(tea.KeyMsg{Type: tea.KeyCtrlK})
		if !m.gate.Protected() || m.grand || len(m.revealValue) != 0 {
			t.Fatal("PIN protection did not close Grand View and clear its buffer")
		}
		for _, b := range backing {
			if b != 0 {
				t.Fatal("PIN protection did not zero the Grand View buffer")
			}
		}
	})

	t.Run("full lock", func(t *testing.T) {
		m, _ := operationModel(t)
		focusGrandPassword(m)
		setGrandPassword(t, m, "abc")
		m.key(key("G"))
		backing := m.revealValue
		m.key(tea.KeyMsg{Type: tea.KeyCtrlL})
		if m.grand || len(m.revealValue) != 0 || m.ws.ActiveSession().DB != nil {
			t.Fatal("full lock did not close Grand View and lock the vault")
		}
		for _, b := range backing {
			if b != 0 {
				t.Fatal("full lock did not zero the Grand View buffer")
			}
		}
	})
}

func TestGrandViewNotesAndMultilineFieldsShowPlainText(t *testing.T) {
	for _, field := range []string{"Notes", "Custom", "Password"} {
		t.Run(field, func(t *testing.T) {
			m, _ := operationModel(t)
			m.grand = true
			m.revealField = field
			m.revealValue = []byte("First line\r\n\r\n  Indented\ttext\nLast line\x1b[2J")
			rows := m.grandTextRows()
			if got := strings.Join(rows, "\n"); got != "First line\n\n  Indented    text\nLast line" {
				t.Fatalf("multiline text lost formatting or terminal safety: %q", got)
			}
			view := ansi.Strip(strings.Join(m.grandLines(), "\n"))
			if !strings.Contains(view, "First line") || !strings.Contains(view, "Last line") || strings.Contains(view, "positions start at") {
				t.Fatal("multiline field used the character grid")
			}
		})
	}
}

func TestGrandViewSingleLineNotesStayPlainAndLongNotesScroll(t *testing.T) {
	m, _ := operationModel(t)
	m.grand = true
	m.revealField = "Notes"
	m.revealValue = []byte("Single line note")
	view := ansi.Strip(strings.Join(m.grandLines(), "\n"))
	if !strings.Contains(view, "Single line note") || strings.Contains(view, "positions start at") {
		t.Fatal("single line notes used the character grid")
	}
	m.width, m.height = 48, 14
	m.revealValue = []byte(strings.Repeat("Wrapped notes need the full terminal width. ", 10) + "FINAL")
	m.key(tea.KeyMsg{Type: tea.KeyEnd})
	view = ansi.Strip(strings.Join(m.grandLines(), "\n"))
	if m.grandOffset == 0 || !strings.Contains(view, "FINAL") {
		t.Fatal("cannot scroll to the end of wrapped notes")
	}
}
