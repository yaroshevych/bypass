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

func TestRemovedVaultAndDeleteAliasesDoNotActivate(t *testing.T) {
	m, _ := operationModel(t)
	m.ws.ActiveView().Focus = 1
	for _, msg := range []tea.KeyMsg{key("O"), key("N"), {Type: tea.KeyCtrlO}, {Type: tea.KeyCtrlD}} {
		m.Update(msg)
		if m.screen != browse || m.dialog != nil || m.lifecycle != nil {
			t.Fatalf("removed shortcut %q opened an action", msg.String())
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

func TestCtrlWUsesInputContext(t *testing.T) {
	t.Run("editor", func(t *testing.T) {
		m, _ := operationModel(t)
		m.startEditor(nil, "root")
		m.editor.inputs[editTitle].set("first second")
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if m.editor == nil || m.editor.inputs[editTitle].value() != "first " || m.dialog != nil || len(m.ws.Views) != 1 {
			t.Fatal("Ctrl+W did not erase the preceding word in the editor")
		}
	})
	t.Run("palette filter", func(t *testing.T) {
		m, _ := operationModel(t)
		m.openPalette("actions", false)
		m.palette.query.set("first second")
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if m.palette == nil || m.palette.query.value() != "first " || len(m.ws.Views) != 1 {
			t.Fatal("Ctrl+W did not erase the preceding word in the palette")
		}
	})
	t.Run("password", func(t *testing.T) {
		m, _ := operationModel(t)
		m.startEditor(nil, "root")
		m.editor.focus = editPassword
		m.editor.inputs[editPassword].set("first second")
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if m.editor.inputs[editPassword].value() != "first " || m.dialog != nil {
			t.Fatal("Ctrl+W did not erase the preceding password word")
		}
	})
	t.Run("group input", func(t *testing.T) {
		m, _ := operationModel(t)
		m.Update(key("g"))
		m.dialog.input.set("first second")
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if m.dialog == nil || m.dialog.input.value() != "first " {
			t.Fatal("Ctrl+W did not erase the preceding group-name word")
		}
	})
	t.Run("search input", func(t *testing.T) {
		m, _ := operationModel(t)
		m.Update(key("/"))
		m.Update(key("first second"))
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if !m.searching || m.query.value() != "first " {
			t.Fatal("Ctrl+W did not erase the preceding search word")
		}
	})
	t.Run("vault picker path", func(t *testing.T) {
		m, _ := operationModel(t)
		m.openPicker()
		m.pickerEditing = true
		m.inputs[0].set("first second")
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if m.screen != chooseFile || m.inputs[0].value() != "first " {
			t.Fatal("Ctrl+W did not erase the preceding path word")
		}
	})
	t.Run("vault creation password", func(t *testing.T) {
		m, _ := operationModel(t)
		m.lifecycle = &lifecycleState{kind: "create", inputs: make([]input, 5), focus: 1}
		m.lifecycle.inputs[1].set("first second")
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if m.lifecycle == nil || m.lifecycle.inputs[1].value() != "first " {
			t.Fatal("Ctrl+W did not erase the preceding vault-password word")
		}
	})
	t.Run("workspace", func(t *testing.T) {
		m, _ := operationModel(t)
		m.duplicateTab()
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		if len(m.ws.Views) != 1 {
			t.Fatal("Ctrl+W did not close the workspace tab")
		}
	})
}

func TestCloseTabPaletteActionWorksFromDirtyEditor(t *testing.T) {
	m, _ := operationModel(t)
	m.duplicateTab()
	m.startEditor(nil, "root")
	m.editor.inputs[editTitle].set("unsaved draft")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.palette == nil {
		t.Fatal("command palette did not open from the editor")
	}
	for _, command := range m.commands() {
		if command.id == "close-view" && command.key != "" {
			t.Fatal("Close Tab advertised the workspace shortcut while editing")
		}
	}
	m.Update(key("Close Tab"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.dialog == nil || m.dialog.kind != "unsaved" || len(m.ws.Views) != 2 {
		t.Fatal("Close Tab did not ask what to do with the dirty draft")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.ws.Views) != 1 || m.editor != nil {
		t.Fatal("discarding the draft did not close the tab")
	}
}

func TestReadOnlyShortcutDoesNotInterceptEditorText(t *testing.T) {
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	m.Update(key("r"))
	if !s.ReadOnly {
		t.Fatal("r did not enable read-only mode")
	}
	m.Update(key("r"))
	if s.ReadOnly {
		t.Fatal("r did not re-enable writes")
	}
	m.Update(key("R"))
	if s.ReadOnly {
		t.Fatal("removed Shift+R binding changed read-only mode")
	}
	m.startEditor(nil, "root")
	m.Update(key("r"))
	if m.editor.inputs[editTitle].value() != "r" || s.ReadOnly {
		t.Fatal("r did not remain text inside the editor")
	}
}

func TestRemovedAltTabAliasDoesNotLeaveEditor(t *testing.T) {
	m, _ := operationModel(t)
	m.duplicateTab()
	m.startEditor(nil, "root")
	m.editor.inputs[editTitle].set("unsaved")
	active := m.ws.Active
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'1'}})
	if m.ws.Active != active || m.dialog != nil || m.editor == nil {
		t.Fatal("removed Alt+1 alias interrupted the editor")
	}
}
