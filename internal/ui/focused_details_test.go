package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type delayedDetailsBackend struct {
	*vault.NativeBackend
	slowEntry        string
	started, release chan struct{}
}

type retryDetailsBackend struct {
	calls    int
	resolves int
}

func (*retryDetailsBackend) Open(_ context.Context, path string, _ vault.Credentials) (*vault.Database, error) {
	return fixture(path), nil
}

func (b *retryDetailsBackend) EntryDetails(_ context.Context, _ *vault.Database, id string) ([]vault.Field, error) {
	b.calls++
	return []vault.Field{{Name: "Notes", Value: "fresh details for " + id}}, nil
}

func (b *retryDetailsBackend) ResolveField(_ context.Context, _ *vault.Database, _ vault.Credentials, id, _ string) ([]byte, error) {
	b.resolves++
	return []byte("fresh secret for " + id), nil
}

func (b *delayedDetailsBackend) EntryDetails(ctx context.Context, db *vault.Database, id string) ([]vault.Field, error) {
	if id == b.slowEntry {
		close(b.started)
		<-b.release
		// Model a response already produced before cancellation was observed.
		return []vault.Field{{Name: "Notes", Value: "late first notes"}}, nil
	}
	return b.NativeBackend.EntryDetails(ctx, db, id)
}

func TestVaultRuntimeFocusedDetailsRejectLateSelectionAndLockResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "focus.kdbx")
	credentials := vault.Credentials{Password: []byte("synthetic focused details master")}
	defer credentials.Clear()
	native := vault.NewBackend()
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
	backend := &delayedDetailsBackend{NativeBackend: vault.NewBackend(), started: make(chan struct{}), release: make(chan struct{})}
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
	for _, entry := range session.Database().Entries {
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
	if session.Database() != nil {
		t.Fatal("late details resurrected locked data")
	}
}

func TestFocusedDetailsRetriesOnceAfterContentReplacement(t *testing.T) {
	backend := &retryDetailsBackend{}
	m := New(Options{Backend: backend})
	t.Cleanup(m.Close)
	m.width, m.height = 120, 35
	ctx := context.Background()
	native := vault.NewBackend()
	creds := vault.Credentials{Password: []byte("synthetic details fixture master")}
	defer creds.Clear()
	dir := t.TempDir()
	seed, err := native.CreateWithOptions(ctx, filepath.Join(dir, "focus.kdbx"), creds, vault.CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Clear()
	db, err := native.Apply(ctx, seed, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "Fixture"}}, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	entryID := db.Entries[0].UUID
	m.ws.Open(db)
	m.screen = browse
	m.normalize()
	read := m.loadFocusedDetails()
	if read == nil {
		t.Fatal("focused details read did not start")
	}
	core := m.ws.ActiveSession().Core
	core.ReplaceDatabase(core.Database().Clone())
	changed := read().(focusedDetailsMsg)
	if changed.err == nil {
		t.Fatal("snapshot read did not reject the replaced content")
	}
	_, retry := m.Update(changed)
	if retry == nil {
		t.Fatal("changed snapshot did not schedule one fresh read")
	}
	m.Update(retry())
	if backend.calls != 1 || m.selected() == nil || m.selected().Get("Notes") != "fresh details for "+entryID {
		t.Fatal("retry did not apply details from the current database")
	}
}

func TestRevealRetriesOnceAfterContentReplacement(t *testing.T) {
	backend := &retryDetailsBackend{}
	m := New(Options{Backend: backend})
	t.Cleanup(m.Close)
	m.width, m.height = 120, 35
	m.ws.Open(fixture("/tmp/reveal-content-retry.kdbx"))
	m.screen = browse
	m.normalize()
	current := m.ws.ActiveSession()
	m.revealEntry, m.revealField, m.revealSession = "a", "Password", current.ID
	read := m.requestRevealField()
	current.Core.ReplaceDatabase(current.Database().Clone())
	changed := read().(resolvedRevealMsg)
	if changed.err == nil {
		t.Fatal("reveal snapshot did not reject replaced content")
	}
	_, retry := m.Update(changed)
	if retry == nil {
		t.Fatal("changed reveal did not schedule one fresh read")
	}
	m.Update(retry())
	if backend.resolves != 1 || string(m.revealValue) != "fresh secret for a" {
		t.Fatal("reveal retry did not resolve against current content")
	}
}
