package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type lifecycleBackend struct {
	operationBackend
	fail          bool
	changes       int
	createdPath   string
	createOptions vault.CreateOptions
}

func (b *lifecycleBackend) CreateWithOptions(_ context.Context, path string, _ vault.Credentials, opts vault.CreateOptions) (*vault.Database, error) {
	b.createdPath, b.createOptions = path, opts
	if b.fail {
		return nil, errors.New("sensitive backend output")
	}
	return fixture(path), nil
}
func (b *lifecycleBackend) ChangeCredentials(_ context.Context, db *vault.Database, _, _ vault.Credentials, _ string) (*vault.Database, error) {
	b.changes++
	if b.fail {
		return nil, errors.New("sensitive backend output")
	}
	return db.Clone(), nil
}
func (b *lifecycleBackend) Info(context.Context, string, vault.Credentials) (vault.DatabaseInfo, error) {
	return vault.DatabaseInfo{Name: "Test Vault", Cipher: "AES", KDF: "Argon2", EntryCount: 2}, nil
}
func (b *lifecycleBackend) Settings(context.Context, string, vault.Credentials) (vault.DatabaseSettings, error) {
	return vault.DatabaseSettings{Cipher: "AES", KDF: "Argon2"}, nil
}

func TestCreateVaultFormMasksInputsAndRetainsFailedDraft(t *testing.T) {
	b := &lifecycleBackend{fail: true}
	m := New(Options{Backend: b, Settings: platform.Settings{BackupDir: t.TempDir()}})
	t.Cleanup(m.Close)
	m.key(key("N"))
	if m.lifecycle == nil {
		t.Fatal("New Vault did not open")
	}
	l := m.lifecycle
	l.inputs[0].set(filepath.Join(t.TempDir(), "new.kdbx"))
	l.focus = 1
	m.key(key("qN1-secret"))
	l.inputs[2].set("qN1-secret")
	l.inputs[4].set("100")
	if strings.Contains(m.View(), "qN1-secret") {
		t.Fatal("create password rendered")
	}
	cmd := m.key(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("create was not submitted")
	}
	m.Update(cmd())
	if m.lifecycle != l || l.inputs[1].value() != "qN1-secret" || l.err == "" || strings.Contains(l.err, "sensitive") {
		t.Fatal("failed create lost its draft or exposed backend error")
	}
	b.fail = false
	cmd = m.key(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.Update(cmd())
	if m.lifecycle != nil || m.ws.ActiveSession() == nil || m.ws.ActiveSession().DB == nil || b.createOptions.DecryptionTimeMS != 100 {
		t.Fatal("create did not open the new vault")
	}
}

func TestCredentialConfirmationAndSharedSessionUpdate(t *testing.T) {
	m, _ := operationModel(t)
	b := &lifecycleBackend{}
	m.backend = b
	m.ws.Duplicate()
	s := m.ws.ActiveSession()
	oldBuffer := s.Credentials.Password
	m.openCredentials()
	l := m.lifecycle
	l.inputs[0].set("NEW-MASTER")
	l.inputs[1].set("mismatch")
	if cmd := m.submitLifecycle(); cmd != nil || l.err == "" {
		t.Fatal("password mismatch submitted")
	}
	l.inputs[1].set("NEW-MASTER")
	if cmd := m.submitLifecycle(); cmd != nil || !l.confirm || l.proceed {
		t.Fatal("confirmation did not default to Cancel")
	}
	m.lifecycleKey(key("enter"))
	if l.confirm || b.changes != 0 {
		t.Fatal("default confirmation changed credentials")
	}
	m.submitLifecycle()
	m.lifecycleKey(key("right"))
	cmd := m.lifecycleKey(key("enter"))
	if cmd == nil {
		t.Fatal("confirmed credential change did not run")
	}
	m.Update(cmd())
	if string(s.Credentials.Password) != "NEW-MASTER" || len(m.ws.Views) != 2 || m.lifecycle != nil {
		t.Fatal("shared session credentials were not updated")
	}
	for _, v := range oldBuffer {
		if v != 0 {
			t.Fatal("old credential buffer retained")
		}
	}
}

func TestLockDiscardsLateCredentialResult(t *testing.T) {
	m, _ := operationModel(t)
	m.backend = &lifecycleBackend{}
	m.openCredentials()
	m.lifecycle.inputs[0].set("NEW-MASTER")
	m.lifecycle.inputs[1].set("NEW-MASTER")
	m.submitLifecycle()
	m.lifecycle.proceed = true
	cmd := m.submitLifecycle()
	result := cmd().(lifecycleMsg)
	buffer := result.credentials.Password
	s := m.ws.ActiveSession()
	m.lockSession(s.ID)
	m.Update(result)
	if s.DB != nil || m.lifecycle != nil {
		t.Fatal("late result resurrected locked vault")
	}
	for _, v := range buffer {
		if v != 0 {
			t.Fatal("late credentials were not cleared")
		}
	}
}

func TestDatabaseInfoAndSettingsHaveKeyboardExit(t *testing.T) {
	for _, settings := range []bool{false, true} {
		m, _ := operationModel(t)
		m.backend = &lifecycleBackend{}
		cmd := m.openDatabaseInfo(settings)
		if cmd == nil {
			t.Fatal("info request not started")
		}
		m.Update(cmd())
		if !strings.Contains(m.View(), "Argon2") || m.writing {
			t.Fatal("info did not render or retained busy state")
		}
		m.lifecycleKey(key("q"))
		if m.lifecycle != nil || m.ws.ActiveSession().DB == nil {
			t.Fatal("info exit closed vault")
		}
	}
}

func TestNativeCapabilitiesGateDirectActionsAndPalette(t *testing.T) {
	m, _ := operationModel(t)
	m.backend = &lifecycleBackend{}
	m.backendCapabilities = map[string]bool{"db-create": false, "db-edit": false, "db-info": false}
	m.key(key("N"))
	if m.lifecycle != nil || m.canCreateVault() {
		t.Fatal("New Vault shortcut bypassed native capability checks")
	}
	m.openCredentials()
	m.openDatabaseInfo(false)
	if m.lifecycle != nil {
		t.Fatal("unsupported native action opened a form")
	}
	for _, command := range m.commands() {
		if command.id == "new-vault" || command.id == "credentials" || command.id == "database-info" {
			if command.reason == "" {
				t.Fatalf("unsupported action %s was enabled in palette", command.id)
			}
		}
	}
}
