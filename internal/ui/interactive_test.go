package ui

import (
	"bypass/internal/vaulttest"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInteractiveUnlockDropsCredentialsAndResolvesOnDemand(t *testing.T) {
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("keepassxc-cli required")
	}
	path := filepath.Join(t.TempDir(), "session.kdbx")
	master := []byte("synthetic UI master password")
	defer clear(master)
	native := vaulttest.NewCLI("")
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
	m := New(Options{Backend: vault.NewInteractiveCLI(""), InitialPath: path, ReadOnly: true, Settings: platform.Settings{BackupDir: t.TempDir()}})
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
	if session == nil || !session.DB.Interactive() || len(session.Credentials.Password) != 0 {
		t.Fatal("workspace retained credentials or lost interactive session")
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
		t.Fatal("reveal did not resolve through CLI")
	}
	resolved := cmd().(resolvedRevealMsg)
	defer clear(resolved.value)
	if resolved.err != nil || string(resolved.value) != "synthetic UI entry password" {
		t.Fatal("on-demand password resolution failed")
	}
	m.operationKey(key("R"))
	if session.ReadOnly {
		t.Fatal("manual read-only toggle did not enable writes")
	}
	m.operationKey(key("R"))
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
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
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
