package ui

import (
	"strings"
	"testing"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCustomFieldDialogEditAndAdd(t *testing.T) {
	m := New(Options{})
	m.editor = newEntryEditor(&vault.Entry{Fields: []vault.Field{
		{Name: "Account", Value: "old"}, {Name: "TOTP Seed", Value: "secret", Protected: true},
	}}, "root")
	e := m.editor
	e.openCustomFields()
	if len(e.customFields) != 1 {
		t.Fatalf("custom fields include TOTP configuration: %v", e.customFields)
	}
	view := ansi.Strip(strings.Join(m.customFieldsDialog(70, 25), "\n"))
	for _, want := range []string{"Account", "enter", "Add New", "Save", "Cancel", "esc"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list missing %q: %s", want, view)
		}
	}
	e.customKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !e.customEdit || e.customName.value() != "Account" {
		t.Fatal("enter did not open selected field")
	}
	e.customValue.set("first\nsecond")
	e.customConcealed = true
	e.saveCustomField()
	if !e.dirty() || e.customFields[0].Value != "first\nsecond" || !e.customFields[0].Protected {
		t.Fatalf("field edit was not retained: %+v", e.customFields)
	}
	e.customIndex = len(e.customFields)
	e.customKey(tea.KeyMsg{Type: tea.KeyEnter})
	e.customName.set("Additional")
	e.customValue.set("value")
	e.saveCustomField()
	if len(e.customFields) != 2 || e.customFields[1].Name != "Additional" {
		t.Fatalf("field was not added: %+v", e.customFields)
	}
	e.customKey(tea.KeyMsg{Type: tea.KeyEsc})
	if e.customOpen || len(e.customFields) != 1 || e.customFields[0].Value != "old" || e.dirty() {
		t.Fatalf("cancel did not discard dialog changes: %+v", e.customFields)
	}
	e.openCustomFields()
	e.customKey(tea.KeyMsg{Type: tea.KeyEnter})
	e.customValue.set("saved")
	e.saveCustomField()
	e.customIndex = len(e.customFields) + 1
	e.customKey(tea.KeyMsg{Type: tea.KeyEnter})
	if e.customOpen || e.customFields[0].Value != "saved" || !e.dirty() {
		t.Fatalf("save did not retain dialog changes: %+v", e.customFields)
	}
}

func TestCustomFieldDeleteRespectsListCancel(t *testing.T) {
	e := newEntryEditor(&vault.Entry{Fields: []vault.Field{{Name: "First", Value: "one"}, {Name: "Second", Value: "two"}}}, "root")
	deleteFirst := func() {
		e.customKey(tea.KeyMsg{Type: tea.KeyEnter})
		e.customFocus = 4
		e.customKey(tea.KeyMsg{Type: tea.KeyEnter})
		if e.customEdit || len(e.customFields) != 1 || e.customFields[0].Name != "Second" {
			t.Fatalf("delete did not return to the list: %+v", e.customFields)
		}
	}
	e.openCustomFields()
	deleteFirst()
	e.customKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(e.customFields) != 2 || e.customFields[0].Name != "First" || e.dirty() {
		t.Fatalf("cancel did not restore deleted field: %+v", e.customFields)
	}
	e.openCustomFields()
	deleteFirst()
	e.customIndex = len(e.customFields) + 1
	e.customKey(tea.KeyMsg{Type: tea.KeyEnter})
	if e.customOpen || len(e.customFields) != 1 || !e.dirty() {
		t.Fatalf("save did not retain deletion: %+v", e.customFields)
	}
}
