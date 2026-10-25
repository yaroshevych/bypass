package ui

import (
	"bypass/internal/vaulttest"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type delayedDetailsBackend struct {
	*vault.InteractiveCLI
	slowEntry        string
	started, release chan struct{}
}

func (b *delayedDetailsBackend) EntryDetails(ctx context.Context, db *vault.Database, id string) ([]vault.Field, error) {
	if id == b.slowEntry {
		close(b.started)
		<-b.release
		// Model a response already produced before cancellation was observed.
		return []vault.Field{{Name: "Notes", Value: "late first notes"}}, nil
	}
	return b.InteractiveCLI.EntryDetails(ctx, db, id)
}

func TestInteractiveFocusedDetailsRejectLateSelectionAndLockResults(t *testing.T) {
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("keepassxc-cli required")
	}
	path := filepath.Join(t.TempDir(), "focus.kdbx")
	credentials := vault.Credentials{Password: []byte("synthetic focused details master")}
	defer credentials.Clear()
	native := vaulttest.NewCLI("")
	seed, err := native.CreateWithOptions(context.Background(), path, credentials, vault.CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { seed.Clear() }()
	for _, title := range []string{"First", "Second"} {
		updated, err := native.Apply(context.Background(), seed, credentials, vault.Change{Kind: vault.AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": title, "UserName": title + " user", "Notes": title + " notes"}}, filepath.Join(t.TempDir(), "backups"))
		if err != nil {
			t.Fatal(err)
		}
		seed.Clear()
		seed = updated
	}
	backend := &delayedDetailsBackend{InteractiveCLI: vault.NewInteractiveCLI(""), started: make(chan struct{}), release: make(chan struct{})}
	for _, entry := range seed.Entries {
		if entry.Get("Title") == "First" {
			backend.slowEntry = entry.UUID
		}
	}
	defer close(backend.release)
	m := New(Options{Backend: backend, InitialPath: path, Settings: platform.Settings{BackupDir: t.TempDir()}})
	defer m.Close()
	m.width, m.height = 120, 35
	m.inputs[0].set(string(credentials.Password))
	opened := m.startOpen()()
	_, firstRead := m.Update(opened)
	if firstRead == nil {
		t.Fatal("first selection did not load details")
	}
	firstResult := make(chan tea.Msg, 1)
	go func() { firstResult <- firstRead() }()
	select {
	case <-backend.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first read did not start")
	}
	session := m.ws.ActiveSession()
	var second string
	for _, entry := range session.DB.Entries {
		if entry.Get("Title") == "Second" {
			second = entry.UUID
		}
	}
	m.ws.ActiveView().SelectedUUID = second
	_, secondRead := m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	if secondRead == nil {
		t.Fatal("changed selection did not load details")
	}
	secondResult := secondRead()
	m.Update(secondResult)
	if selected := m.selected(); selected == nil || selected.Get("Notes") != "Second notes" {
		t.Fatal("new selection did not load its own notes")
	}
	// A delayed response from the old selection must not replace the new one.
	backend.release <- struct{}{}
	var firstMessage tea.Msg
	select {
	case firstMessage = <-firstResult:
		m.Update(firstMessage)
	case <-time.After(3 * time.Second):
		t.Fatal("delayed read did not finish")
	}
	if selected := m.selected(); selected == nil || selected.UUID != second || selected.Get("Notes") != "Second notes" {
		t.Fatal("late response replaced the focused item's details")
	}
	m.lockCurrent(true)
	m.Update(firstMessage)
	m.Update(secondResult)
	if session.DB != nil {
		t.Fatal("late details resurrected locked data")
	}
}
