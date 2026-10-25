package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

type memoryRecentStore struct{ paths []string }

func (s *memoryRecentStore) LoadRecent() ([]string, error) {
	return append([]string(nil), s.paths...), nil
}
func (s *memoryRecentStore) Remember(path string) error {
	s.paths = append([]string{path}, s.paths...)
	return nil
}
func (s *memoryRecentStore) Remove(path string) error {
	for i, item := range s.paths {
		if item == path {
			s.paths = append(s.paths[:i], s.paths[i+1:]...)
			break
		}
	}
	return nil
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
	m.key(key("ctrl+f"))
	m.key(key("alice"))
	if got := m.entries(); len(got) != 1 || got[0].UUID != "a" {
		t.Fatal("search didn't filter")
	}
	m.key(key("esc"))
	if len(m.entries()) != 2 {
		t.Fatal("cancel didn't restore query")
	}
	m.key(key("ctrl+f"))
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
	for _, want := range []string{"Password auth", "Could not unlock", "tab next"} {
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
	openRow := -1
	for i, line := range m.welcomeLines() {
		if strings.Contains(ansi.Strip(line), "ctrl+o") {
			openRow = i
			break
		}
	}
	cmd := m.pointer(tea.MouseMsg{X: 40, Y: openRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
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

func TestFilePickerEnterOpensHighlightedParent(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	m := New(Options{})
	m.screen = chooseFile
	m.directory = child
	m.inputs[0].set(child + string(os.PathSeparator))
	m.Update(listDirectory(child)())
	if len(m.files) == 0 || m.files[0].name != ".." {
		t.Fatal("parent row missing")
	}
	cmd := m.key(key("enter"))
	if cmd == nil {
		t.Fatal("parent navigation did not load directory")
	}
	m.Update(cmd())
	if m.directory != parent {
		t.Fatalf("Enter opened %q instead of selected parent %q", m.directory, parent)
	}
}

func TestOpenVaultPickerUsesPathAndBrowseGroups(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			m := New(Options{})
			m.width, m.height = 100, 30
			m.settings.Appearance = appearance
			m.screen = chooseFile
			m.inputs[0].set("/tmp/vaults/")
			m.files = []fileItem{{name: "..", path: "/tmp", dir: true}, {name: "Personal", path: "/tmp/vaults/Personal", dir: true}}
			rows := m.pickerLines()
			pathHeader, pathValue, browseHeader, firstItem := -1, -1, -1, -1
			for i, row := range rows {
				plain := ansi.Strip(row)
				switch {
				case strings.Contains(plain, "PATH"):
					pathHeader = i
					if appearance == "compact" && !strings.Contains(plain, "│   PATH") {
						t.Fatalf("compact group heading indentation changed: %q", plain)
					}
					if appearance == "classic" && !strings.Contains(plain, "│  PATH") {
						t.Fatalf("classic group heading should have one fewer cell of indentation: %q", plain)
					}
				case strings.Contains(plain, "/tmp/vaults/"):
					pathValue = i
					wantPath := "│  /tmp/vaults/"
					if appearance == "compact" {
						wantPath = "│   /tmp/vaults/"
					}
					if !strings.Contains(plain, wantPath) {
						t.Fatalf("%s path has incorrect left padding; want %q: %q", appearance, wantPath, plain)
					}
				case strings.Contains(plain, "BROWSE"):
					browseHeader = i
				case strings.Contains(plain, "../"):
					firstItem = i
				}
			}
			if pathHeader < 0 || pathValue != pathHeader+1 || browseHeader != pathValue+2 || firstItem != browseHeader+1 {
				plainRows := make([]string, len(rows))
				for i, row := range rows {
					plainRows[i] = ansi.Strip(row)
				}
				t.Fatalf("Open Vault picker groups are missing or out of order: PATH=%d path=%d BROWSE=%d first item=%d\n%s", pathHeader, pathValue, browseHeader, firstItem, strings.Join(plainRows, "\n"))
			}
			if appearance == "compact" && !strings.Contains(rows[firstItem], "") {
				t.Fatalf("compact picker selection did not use the shared pill list style: %q", rows[firstItem])
			}
		})
	}
}

func TestFilePickerArrowNavigationEntersPathAndHasNoTabShortcut(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.screen = chooseFile
	m.files = []fileItem{{name: "..", path: "/tmp", dir: true}, {name: "Vaults", path: "/tmp/Vaults", dir: true}}
	m.fileIndex = 1

	m.key(tea.KeyMsg{Type: tea.KeyUp})
	if m.fileIndex != 0 || m.pickerEditing {
		t.Fatalf("Up moved focus out of Browse: index=%d pathEditing=%v", m.fileIndex, m.pickerEditing)
	}
	m.key(tea.KeyMsg{Type: tea.KeyUp})
	if !m.pickerEditing {
		t.Fatal("Up from the first Browse item did not focus PATH")
	}
	m.key(tea.KeyMsg{Type: tea.KeyTab})
	if !m.pickerEditing {
		t.Fatal("Tab unexpectedly moved focus out of PATH")
	}
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	if m.pickerEditing || m.fileIndex != 0 {
		t.Fatalf("Down from PATH did not return to the first Browse item: index=%d pathEditing=%v", m.fileIndex, m.pickerEditing)
	}
	for _, row := range m.pickerLines() {
		if strings.Contains(ansi.Strip(row), "tab path") {
			t.Fatal("Open Vault footer still advertises the removed Tab shortcut")
		}
	}
}

func TestOpenVaultPickerShowsAndNavigatesRecentVaults(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.recent = []string{
		"/recent/vault-1.kdbx", "/recent/vault-2.kdbx", "/recent/vault-3.kdbx",
		"/recent/vault-4.kdbx", "/recent/vault-5.kdbx", "/recent/vault-6.kdbx",
	}
	m.openPicker()
	m.files = []fileItem{{name: "folder", path: "/folder", dir: true}}
	if !m.pickerRecent || m.pickerRecentIndex != 0 || m.pickerRecentCount() != 5 {
		t.Fatalf("picker did not focus the first of up to five recent vaults: focus=%v index=%d count=%d", m.pickerRecent, m.pickerRecentIndex, m.pickerRecentCount())
	}
	rows := m.pickerLines()
	recentHeader, pathHeader, browseHeader := -1, -1, -1
	for i, row := range rows {
		plain := ansi.Strip(row)
		if strings.Contains(plain, "RECENT") {
			recentHeader = i
		}
		if strings.Contains(plain, "PATH") {
			pathHeader = i
		}
		if strings.Contains(plain, "BROWSE") {
			browseHeader = i
		}
	}
	if recentHeader < 0 || pathHeader <= recentHeader || browseHeader <= pathHeader {
		t.Fatalf("picker groups are not ordered RECENT, PATH, BROWSE: recent=%d path=%d browse=%d", recentHeader, pathHeader, browseHeader)
	}
	for i := 1; i <= 5; i++ {
		if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), fmt.Sprintf("vault-%d.kdbx", i)) {
			t.Fatalf("recent vault %d is missing from picker", i)
		}
	}
	if strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "vault-6.kdbx") {
		t.Fatal("picker rendered more than five recent vaults")
	}

	m.key(tea.KeyMsg{Type: tea.KeyUp})
	if !m.pickerRecent || m.pickerRecentIndex != 0 {
		t.Fatal("Up moved above the first recent vault")
	}
	for i := 0; i < 4; i++ {
		m.key(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !m.pickerRecent || m.pickerRecentIndex != 4 {
		t.Fatal("Down did not move through the recent vaults")
	}
	m.key(tea.KeyMsg{Type: tea.KeyUp})
	if !m.pickerRecent || m.pickerRecentIndex != 3 {
		t.Fatal("Up did not move to the previous recent vault")
	}
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	if !m.pickerRecent || m.pickerRecentIndex != 4 {
		t.Fatal("Down did not return to the final recent vault")
	}
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	if !m.pickerEditing || m.pickerRecent {
		t.Fatal("Down from the last recent vault did not focus PATH")
	}
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	if m.pickerEditing || m.pickerRecent || m.fileIndex != 0 {
		t.Fatal("Down from PATH did not enter Browse")
	}
	m.key(tea.KeyMsg{Type: tea.KeyUp})
	if !m.pickerEditing {
		t.Fatal("Up from Browse did not focus PATH")
	}
	m.key(tea.KeyMsg{Type: tea.KeyUp})
	if !m.pickerRecent || m.pickerRecentIndex != 4 {
		t.Fatal("Up from PATH did not focus the last recent vault")
	}
	for i := 0; i < 4; i++ {
		m.key(tea.KeyMsg{Type: tea.KeyUp})
	}
	if !m.pickerRecent || m.pickerRecentIndex != 0 {
		t.Fatal("Up did not return to the first recent vault")
	}
	m.key(key("enter"))
	if m.screen != unlock || m.path != m.recent[0] {
		t.Fatalf("Enter did not open the focused recent vault: screen=%v path=%q", m.screen, m.path)
	}
}

func TestPickerRecentFooterConsumesBrowseRowAndDeleteRemovesRecent(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 35
	m.screen = chooseFile
	m.recent = []string{"/recent/one.kdbx", "/recent/two.kdbx", "/recent/three.kdbx", "/recent/four.kdbx", "/recent/five.kdbx"}
	m.files = make([]fileItem, 20)
	for i := range m.files {
		m.files[i] = fileItem{name: fmt.Sprintf("folder-%02d", i), path: fmt.Sprintf("/folder-%02d", i), dir: true}
	}
	m.store = &memoryRecentStore{paths: append([]string(nil), m.recent...)}
	m.pickerRecent = true
	_, recentCapacity := m.fileViewport()
	recentRows := strings.Join(m.pickerLines(), "\n")
	if !strings.Contains(ansi.Strip(recentRows), "delete remove recent") || strings.Contains(ansi.Strip(recentRows), "folder-15") {
		t.Fatal("Recent footer was missing or did not take the final Browse row")
	}

	m.pickerRecent = false
	_, browseCapacity := m.fileViewport()
	browseRows := strings.Join(m.pickerLines(), "\n")
	if browseCapacity != recentCapacity+1 || strings.Contains(ansi.Strip(browseRows), "delete remove recent") || !strings.Contains(ansi.Strip(browseRows), "folder-15") {
		t.Fatalf("Browse did not reclaim the Recent footer row: recent=%d browse=%d", recentCapacity, browseCapacity)
	}

	m.pickerRecent = true
	m.pickerRecentIndex = 1
	cmd := m.key(tea.KeyMsg{Type: tea.KeyDelete})
	if cmd == nil {
		t.Fatal("Delete did not schedule recent-vault removal")
	}
	msg := cmd()
	m.Update(msg)
	if len(m.recent) != 4 || m.recent[1] != "/recent/three.kdbx" || m.pickerRecentIndex != 1 || !m.pickerRecent {
		t.Fatalf("Delete did not remove the selected recent vault and preserve focus: recent=%v focus=%v index=%d", m.recent, m.pickerRecent, m.pickerRecentIndex)
	}
	m.recent = []string{"/recent/only.kdbx"}
	m.store = &memoryRecentStore{paths: append([]string(nil), m.recent...)}
	m.pickerRecent = true
	m.pickerRecentIndex = 0
	cmd = m.key(tea.KeyMsg{Type: tea.KeyDelete})
	m.Update(cmd())
	if len(m.recent) != 0 || m.pickerRecent {
		t.Fatalf("removing the last recent vault left Recent focused: recent=%v focus=%v", m.recent, m.pickerRecent)
	}
}

func TestOpenVaultPickerKeepsDialogPositionAcrossDirectorySizes(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 35
	m.settings.Appearance = "classic"
	m.screen = chooseFile
	m.recent = []string{"/recent/one.kdbx", "/recent/two.kdbx", "/recent/three.kdbx", "/recent/four.kdbx", "/recent/five.kdbx"}
	m.directory = "/first"
	m.inputs[0].set("/first/")
	m.files = []fileItem{{name: "only.kdbx", path: "/first/only.kdbx"}}
	_, capacity := m.fileViewport()
	if capacity != 16 {
		t.Fatalf("browse viewport capacity = %d, want 16", capacity)
	}
	first := m.pickerLines()
	m.directory = "/second"
	m.inputs[0].set("/second/")
	m.files = make([]fileItem, 12)
	for i := range m.files {
		m.files[i] = fileItem{name: fmt.Sprintf("folder-%02d", i), path: fmt.Sprintf("/second/folder-%02d", i), dir: true}
	}
	second := m.pickerLines()
	if len(first) != len(second) {
		t.Fatalf("picker screen height changed: %d -> %d", len(first), len(second))
	}
	for _, marker := range []string{"OPEN VAULT", "PATH", "BROWSE", "└"} {
		findRow := func(rows []string) int {
			for i, row := range rows {
				if strings.Contains(ansi.Strip(row), marker) {
					return i
				}
			}
			return -1
		}
		if a, b := findRow(first), findRow(second); a < 0 || a != b {
			t.Fatalf("picker %q position changed between directories: %d -> %d", marker, a, b)
		}
	}
}

func TestFocusedCompactPickerPathMatchesBrowseItemPillBounds(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.settings.Appearance = "compact"
	m.screen = chooseFile
	m.inputs[0].set("/tmp/vaults/")
	m.files = []fileItem{{name: "Vault", path: "/tmp/vaults/Vault", dir: true}}
	m.pickerEditing = true
	pathRow := ""
	for _, row := range m.pickerLines() {
		if strings.Contains(ansi.Strip(row), "/tmp/vaults/") {
			pathRow = ansi.Strip(row)
			break
		}
	}
	pathLeftByte, pathRightByte := strings.Index(pathRow, ""), strings.Index(pathRow, "")
	if pathLeftByte < 0 || pathRightByte < 0 {
		t.Fatalf("focused compact path is missing its pill background: %q", pathRow)
	}
	pathLeft := ansi.StringWidth(pathRow[:pathLeftByte])
	pathRight := ansi.StringWidth(pathRow[:pathRightByte])
	pathTextByte := strings.Index(pathRow, "/tmp/vaults/")
	if pathTextByte < 0 || ansi.StringWidth(pathRow[pathLeftByte:pathTextByte]) != 2 {
		t.Fatalf("focused compact path has unexpected left padding: %q", pathRow)
	}

	m.pickerEditing = false
	browseRow := ""
	for _, row := range m.pickerLines() {
		if strings.Contains(ansi.Strip(row), "Vault/") {
			browseRow = ansi.Strip(row)
			break
		}
	}
	browseLeftByte, browseRightByte := strings.Index(browseRow, ""), strings.Index(browseRow, "")
	if browseLeftByte < 0 || browseRightByte < 0 || ansi.StringWidth(browseRow[:browseLeftByte]) != pathLeft || ansi.StringWidth(browseRow[:browseRightByte]) != pathRight {
		t.Fatalf("focused path pill bounds differ from a Browse item: path=%q browse=%q", pathRow, browseRow)
	}
}

func TestFocusedClassicPickerCaretFollowsPathText(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.settings.Appearance = "classic"
	m.screen = chooseFile
	m.inputs[0].set("/tmp/vaults/")
	m.pickerEditing = true
	for _, row := range m.pickerLines() {
		plain := ansi.Strip(row)
		if strings.Contains(plain, "/tmp/vaults/") {
			if !strings.Contains(plain, "/tmp/vaults/▏ ") {
				t.Fatalf("classic path caret is not adjacent to the path with fill to its right: %q", plain)
			}
			return
		}
	}
	t.Fatal("focused classic path row was not rendered")
}

func TestFilePickerArrowEnterOpensDirectoryThenVault(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(child, "fixture.kdbx")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{})
	m.screen = chooseFile
	m.directory = parent
	m.inputs[0].set(parent + string(os.PathSeparator))
	m.Update(listDirectory(parent)())
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	cmd := m.key(key("enter"))
	if cmd == nil {
		t.Fatal("selected directory didn't open")
	}
	m.Update(cmd())
	if m.directory != child {
		t.Fatal("opened wrong directory")
	}
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	m.key(key("enter"))
	if m.screen != unlock || m.path != path {
		t.Fatal("selected vault didn't open")
	}
}

func TestFilePickerBackspaceRespectsFocus(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	m := New(Options{})
	m.screen = chooseFile
	m.directory = child
	m.inputs[0].set(child + string(os.PathSeparator))
	m.key(key("ctrl+u"))
	m.key(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.directory != child {
		t.Fatal("editing path navigated directory")
	}
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	cmd := m.key(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmd == nil {
		t.Fatal("list Backspace didn't navigate parent")
	}
	m.Update(cmd())
	if m.directory != parent {
		t.Fatal("parent directory not opened")
	}
}

func TestNumberKeysSwitchViewsIncludingLockedVault(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/first.kdbx"))
	m.ws.Open(fixture("/tmp/second.kdbx"))
	m.normalize()
	m.key(key("1"))
	if m.ws.Active != 0 {
		t.Fatal("1 did not select first view")
	}
	m.key(key("2"))
	if m.ws.Active != 1 {
		t.Fatal("2 did not select second view")
	}
	m.ws.LockActive()
	m.key(key("1"))
	if m.ws.ActiveSession().DB == nil {
		t.Fatal("could not leave locked vault with number key")
	}
	m.key(key("2"))
	if m.ws.ActiveSession().DB != nil {
		t.Fatal("number key did not select locked vault")
	}
	m.key(key("9"))
	if m.ws.Active != 1 {
		t.Fatal("unavailable view number changed selection")
	}
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'1'}})
	if m.ws.Active != 0 {
		t.Fatal("Alt+1 alias broken")
	}
}

func TestNumberKeysRemainTextInInputs(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		m := New(Options{})
		m.ws.Open(fixture("/tmp/first.kdbx"))
		m.ws.Open(fixture("/tmp/second.kdbx"))
		m.normalize()
		m.key(key("/"))
		m.key(key("1"))
		m.key(key("2"))
		if m.ws.Active != 1 || m.query.value() != "12" {
			t.Fatal("search digits triggered tab switching")
		}
	})
	t.Run("unlock", func(t *testing.T) {
		m := New(Options{})
		m.ws.Open(fixture("/tmp/first.kdbx"))
		m.ws.Open(fixture("/tmp/second.kdbx"))
		m.beginUnlock("/tmp/third.kdbx")
		m.key(key("1"))
		m.key(key("2"))
		if m.ws.Active != 1 || m.inputs[0].value() != "12" {
			t.Fatal("password digits triggered tab switching")
		}
	})
	t.Run("editor", func(t *testing.T) {
		m, _ := operationModel(t)
		m.ws.Open(fixture("/tmp/other.kdbx"))
		m.ws.ActiveSession().ReadOnly = false
		m.normalize()
		m.key(key("e"))
		m.key(key("1"))
		m.key(key("2"))
		if m.ws.Active != 1 || m.editor.inputs[editTitle].value() != "Alpha12" {
			t.Fatal("editor digits triggered tab switching")
		}
	})
}
