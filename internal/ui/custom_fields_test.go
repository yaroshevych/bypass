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

func TestCustomFieldKeyboardNavigationAndValidation(t *testing.T) {
	e := newEntryEditor(&vault.Entry{Fields: []vault.Field{{Name: "Existing", Value: "original"}}}, "root")
	e.openCustomFields()
	key := func(k tea.KeyType) { e.key(tea.KeyMsg{Type: k}) }
	key(tea.KeyDown) // Add New
	key(tea.KeyEnter)
	if !e.customEdit {
		t.Fatal("Add New did not open the field dialog")
	}
	key(tea.KeyCtrlS)
	if e.customErr == "" || !e.customEdit {
		t.Fatal("empty field name was accepted")
	}
	for _, name := range []string{"Existing", "Email", "TOTP Seed"} {
		e.customName.set(name)
		key(tea.KeyCtrlS)
		if e.customErr == "" || !e.customEdit || len(e.customFields) != 1 {
			t.Fatalf("invalid field name %q was accepted", name)
		}
	}
	e.customName.set("New field")
	key(tea.KeyTab)
	e.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	key(tea.KeyEnter)
	e.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
	key(tea.KeyTab)
	key(tea.KeySpace)
	key(tea.KeyTab)
	key(tea.KeyEnter)
	if e.customEdit || len(e.customFields) != 2 || e.customFields[1] != (vault.Field{Name: "New field", Value: "first\nsecond", Protected: true}) {
		t.Fatalf("keyboard edit produced unexpected fields: %+v", e.customFields)
	}
	key(tea.KeyDown) // Add New
	key(tea.KeyDown) // Save
	key(tea.KeyEnter)
	if e.customOpen || !e.dirty() {
		t.Fatal("list Save did not retain keyboard edits")
	}

	e.openCustomFields()
	key(tea.KeyEnter)
	e.customName.set("Discarded")
	key(tea.KeyEsc)
	if e.customEdit || e.customFields[0].Name != "Existing" {
		t.Fatal("nested Cancel did not discard the field draft")
	}
	key(tea.KeyEsc)
}

func TestCustomFieldSelectionHintOnlyOnSelectedRow(t *testing.T) {
	m := New(Options{})
	m.editor = newEntryEditor(&vault.Entry{Fields: []vault.Field{{Name: "First"}, {Name: "Second"}}}, "root")
	m.editor.openCustomFields()
	for _, selected := range []int{0, 1, 2} {
		m.editor.customIndex = selected
		opts, _ := m.customFieldsDialogOptions(70, 25)
		for i, row := range opts.Body {
			if got, want := strings.Contains(ansi.Strip(row), "enter"), i == selected; got != want {
				t.Fatalf("selection %d row %d: enter hint = %v, want %v", selected, i, got, want)
			}
		}
	}
}

func TestEmailIsStandardFieldAfterURL(t *testing.T) {
	m, _ := operationModel(t)
	m.ws.ActiveSession().DB.Entries[0].Fields = append(m.ws.ActiveSession().DB.Entries[0].Fields, vault.Field{Name: "URL", Value: "https://example.test"}, vault.Field{Name: "Email", Value: "person@example.test"}, vault.Field{Name: "Notes", Value: "notes"})
	names := m.fieldNames()
	urlIndex, emailIndex, notesIndex := -1, -1, -1
	for i, name := range names {
		switch name {
		case "URL":
			urlIndex = i
		case "Email":
			emailIndex = i
		case "Notes":
			notesIndex = i
		}
	}
	if emailIndex != urlIndex+1 || notesIndex != emailIndex+1 {
		t.Fatalf("unexpected view field order: %v", names)
	}
	e := newEntryEditor(&m.ws.ActiveSession().DB.Entries[0], "root")
	if fieldIndex("Email") != fieldIndex("URL")+1 || e.values()["Email"] != "person@example.test" {
		t.Fatalf("email was not loaded as a standard editor field: %+v", e.customFields)
	}
	for _, field := range e.customFields {
		if field.Name == "Email" {
			t.Fatal("email appeared in custom fields")
		}
	}
	e.inputs[editEmail].set("updated@example.test")
	if !e.dirty() || e.values()["Email"] != "updated@example.test" {
		t.Fatal("email edit was not captured")
	}
}

func TestEmptyStandardFieldsAreHidden(t *testing.T) {
	m, _ := operationModel(t)
	e := &m.ws.ActiveSession().DB.Entries[0]
	e.Fields = []vault.Field{{Name: "Title", Value: "Entry"}, {Name: "UserName"}, {Name: "URL"}, {Name: "Email"}, {Name: "Notes"}}
	e.HasPassword = false
	if names := m.fieldNames(); len(names) != 0 {
		t.Fatalf("empty standard fields are visible: %v", names)
	}
	e.HasPassword = true
	if names := m.fieldNames(); len(names) != 1 || names[0] != "Password" {
		t.Fatalf("password presence not reflected: %v", names)
	}
}
