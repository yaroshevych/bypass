package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestEditorAttachmentsStageAndCancel(t *testing.T) {
	e := newEntryEditor(&vault.Entry{AttachmentNames: []string{"old.png", "keep.pdf"}}, "root")
	e.setAttachmentSizes([]int64{1234, 42})
	e.openEditorAttachments()
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !e.attachmentEdit || e.attachmentName.value() != "old.png" {
		t.Fatal("existing attachment did not open")
	}
	e.attachmentName.set("renamed.png")
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !e.attachmentsChanged() || e.attachments[0].Name != "renamed.png" {
		t.Fatal("rename was not staged")
	}
	e.attachmentIndex = 1
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	e.attachmentFocus = 2
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(e.attachments) != 1 {
		t.Fatal("delete did not stage")
	}
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	e.attachmentName.set("/tmp/new.png")
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if len(e.attachments) != 2 || e.attachments[1].Name != "new.png" {
		t.Fatal("new file not staged")
	}
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEsc})
	if e.attachmentsOpen || e.dirty() || len(e.attachments) != 2 || e.attachments[0].Name != "old.png" {
		t.Fatal("cancel did not restore attachments")
	}
	e.openEditorAttachments()
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	e.attachmentName.set("/tmp/new.png")
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	e.attachmentIndex = len(e.attachments) + 1
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	if e.attachmentsOpen || !e.dirty() {
		t.Fatal("list save did not retain attachments")
	}
	edits, paths := e.attachmentEdits()
	if len(edits) != 3 || edits[0].OriginalIndex == nil || *edits[0].OriginalIndex != 0 || edits[2].OriginalIndex != nil || paths[2] != "/tmp/new.png" || edits[2].Name != "new.png" {
		t.Fatalf("unexpected attachment changes: %+v paths %v", edits, paths)
	}
}

func TestEditorAttachmentsDialogSizeAndValidation(t *testing.T) {
	m := New(Options{})
	m.editor = newEntryEditor(&vault.Entry{AttachmentNames: []string{"existing.png"}}, "root")
	e := m.editor
	e.setAttachmentSizes([]int64{2048})
	if e.dirty() {
		t.Fatal("loading size marked editor dirty")
	}
	e.openEditorAttachments()
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	view := ansi.Strip(strings.Join(m.editorAttachmentsDialog(70, 25), "\n"))
	for _, want := range []string{"File name", "existing.png", "File size: 2048 bytes", "Save", "Delete", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dialog missing %q: %s", want, view)
		}
	}
	e.attachmentName.set("invalid/path")
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if e.attachmentErr == "" || !e.attachmentEdit {
		t.Fatal("invalid rename was accepted")
	}
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEsc})
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEsc})
	if e.dirty() {
		t.Fatal("canceled edit changed attachment")
	}
}

func TestEditorAttachmentsNewEntryAndDeleteAll(t *testing.T) {
	e := newEntryEditor(nil, "root")
	e.openEditorAttachments()
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	e.attachmentName.set("/tmp/picture.jpg")
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	e.attachmentIndex = len(e.attachments) + 1
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	edits, paths := e.attachmentEdits()
	if len(edits) != 1 || edits[0].Name != "picture.jpg" || paths[0] != "/tmp/picture.jpg" {
		t.Fatal("new entry attachment not staged")
	}
	old := newEntryEditor(&vault.Entry{AttachmentNames: []string{"only"}}, "root")
	old.openEditorAttachments()
	old.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	old.attachmentFocus = 2
	old.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	old.attachmentIndex = 1
	old.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	edits, _ = old.attachmentEdits()
	if edits == nil || len(edits) != 0 {
		t.Fatal("delete all must produce non-nil empty changes")
	}
}

func TestEditorAttachmentsRejectDuplicateAndNewFormHasNoDelete(t *testing.T) {
	m := New(Options{})
	m.editor = newEntryEditor(&vault.Entry{AttachmentNames: []string{"one.txt", "two.txt"}}, "root")
	e := m.editor
	e.openEditorAttachments()
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEnter})
	e.attachmentName.set("two.txt")
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if e.attachmentErr == "" || e.attachments[0].Name != "one.txt" {
		t.Fatal("duplicate rename was accepted")
	}
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyEsc})
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	view := ansi.Strip(strings.Join(m.editorAttachmentsDialog(70, 25), "\n"))
	if strings.Contains(view, "Delete") {
		t.Fatal("new attachment form offers delete")
	}
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyTab})
	e.attachmentKey(tea.KeyMsg{Type: tea.KeyTab})
	if e.attachmentFocus != 3 {
		t.Fatal("new attachment form did not skip delete")
	}
}

func TestStagedAttachmentShowsFilenameAndSizeAndKeepsSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(path, []byte("1234567"), 0600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{})
	defer m.Close()
	m.editor = newEntryEditor(nil, "root")
	e := m.editor
	e.openEditorAttachments()
	m.editorKey(tea.KeyMsg{Type: tea.KeyEnter})
	e.attachmentName.set(path)
	cmd := m.editorKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("attachment size lookup was not started")
	}
	m.Update(cmd())
	m.editorKey(tea.KeyMsg{Type: tea.KeyEnter})
	if e.attachmentName.value() != "original.txt" {
		t.Fatalf("edit name = %q", e.attachmentName.value())
	}
	view := ansi.Strip(strings.Join(m.editorAttachmentsDialog(70, 25), "\n"))
	if !strings.Contains(view, "File size: 7 bytes") || strings.Contains(view, path) {
		t.Fatalf("staged attachment details: %s", view)
	}
	e.attachmentName.set("renamed.txt")
	m.editorKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	edits, paths := e.attachmentEdits()
	if len(edits) != 1 || edits[0].Name != "renamed.txt" || paths[0] != path {
		t.Fatal("renaming changed the source path")
	}
}

func TestStagedAttachmentDetailsFitAppearances(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.editor = newEntryEditor(nil, "root")
		m.editor.attachments = []editorAttachment{{Name: "image.png", Path: "/tmp/image.png", OriginalIndex: -1, Size: 42}}
		m.editor.openEditorAttachments()
		m.editor.startAttachmentEdit(0)
		view := ansi.Strip(strings.Join(m.editorAttachmentsDialog(48, 16), "\n"))
		for _, label := range []string{"File name", "image.png", "File size: 42 bytes", "Save", "Delete", "Cancel"} {
			if !strings.Contains(view, label) {
				t.Errorf("%s attachment dialog missing %q", appearance, label)
			}
		}
		m.Close()
	}
}
