package ui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"keepassui/internal/vault"
)

func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
func fixture(path string) *vault.Database {
	return &vault.Database{Path: path, RootUUID: "root", Groups: []vault.Group{{UUID: "root", Name: "Root", SearchEnabled: true}, {UUID: "empty", ParentUUID: "root", Name: "Empty group", SearchEnabled: true}}, Entries: []vault.Entry{
		{UUID: "a", GroupUUID: "root", Fields: []vault.Field{{Name: "Title", Value: "Alpha"}, {Name: "UserName", Value: "alice"}, {Name: "Password", Value: "PASSWORD-DO-NOT-RENDER", Protected: true}, {Name: "Private", Value: "CUSTOM-DO-NOT-RENDER", Protected: true}}},
		{UUID: "b", GroupUUID: "root", Fields: []vault.Field{{Name: "Title", Value: "Beta"}, {Name: "Notes", Value: "public note"}}},
	}}
}
func TestRenderNeverExposesProtectedFields(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/fixture.kdbx")
	m.ws.Open(db)
	m.normalize()
	for _, size := range [][2]int{{120, 35}, {80, 24}, {20, 5}, {160, 45}} {
		m.width, m.height = size[0], size[1]
		text := m.View()
		for _, secret := range []string{"PASSWORD-DO-NOT-RENDER", "CUSTOM-DO-NOT-RENDER"} {
			if strings.Contains(text, secret) {
				t.Fatal("secret exposed")
			}
		}
		lines := strings.Split(text, "\n")
		if len(lines) != size[1] {
			t.Fatalf("height=%d want%d", len(lines), size[1])
		}
		for _, line := range lines {
			if lipgloss.Width(line) != size[0] {
				t.Fatalf("width=%d want%d", lipgloss.Width(line), size[0])
			}
		}
	}
}
func TestSearchExcludesSecretsAndFiltersLive(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/fixture.kdbx"))
	m.normalize()
	m.key(key("/"))
	m.key(key("alice"))
	if got := m.entries(); len(got) != 1 || got[0].UUID != "a" {
		t.Fatal("search didn't filter")
	}
	m.key(key("esc"))
	if len(m.entries()) != 2 {
		t.Fatal("cancel didn't restore query")
	}
	m.key(key("/"))
	m.key(key("PASSWORD-DO-NOT-RENDER"))
	m.key(key("enter"))
	if len(m.entries()) != 0 {
		t.Fatal("protected secret searchable")
	}
	m.key(key("esc"))
	if len(m.entries()) != 2 {
		t.Fatal("clear didn't restore entries")
	}
}
func TestInputIsolationAndMasking(t *testing.T) {
	m := New(Options{InitialPath: "/tmp/test.kdbx"})
	m.key(key("qjkl/"))
	if got := m.inputs[0].value(); got != "qjkl/" {
		t.Fatal("input triggered navigation")
	}
	if strings.Contains(m.View(), "qjkl/") {
		t.Fatal("password rendered")
	}
	m.key(key("esc"))
	if len(m.inputs[0].text) != 0 {
		t.Fatal("input not cleared")
	}
}
func TestTerminalEscapeInjection(t *testing.T) {
	got := safe("hello\x1b[2J\x1b]52;c;YmFk\x07world\x00\u202e")
	if strings.ContainsAny(got, "\x1b\x00\u202e") {
		t.Fatal("control bytes retained")
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatal("visible text lost")
	}
}
func TestStaleOpenResultCleared(t *testing.T) {
	m := New(Options{})
	m.request = 2
	db := fixture("/tmp/test.kdbx")
	m.Update(openedMsg{request: 1, db: db})
	if len(db.Entries) != 0 || len(m.ws.Views) != 0 {
		t.Fatal("canceled response retained")
	}
}
func TestOpenErrorSanitized(t *testing.T) {
	m := New(Options{InitialPath: "/tmp/test.kdbx"})
	m.request = 1
	m.Update(openedMsg{request: 1, err: errors.New("secret diagnostics")})
	if strings.Contains(m.View(), "secret diagnostics") || !strings.Contains(m.View(), "Could not unlock") {
		t.Fatal("backend diagnostic leaked or error absent")
	}
}

type testBackend struct{ ctx context.Context }

func (b *testBackend) Open(ctx context.Context, path string, creds vault.Credentials) (*vault.Database, error) {
	b.ctx = ctx
	return fixture(path), nil
}
func TestOpenClearsInputAndContextOnClose(t *testing.T) {
	b := &testBackend{}
	m := New(Options{Backend: b, InitialPath: "/tmp/test.kdbx"})
	m.inputs[0].set("demo-secret")
	cmd := m.startOpen()
	if cmd == nil || len(m.inputs[0].text) != 0 {
		t.Fatal("password retained")
	}
	msg := cmd()
	m.Update(msg)
	m.Close()
	if b.ctx.Err() == nil {
		t.Fatal("open context not canceled")
	}
	if len(m.ws.Views) != 0 {
		t.Fatal("views retained")
	}
}
func TestEditingUnicodeInput(t *testing.T) {
	var i input
	i.set("a猫b")
	i.update(tea.KeyMsg{Type: tea.KeyLeft})
	i.update(tea.KeyMsg{Type: tea.KeyBackspace})
	if i.value() != "ab" {
		t.Fatalf("got%q", i.value())
	}
	i.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")})
	if i.value() != "aéb" {
		t.Fatal(i.value())
	}
	i.clear()
	if i.value() != "" {
		t.Fatal("clear failed")
	}
}

func TestSearchQueryCopiesCleared(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/test.kdbx"))
	m.normalize()
	m.ws.ActiveView().Query = "prior"
	m.key(key("/"))
	m.key(key("enter"))
	if m.previousQuery != "" {
		t.Fatal("accepted search retained old query")
	}
	m.key(key("/"))
	m.key(key("esc"))
	if m.previousQuery != "" {
		t.Fatal("canceled search retained old query")
	}
	m.Close()
	if m.previousQuery != "" {
		t.Fatal("closed model retained query")
	}
}

func TestReopenRestoresGroupByUUID(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/test.kdbx")
	m.ws.Open(db)
	v := m.ws.ActiveView()
	v.NavIndex = 5
	v.GroupUUID = "empty"
	m.ws.LockActive()
	reopened := fixture("/tmp/test.kdbx")
	reopened.Groups = []vault.Group{reopened.Groups[1], reopened.Groups[0]}
	m.Update(openedMsg{db: reopened})
	if got := m.navigation()[m.ws.ActiveView().NavIndex].group; got != "empty" {
		t.Fatalf("group changed: %q", got)
	}
}

func TestViewDoesNotMutateWorkspace(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/test.kdbx"))
	m.normalize()
	before := *m.ws.ActiveView()
	m.View()
	m.View()
	if !reflect.DeepEqual(before, *m.ws.ActiveView()) {
		t.Fatal("render mutated view state")
	}
}

func TestUnlockControlsFitMinimumSize(t *testing.T) {
	m := New(Options{InitialPath: "/tmp/test.kdbx"})
	m.width, m.height = 65, 16
	m.err = "Could not unlock"
	text := m.View()
	for _, want := range []string{"Password authentication", "Could not unlock", "Enter unlock"} {
		if !strings.Contains(text, want) {
			t.Fatalf("minimum view hides %q", want)
		}
	}
}

func TestEmptyVaultAndSearchDisabledGroups(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/test.kdbx")
	db.Groups[0].SearchEnabled = false
	m.ws.Open(db)
	m.normalize()
	if len(m.entries()) != 2 {
		t.Fatal("search-disabled group should still be browsable")
	}
	m.ws.ActiveView().Query = "Alpha"
	if len(m.entries()) != 0 {
		t.Fatal("search ignored group search setting")
	}
	m.ws.Close()
	db = fixture("/tmp/empty.kdbx")
	db.Entries = nil
	m.ws.Open(db)
	m.normalize()
	if !strings.Contains(m.View(), "No entries") {
		t.Fatal("empty vault missing useful state")
	}
}

func TestAlreadyOpenVaultDoesNotRequestPasswordAgain(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/test.kdbx"))
	m.normalize()
	m.screen = chooseFile
	m.beginUnlock("/tmp/test.kdbx")
	if m.screen != browse || len(m.ws.Views) != 1 {
		t.Fatal("already-open vault required reauthentication or duplicated view")
	}
}

func TestPasswordPasteRejectsNewlines(t *testing.T) {
	m := New(Options{InitialPath: "/tmp/test.kdbx"})
	m.key(key("secret\nchanged"))
	if m.inputs[0].value() != "" || m.err == "" {
		t.Fatal("unsupported password silently transformed")
	}
}

func TestWelcomeMouseAndVaultTabMouse(t *testing.T) {
	m := New(Options{})
	top := max(1, (m.height-18)/2)
	cmd := m.pointer(tea.MouseMsg{X: 40, Y: top + 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil || m.screen != chooseFile {
		t.Fatal("Open Vault button not clickable")
	}
	m.screen = browse
	m.ws.Open(fixture("/tmp/a.kdbx"))
	m.ws.Open(fixture("/tmp/b.kdbx"))
	m.pointer(tea.MouseMsg{X: 1, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.ws.Active != 0 {
		t.Fatal("tab not clickable")
	}
}
