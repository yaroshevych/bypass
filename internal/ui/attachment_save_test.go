package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSaveFocusedAttachment(t *testing.T) {
	dir := t.TempDir()
	backend := &extrasTestBackend{exportData: []byte("attachment contents")}
	m := newExtrasTestModel(t, extrasEntryDB(filepath.Join(dir, "vault.kdbx")), backend)
	defer m.Close()
	m.ws.ActiveSession().ReadOnly = true
	v := m.ws.ActiveView()
	v.Focus, v.FieldIndex = 2, len(m.fieldNames())
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	state := m.extras
	dest := filepath.Join(dir, "document.pdf")
	if state == nil || state.action != "save" || state.input.value() != dest {
		t.Fatalf("save dialog did not default to vault directory: %#v", state)
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.attachmentLines(state), "\n")), "SAVE ATTACHMENT") {
		t.Fatalf("save dialog title missing: %q", ansi.Strip(strings.Join(m.attachmentLines(state), "\n")))
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not start save")
	}
	m.Update(cmd())
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "attachment contents" {
		t.Fatalf("saved data %q: %v", data, err)
	}
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("save permissions: %v", err)
	}
	if m.extras != nil {
		t.Fatal("save dialog did not close after success")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.extras == nil || !strings.Contains(m.extras.err, "already exists") {
		t.Fatal("existing file was not protected")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.extras != nil {
		t.Fatal("escape did not close save dialog")
	}
}

func TestSaveRejectsAmbiguousAttachment(t *testing.T) {
	db := extrasEntryDB(filepath.Join(t.TempDir(), "vault.kdbx"))
	db.Entries[0].AttachmentNames = []string{"duplicate.txt", "duplicate.txt"}
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, db, backend)
	defer m.Close()
	m.ws.ActiveView().Focus, m.ws.ActiveView().FieldIndex = 2, len(m.fieldNames())+1
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if m.extras != nil || backend.exportCalls != 0 || !strings.Contains(m.notice, "duplicate") {
		t.Fatal("ambiguous attachment save was not rejected")
	}
}

func TestSaveDialogResponsive(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
			m := newExtrasTestModel(t, extrasEntryDB(filepath.Join(t.TempDir(), "vault.kdbx")), &extrasTestBackend{})
			m.width, m.height, m.settings.Appearance = size[0], size[1], appearance
			m.ws.ActiveView().Focus, m.ws.ActiveView().FieldIndex = 2, len(m.fieldNames())
			m.saveFocusedAttachment()
			view := ansi.Strip(strings.Join(m.attachmentLines(m.extras), "\n"))
			for _, label := range []string{"SAVE ATTACHMENT", "Save to", "enter save", "esc cancel"} {
				if !strings.Contains(view, label) {
					t.Errorf("%s %v: missing %q", appearance, size, label)
				}
			}
			m.Close()
		}
	}
}
