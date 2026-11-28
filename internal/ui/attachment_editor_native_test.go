package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAttachmentEditorNativeSaveAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.kdbx")
	backup := filepath.Join(dir, "backups")
	creds := vault.Credentials{Password: []byte("synthetic master")}
	native := vault.NewBackend()
	db, err := native.CreateWithOptions(ctx, path, creds, vault.CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	next, err := native.Apply(ctx, db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Entry"}}, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	db = next
	for _, item := range []struct{ name, content string }{{"first.txt", "first content"}, {"remove.txt", "discard"}} {
		next, err = native.Apply(ctx, db, creds, vault.Change{Kind: vault.AddAttachment, EntryUUID: db.Entries[0].UUID, AttachmentName: item.name, AttachmentData: []byte(item.content)}, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.Clear()
		db = next
	}
	db.Clear()
	source := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(source, []byte("new content"), 0600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Backend: native, InitialPath: path, Settings: platform.Settings{BackupDir: backup}})
	defer m.Close()
	m.width, m.height = 120, 35
	m.inputs[0].set(string(creds.Password))
	opened := m.startOpen()().(openedMsg)
	if opened.err != nil {
		t.Fatal(opened.err)
	}
	m.Update(opened)
	m.ws.ActiveSession().ReadOnly = false
	m.ws.ActiveView().Focus = 1
	edit, _ := m.operationKey(key("e"))
	if edit == nil {
		t.Fatal("editor preparation did not start")
	}
	ready := edit().(editorReadyMsg)
	if ready.err != nil {
		t.Fatal(ready.err)
	}
	m.Update(ready)
	if m.editor == nil {
		t.Fatal("editor did not open")
	}
	if len(m.editor.attachments) != 2 || m.editor.attachments[0].Size != int64(len("first content")) || m.editor.attachments[1].Size != int64(len("discard")) {
		t.Fatalf("attachment sizes not loaded: %+v", m.editor.attachments)
	}
	press := func(k tea.KeyMsg) { m.Update(k) }
	m.editor.focus = editAttachments
	press(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editor.attachmentsOpen {
		t.Fatal("attachments did not open")
	}
	press(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editor.attachmentEdit {
		t.Fatal("attachment editor did not open")
	}
	m.editor.attachmentName.set("renamed.txt")
	press(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.editor.attachmentIndex = 1
	press(tea.KeyMsg{Type: tea.KeyEnter})
	m.editor.attachmentFocus = 2
	press(tea.KeyMsg{Type: tea.KeyEnter})
	m.editor.attachmentIndex = len(m.editor.attachments)
	press(tea.KeyMsg{Type: tea.KeyEnter})
	m.editor.attachmentName.set(source)
	press(tea.KeyMsg{Type: tea.KeyCtrlS})
	press(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editor.attachmentName.value() != "new.txt" {
		t.Fatal("staged file edit did not show filename")
	}
	m.editor.attachmentName.set("imported.txt")
	press(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.editor.attachmentIndex = len(m.editor.attachments) + 1
	press(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editor.attachmentsOpen {
		t.Fatal("attachments dialog did not save")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("entry save did not start")
	}
	m.Update(cmd())
	if m.editor != nil {
		t.Fatalf("entry save failed: %s", m.editor.err)
	}
	m.Close()
	reopened, err := native.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Clear()
	if len(reopened.Entries[0].AttachmentNames) != 2 || reopened.Entries[0].AttachmentNames[0] != "renamed.txt" || reopened.Entries[0].AttachmentNames[1] != "imported.txt" {
		t.Fatalf("attachment names: %v", reopened.Entries[0].AttachmentNames)
	}
	for _, item := range []struct{ name, content string }{{"renamed.txt", "first content"}, {"imported.txt", "new content"}} {
		data, err := native.ExportAttachment(ctx, reopened, creds, reopened.Entries[0].UUID, item.name)
		if err != nil || string(data) != item.content {
			t.Fatalf("attachment %q: %v", item.name, err)
		}
		clear(data)
	}
}
