package ui

import (
	"fmt"
	"strings"
	"testing"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRightClickOpensContextPaletteForClickedEntry(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.ws.Open(fixture("/tmp/right-click.kdbx"))
	m.normalize()
	m.ws.ActiveView().SelectedUUID = "b"

	pane := m.layout().Panes[1]
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: pane.X + 2, Y: pane.Y + 1}
	m.pointer(msg)

	if m.palette == nil || !m.palette.context {
		t.Fatal("right click did not open the context action palette")
	}
	if got := m.ws.ActiveView().SelectedUUID; got != "a" {
		t.Fatalf("right click did not select the clicked entry: got %q", got)
	}
	if m.palette.x != msg.X || m.palette.y != msg.Y {
		t.Fatalf("context palette position = (%d,%d), want (%d,%d)", m.palette.x, m.palette.y, msg.X, msg.Y)
	}
}

func TestFilePickerViewportKeepsTwoContextRows(t *testing.T) {
	m := New(Options{})
	m.height = 30
	m.files = make([]fileItem, 40)
	m.fileIndex = 13
	start, capacity := m.fileViewport()
	if capacity != 15 || start != 1 {
		t.Fatalf("file viewport = start %d, capacity %d; want 1, 15", start, capacity)
	}
	if m.fileIndex-start != capacity-3 {
		t.Fatalf("selected file row %d, want two rows after it", m.fileIndex-start)
	}
	m.fileOffset = start
	m.fileIndex++
	next, _ := m.fileViewport()
	if m.fileIndex-next != capacity-3 {
		t.Fatalf("selected file row after scrolling = %d, want %d", m.fileIndex-next, capacity-3)
	}
}

func TestDestinationViewportAndMouseShareScrollOffset(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.ws.Open(fixture("/tmp/destination-picker.kdbx"))
	groups := make([]vault.Group, 40)
	for i := range groups {
		groups[i] = vault.Group{UUID: fmt.Sprintf("g-%02d", i), Path: fmt.Sprintf("Root/Group %02d", i)}
	}
	m.dialog = &actionDialog{kind: "destination", title: "Move Entry", entryUUID: "a", groups: groups, selected: 30}
	d := m.dialog
	start, capacity := m.destinationViewport(d.filteredGroups())
	if d.selected-start != capacity-3 {
		t.Fatalf("selected destination row %d, want two rows after it", d.selected-start)
	}
	d.offset = start
	d.selected++
	next, _ := m.destinationViewport(d.filteredGroups())
	if d.selected-next != capacity-3 {
		t.Fatalf("selected destination row after scrolling = %d, want %d", d.selected-next, capacity-3)
	}
	d.offset = next

	target := next + 4
	lines := m.dialogLines()
	row := -1
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), groupLabel(d.filteredGroups()[target].Path)) {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("destination target is not visible")
	}
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 50, Y: row}
	m.pointer(msg)
	if d.selected != target {
		t.Fatalf("mouse selected destination %d, want %d", d.selected, target)
	}
}

func TestDestinationUsesAvailableSmallScreenRows(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 16
	m.dialog = &actionDialog{kind: "destination", groups: make([]vault.Group, 20)}
	_, capacity := m.destinationViewport(m.dialog.groups)
	if capacity < 5 {
		t.Fatalf("destination list has only %d rows at 48x16", capacity)
	}
	withError := capacity
	m.dialog.err = "Could not move item"
	_, withError = m.destinationViewport(m.dialog.groups)
	if withError >= capacity {
		t.Fatal("destination capacity did not reserve room for error")
	}
}

func TestFilePickerMouseUsesScrolledViewport(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = 100, 30
			m.screen = chooseFile
			m.files = make([]fileItem, 40)
			for i := range m.files {
				m.files[i] = fileItem{name: fmt.Sprintf("vault-%02d.kdbx", i), path: fmt.Sprintf("/vault-%02d.kdbx", i)}
			}
			m.fileIndex = 12
			m.adjustViewport()
			start, _ := m.fileViewport()
			target := start + 4
			x, y := renderedTextPosition(t, m.pickerLines(), m.files[target].name)
			msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
			m.pointer(msg)
			if m.fileIndex != target {
				t.Fatalf("mouse selected file %d, want %d", m.fileIndex, target)
			}
		})
	}
}

func TestFilePickerMouseSelectsRecentAndBrowseRows(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = 100, 30
			m.screen = chooseFile
			m.recent = []string{"/recent/one.kdbx", "/recent/two.kdbx"}
			m.files = []fileItem{{name: "folder", path: "/folder", dir: true}, {name: "vault.kdbx", path: "/vault.kdbx"}}
			m.pickerRecent = true
			m.directory = "/"
			m.inputs[0].set("/")
			x, y := renderedTextPosition(t, m.pickerLines(), "two.kdbx")
			m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
			if !m.pickerRecent || m.pickerRecentIndex != 1 {
				t.Fatalf("mouse did not select second recent vault: recent=%v index=%d", m.pickerRecent, m.pickerRecentIndex)
			}
			x, y = renderedTextPosition(t, m.pickerLines(), "vault.kdbx")
			m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
			if m.pickerRecent || m.pickerEditing || m.fileIndex != 1 {
				t.Fatalf("mouse did not select second Browse item: recent=%v path=%v index=%d", m.pickerRecent, m.pickerEditing, m.fileIndex)
			}
			_, y = renderedTextPosition(t, m.pickerLines(), "PATH")
			m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y + 1})
			if !m.pickerEditing || m.pickerRecent {
				t.Fatal("mouse did not focus PATH in the reordered picker")
			}
		})
	}
}

// Find a visible target independently of the implementation's hit-test geometry.
func renderedTextPosition(t *testing.T, lines []string, text string) (int, int) {
	t.Helper()
	for y, line := range lines {
		plain := ansi.Strip(line)
		if index := strings.Index(plain, text); index >= 0 {
			return ansi.StringWidth(plain[:index]), y
		}
	}
	t.Fatalf("visible target %q missing", text)
	return 0, 0
}

func TestRightClickOutsideBrowseDoesNotOpenPalette(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 120, 35
	m.ws.Open(fixture("/tmp/right-click-unlock.kdbx"))
	m.screen = unlock
	m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 10, Y: 10})
	if m.palette != nil {
		t.Fatal("right click opened a palette outside the browse screen")
	}
}

