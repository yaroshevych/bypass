package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

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
