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
	if capacity != 16 || start != 0 {
		t.Fatalf("file viewport = start %d, capacity %d; want 0, 16", start, capacity)
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
	m.dialog = &actionDialog{kind: "destination", title: "Move Entry", entryUUID: "a", groups: groups, selected: 12}
	d := m.dialog
	start, capacity := d.destinationViewport(m.height, d.filteredGroups())
	if capacity != 15 || start != 0 {
		t.Fatalf("destination viewport = start %d, capacity %d; want 0, 15", start, capacity)
	}
	if d.selected-start != capacity-3 {
		t.Fatalf("selected destination row %d, want two rows after it", d.selected-start)
	}
	d.offset = start
	d.selected++
	next, _ := d.destinationViewport(m.height, d.filteredGroups())
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

func TestFilePickerMouseUsesScrolledViewport(t *testing.T) {
	m := New(Options{})
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
	top := max(0, (m.height-(m.pickerBodyRows()+4))/2)
	boxWidth := min(m.width-6, 82)
	left := max(0, (m.width-boxWidth)/2)
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: left + 4, Y: top + 2 + m.pickerFixedRows() + target - start}
	m.pointer(msg)
	if m.fileIndex != target {
		t.Fatalf("mouse selected file %d, want %d", m.fileIndex, target)
	}
}

func TestFilePickerMouseSelectsRecentAndBrowseRows(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.screen = chooseFile
	m.recent = []string{"/recent/one.kdbx", "/recent/two.kdbx"}
	m.files = []fileItem{{name: "folder", path: "/folder", dir: true}, {name: "vault.kdbx", path: "/vault.kdbx"}}
	m.pickerRecent = true
	m.directory = "/"
	m.inputs[0].set("/")
	_, capacity := m.fileViewport()
	bodyLen := m.pickerFixedRows() + capacity + 1
	top := max(0, (m.height-(min(bodyLen, max(1, m.height-8))+4))/2)
	boxWidth := min(m.width-6, 82)
	left := max(0, (m.width-boxWidth)/2)

	m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: left + 4, Y: top + 4})
	if !m.pickerRecent || m.pickerRecentIndex != 1 {
		t.Fatalf("mouse did not select second recent vault: recent=%v index=%d", m.pickerRecent, m.pickerRecentIndex)
	}
	fileStartY := top + 2 + m.pickerFixedRows()
	m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: left + 4, Y: fileStartY + 1})
	if m.pickerRecent || m.pickerEditing || m.fileIndex != 1 {
		t.Fatalf("mouse did not select second Browse item: recent=%v path=%v index=%d", m.pickerRecent, m.pickerEditing, m.fileIndex)
	}
	pathY := top + 2 + m.pickerPathRow()
	m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: left + 4, Y: pathY})
	if !m.pickerEditing || m.pickerRecent {
		t.Fatal("mouse did not focus PATH in the reordered picker")
	}
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

func TestResizeFocusedUsesVisibleResponsiveDivider(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		focus     int
		zoom      bool
		paneIndex int
		wantDelta int
	}{
		{"wide sidebar", 140, 0, false, 0, 2},
		{"wide entries", 140, 1, false, 1, 2},
		{"wide details", 140, 2, false, 2, 2},
		{"medium sidebar", 100, 0, false, 0, 2},
		{"medium entries", 100, 1, false, 1, 2},
		{"medium details", 100, 2, false, 1, 2},
		{"narrow fixed", 70, 1, false, 0, 0},
		{"zoom fixed", 140, 2, true, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Options{})
			m.width, m.height = tt.width, 30
			m.ws.Open(fixture("/tmp/resize-" + tt.name + ".kdbx"))
			v := m.ws.ActiveView()
			v.Focus, v.Zoom = tt.focus, tt.zoom
			before := m.layout()
			beforeWidth := before.Panes[tt.paneIndex].Width

			m.resizeFocused(2)

			after := m.layout()
			if got := after.Panes[tt.paneIndex].Width - beforeWidth; got != tt.wantDelta {
				t.Fatalf("focused pane width delta = %d, want %d (before=%+v after=%+v)", got, tt.wantDelta, before.Panes, after.Panes)
			}
		})
	}
}
