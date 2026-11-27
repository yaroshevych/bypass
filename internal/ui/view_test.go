package ui

import (
	"strings"
	"testing"

	"bypass/internal/vault"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestDetailShowsAttachmentNames(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/attachments.kdbx")
	db.Entries[0].AttachmentNames = []string{"document.pdf", "photo.png"}
	m.ws.Open(db)
	m.normalize()
	lines, _ := m.detailLayout(40)
	var plain []string
	for _, line := range lines {
		plain = append(plain, ansi.Strip(line))
	}
	view := strings.Join(plain, "\n")
	if !strings.Contains(view, "ATTACHMENTS (2)\n document.pdf\n photo.png") {
		t.Fatalf("attachment list missing from details: %q", view)
	}
	if strings.Count(view, "ATTACHMENTS (2)") != 1 {
		t.Fatalf("attachment header repeated: %q", view)
	}
}

func TestAttachmentFocusNavigation(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	db := fixture("/tmp/attachment-focus.kdbx")
	db.Entries[0].AttachmentNames = []string{"first.pdf", "second.png"}
	m.ws.Open(db)
	m.normalize()
	v := m.ws.ActiveView()
	v.Focus, v.FieldIndex = 2, len(m.fieldNames())-1
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.FieldIndex != len(m.fieldNames()) || !m.attachmentFocused() {
		t.Fatal("down did not focus the first attachment")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.FieldIndex != len(m.fieldNames())+1 {
		t.Fatal("down did not focus the second attachment")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if v.FieldIndex != len(m.fieldNames()) {
		t.Fatal("up did not return to the first attachment")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if v.FieldIndex != len(m.fieldNames())+1 {
		t.Fatal("tab did not focus the second attachment")
	}
	if m.copyField("") != nil || m.toggleReveal() != nil {
		t.Fatal("attachment focus triggered a field action")
	}
}

func TestEntryListColumnsFollowTerminalWidth(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/list-columns.kdbx"))
	for _, tc := range []struct {
		width int
		all   bool
	}{{89, false}, {90, false}, {119, false}, {120, true}} {
		m.width, m.height = tc.width, 30
		heading := ansi.Strip(m.entryLines(80, 5, true)[0])
		if strings.Contains(heading, "USERNAME") != tc.all || strings.Contains(heading, "WEBSITE") != tc.all {
			t.Errorf("terminal width %d: unexpected heading %q", tc.width, heading)
		}
	}
}

func TestFocusedDetailFieldUsesCompactPillAndScrollsTopFieldToTop(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 100, 30
	m.settings.Appearance = "compact"
	m.ws.Open(fixture("/tmp/detail-focus.kdbx"))
	m.normalize()
	v := m.ws.ActiveView()
	v.Focus = 2
	v.FieldIndex = 0
	v.DetailOffset = 8

	lines, positions := m.detailLayout(40)
	if len(positions) == 0 || positions[0] >= len(lines) {
		t.Fatal("focused field position is missing")
	}
	labelRow := -1
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), "USERNAME") {
			labelRow = i
			break
		}
	}
	if labelRow < 0 || strings.Contains(ansi.Strip(lines[labelRow]), "›") {
		t.Fatal("field label retained the focus marker")
	}
	focusedValue := ansi.Strip(lines[positions[0]])
	if !strings.Contains(focusedValue, "alice") || strings.Contains(focusedValue, "›") {
		t.Fatalf("focus position does not identify the value row: %q", focusedValue)
	}
	if !strings.Contains(lines[positions[0]], "") || !strings.Contains(lines[positions[0]], "") {
		t.Fatalf("focused compact value is not rendered as a pill: %q", lines[positions[0]])
	}

	m.adjustViewport()
	if v.DetailOffset != 0 {
		t.Fatalf("focusing the first field left detail offset at %d, want 0", v.DetailOffset)
	}
}

func TestScrollbarsShowOverflowAndPosition(t *testing.T) {
	m, _ := operationModel(t)
	m.width, m.height = 100, 24
	m.settings.ASCII = true
	m.ws.ActiveSession().Database().Entries[0].Fields = append(m.ws.ActiveSession().Database().Entries[0].Fields, vault.Field{Name: "Notes", Value: strings.Repeat("line\n", 60)})
	v := m.ws.ActiveView()
	v.Focus = 2
	lines := m.detailLines(40, 10, true)
	if !strings.HasSuffix(ansi.Strip(lines[0]), "#") || !strings.HasSuffix(ansi.Strip(lines[9]), "|") {
		t.Fatal("details scrollbar did not start at the top")
	}
	v.DetailOffset = 1000
	lines = m.detailLines(40, 10, true)
	if !strings.HasSuffix(ansi.Strip(lines[9]), "#") {
		t.Fatal("details scrollbar did not reach the bottom")
	}
	m.openPalette("actions")
	palette := m.paletteLines()
	paletteRight := (m.width-m.paletteDialogWidth(m.paletteChoices()))/2 + m.paletteDialogWidth(m.paletteChoices()) - 1
	if !columnContains(palette, paletteRight, "#") {
		t.Fatal("overflowing command palette has no scrollbar")
	}
	m.screen = chooseFile
	m.files = make([]fileItem, 50)
	pickerRight := (m.width-min(m.width-6, 60))/2 + min(m.width-6, 60) - 1
	if !columnContains(m.pickerLines(), pickerRight, "#") {
		t.Fatal("overflowing vault picker has no scrollbar")
	}
	m.files = m.files[:1]
	if columnContains(m.pickerLines(), pickerRight, "#") {
		t.Fatal("vault picker showed a scrollbar without overflow")
	}
}

func columnContains(lines []string, column int, marker string) bool {
	for _, line := range lines {
		if ansi.Strip(ansi.Cut(line, column, column+1)) == marker {
			return true
		}
	}
	return false
}

func TestDialogScrollbarsAlignWithFrameInBothAppearances(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			m, _ := operationModel(t)
			m.width, m.height = 100, 20
			m.settings.ASCII, m.settings.Appearance = true, appearance
			m.openPalette("actions")
			width := m.paletteDialogWidth(m.paletteChoices())
			if !columnContains(m.paletteLines(), (m.width-width)/2+width-1, "#") {
				t.Fatal("palette thumb is not on the frame")
			}
			m.screen = chooseFile
			m.files = make([]fileItem, 50)
			width = min(m.width-6, 60)
			if !columnContains(m.pickerLines(), (m.width-width)/2+width-1, "#") {
				t.Fatal("picker thumb is not on the frame")
			}
		})
	}
}
