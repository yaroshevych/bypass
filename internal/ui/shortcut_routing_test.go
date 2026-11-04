package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPrintableShortcutsRemainTextInEditor(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	m.startEditor(nil, "root")
	for _, ch := range []string{"P", "L", "S"} {
		m.Update(key(ch))
	}
	if got := m.editor.inputs[editTitle].value(); got != "PLS" || m.gate.Protected() || m.ws.ActiveSession().DB == nil || (m.runs != nil && m.runs.visible) {
		t.Fatalf("printable shortcuts interrupted text entry: %q", got)
	}
}

func TestRemovedShortcutsDoNotActivate(t *testing.T) {
	m, _ := operationModel(t)
	m.ws.Open(fixture("/tmp/second-key-test.kdbx"))
	before := m.ws.Active
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Alt: true, Runes: []rune{'l'}},
		{Type: tea.KeyRunes, Alt: true, Runes: []rune{'b'}},
		{Type: tea.KeyCtrlX},
		{Type: tea.KeyCtrlB},
		{Type: tea.KeyRunes, Alt: true, Runes: []rune{'1'}},
		{Type: tea.KeyCtrlPgUp},
		{Type: tea.KeyCtrlPgDown},
	} {
		m.Update(msg)
		if m.ws.Active != before || m.ws.ActiveSession().DB == nil || (m.runs != nil && m.runs.visible) {
			t.Fatalf("removed shortcut %q changed state", msg.String())
		}
	}
}

func TestEscapePriority(t *testing.T) {
	m, _ := operationModel(t)
	m.width, m.height = 48, 16
	v := m.ws.ActiveView()
	v.Query, v.Zoom, v.Focus = "Alpha", true, 2
	m.dialog = &actionDialog{kind: "message", title: "Test"}
	m.Update(key("esc"))
	if m.dialog != nil || v.Query != "Alpha" || !v.Zoom || v.Focus != 2 {
		t.Fatal("Esc did not dismiss only the overlay")
	}
	m.Update(key("esc"))
	if v.Query != "" || !v.Zoom || v.Focus != 2 {
		t.Fatal("Esc did not clear the filter before zoom or navigation")
	}
	m.Update(key("esc"))
	if v.Zoom || v.Focus != 2 {
		t.Fatal("Esc did not exit zoom before navigating back")
	}
	m.Update(key("esc"))
	if v.Focus != 1 {
		t.Fatalf("Esc did not navigate back in narrow layout: focus %d", v.Focus)
	}
}
