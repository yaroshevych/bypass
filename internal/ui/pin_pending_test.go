package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPINProtectsPendingSaveWithoutCancelingIt(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	m.Update(key("e"))
	m.Update(key(" Draft"))
	_, save := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if save == nil || !m.writing {
		t.Fatal("save was not started")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if !m.gate.Protected() || !m.writing || m.editor == nil {
		t.Fatal("PIN must mask a pending save while retaining its draft and operation")
	}
	if view := m.View(); strings.Contains(view, "Draft") || strings.Contains(view, "fixture") {
		t.Fatal("protected screen exposed workspace data")
	}
	m.Update(save())
	if !m.gate.Protected() || m.writing || m.editor != nil {
		t.Fatal("save did not finish behind the protected screen")
	}
	m.Update(key("1234"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.gate.Protected() || !strings.Contains(m.View(), "Draft") {
		t.Fatal("PIN unlock did not restore the saved item")
	}
}
