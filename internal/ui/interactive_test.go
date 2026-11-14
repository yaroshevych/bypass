package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func TestVaultRuntimeUnlockDropsCredentialsAndResolvesOnDemand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.kdbx")
	master := []byte("synthetic UI master password")
	defer clear(master)
	native := vault.NewBackend()
	seed, err := native.CreateWithOptions(context.Background(), path, vault.Credentials{Password: master}, vault.CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Clear()
	populated, err := native.Apply(context.Background(), seed, vault.Credentials{Password: master}, vault.Change{Kind: vault.AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "Interactive Entry", "UserName": "alice.public", "URL": "https://visible.test", "Notes": "visible notes\nsecond line"}, PasswordChanged: true, Password: []byte("synthetic UI entry password")}, filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	defer populated.Clear()
	m := New(Options{Backend: native, InitialPath: path, ReadOnly: true, Settings: platform.Settings{BackupDir: t.TempDir()}})
	defer m.Close()
	m.width, m.height = 120, 35
	m.inputs[0].set(string(master))
	cmd := m.startOpen()
	if cmd == nil {
		t.Fatal("unlock did not start")
	}
	msg := cmd().(openedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(msg.credentials.Password) != 0 {
		t.Fatal("unlock result retained master password")
	}
	_, detailCmd := m.Update(msg)
	session := m.ws.ActiveSession()
	if session == nil || !session.DB.Unlocked() || len(session.Credentials.Password) != 0 {
		t.Fatal("workspace retained credentials or lost unlocked vault state")
	}
	if detailCmd == nil {
		t.Fatal("focused item did not request details")
	}
	if !strings.Contains(m.View(), "alice.public") || !strings.Contains(m.View(), "https://visible.test") {
		t.Fatal("non-password fields are hidden")
	}
	view := m.ws.ActiveView()
	for _, query := range []string{"alice.public", "visible.test"} {
		view.Query = query
		m.searchCache = nil
		if len(m.entries()) != 1 {
			t.Fatal("list field filter skipped visible data")
		}
	}
	view.Query = ""
	m.searchCache = nil
	readyDetails := detailCmd().(focusedDetailsMsg)
	m.Update(readyDetails)
	if session.DB.Entries[0].Get("Notes") != "visible notes\nsecond line" || !session.DB.Entries[0].DetailsLoaded {
		t.Fatal("focused notes did not load")
	}
	// Defensive SetCredentials must never reintroduce a password for this backend.
	m.ws.SetCredentials(session.ID, vault.Credentials{Password: master})
	if len(session.Credentials.Password) != 0 {
		t.Fatal("workspace accepted retained master password")
	}
	if strings.Contains(m.View(), "synthetic UI entry password") {
		t.Fatal("password rendered before reveal")
	}
	cmd = m.toggleReveal()
	if cmd == nil {
		t.Fatal("reveal did not resolve through the vault runtime")
	}
	resolved := cmd().(resolvedRevealMsg)
	defer clear(resolved.value)
	if resolved.err != nil {
		t.Fatalf("on-demand password resolution returned an error: %v", resolved.err)
	}
	if string(resolved.value) != "synthetic UI entry password" {
		t.Fatal("on-demand password resolution failed")
	}
	m.operationKey(key("r"))
	if !session.ReadOnly {
		t.Fatal("startup read-only restriction was bypassed")
	}
	if m.requestReload(session.ID) != nil || !strings.Contains(m.notice, "lock and unlock") {
		t.Fatal("reload did not explain authentication requirement")
	}
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	m.protectScreen()
	if !m.gate.Protected() || strings.Contains(m.View(), "Interactive Entry") {
		t.Fatal("PIN cover exposed interactive vault")
	}
	m.Update(key("2468"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.gate.Protected() {
		t.Fatal("PIN did not unlock interface")
	}
	cmd = m.toggleReveal()
	if cmd == nil {
		t.Fatal("PIN protection lost authenticated process")
	}
	resumed := cmd().(resolvedRevealMsg)
	defer clear(resumed.value)
	if resumed.err != nil || string(resumed.value) != "synthetic UI entry password" {
		t.Fatal("authenticated process did not survive PIN protection")
	}
	session.ReadOnly = false
	pendingEdit, _ := m.operationKey(key("e"))
	if pendingEdit == nil {
		t.Fatal("interactive editor preparation did not start")
	}
	ready := pendingEdit().(editorReadyMsg)
	if ready.err != nil {
		t.Fatal(ready.err)
	}
	m.Update(key("L"))
	m.Update(ready)
	if m.editor != nil {
		t.Fatal("late editor result reopened secrets after full lock")
	}
	for _, b := range ready.password {
		if b != 0 {
			t.Fatal("late editor password was not cleared")
		}
	}
	if session.DB != nil || len(session.Credentials.Password) != 0 {
		t.Fatal("full lock retained session")
	}
}

func TestCustomFieldInteractiveSaveAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "custom-fields.kdbx")
	creds := vault.Credentials{Password: []byte("synthetic master")}
	native := vault.NewBackend()
	seed, err := native.CreateWithOptions(ctx, path, creds, vault.CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	populated, err := native.Apply(ctx, seed, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "Entry"}, CustomFields: []vault.Field{{Name: "Existing", Value: "before"}}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seed.Clear()
	populated.Clear()
	m := New(Options{Backend: native, InitialPath: path, Settings: platform.Settings{BackupDir: t.TempDir()}})
	defer m.Close()
	m.width, m.height = 120, 35
	m.inputs[0].set(string(creds.Password))
	open := m.startOpen()
	if open == nil {
		t.Fatal("open did not start")
	}
	opened := open().(openedMsg)
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
	m.editor.focus = editCustomFields
	press := func(k tea.KeyMsg) { m.Update(k) }
	press(tea.KeyMsg{Type: tea.KeyEnter})
	press(tea.KeyMsg{Type: tea.KeyEnter})
	press(tea.KeyMsg{Type: tea.KeyTab})
	press(tea.KeyMsg{Type: tea.KeyCtrlU})
	press(key("first line"))
	press(tea.KeyMsg{Type: tea.KeyEnter})
	press(key("second line"))
	press(tea.KeyMsg{Type: tea.KeyTab})
	press(tea.KeyMsg{Type: tea.KeySpace})
	press(tea.KeyMsg{Type: tea.KeyTab})
	press(tea.KeyMsg{Type: tea.KeyEnter})
	press(tea.KeyMsg{Type: tea.KeyDown})
	press(tea.KeyMsg{Type: tea.KeyDown})
	press(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editor.customOpen {
		t.Fatal("custom fields dialog did not save")
	}
	_, save := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if save == nil {
		t.Fatal("entry save did not start")
	}
	m.Update(save())
	if m.editor != nil {
		t.Fatal("entry save did not complete")
	}
	m.Close()
	reopened, err := native.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Clear()
	fields, err := native.EntryDetails(ctx, reopened, reopened.Entries[0].UUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Name == "Existing" {
			if field.Value != "first line\nsecond line" || !field.Protected {
				t.Fatalf("persisted field = %+v", field)
			}
			return
		}
	}
	t.Fatal("edited field missing after reopening")
}
