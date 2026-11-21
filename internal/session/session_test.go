package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/vault"
)

type memoryClipboard struct{ value []byte }

func (m *memoryClipboard) Write(_ context.Context, value []byte) error {
	m.value = append(m.value[:0], value...)
	return nil
}
func (m *memoryClipboard) Read(context.Context) ([]byte, error) {
	return append([]byte(nil), m.value...), nil
}

func TestHeadlessUnlockSearchManagedCopyLock(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.kdbx")
	backend := vault.NewBackend()
	master := []byte("synthetic master")
	seed, err := backend.CreateWithOptions(ctx, path, vault.Credentials{Password: master}, vault.CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	created, err := backend.Apply(ctx, seed, vault.Credentials{Password: master}, vault.Change{
		Kind: vault.AddEntry, GroupUUID: seed.RootUUID,
		Values:          map[string]string{"Title": "API workflow item", "UserName": "api-user"},
		PasswordChanged: true, Password: []byte("synthetic managed copy"),
	}, filepath.Join(dir, "backups"))
	seed.Clear()
	if err != nil {
		t.Fatal(err)
	}
	entryID := created.Entries[0].UUID
	created.Clear()

	driver := &memoryClipboard{}
	manager := clipboard.New(driver)
	core := New(backend, manager, time.Minute)
	password := []byte("synthetic master")
	if err := core.Open(ctx, path, vault.Credentials{Password: password}); err != nil {
		t.Fatal(err)
	}
	for _, b := range password {
		if b != 0 {
			t.Fatal("Open retained caller credentials")
		}
	}
	ids, err := core.Search("API workflow")
	if err != nil || len(ids) != 1 || ids[0] != entryID {
		t.Fatalf("search: %v, %v", ids, err)
	}
	if err := core.CopyValue(ctx, entryID, "Password", time.Now()); err != nil {
		t.Fatal(err)
	}
	if string(driver.value) != "synthetic managed copy" {
		t.Fatal("managed copy wrote the wrong value")
	}
	if err := core.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if len(driver.value) != 0 {
		t.Fatal("lock did not clear the managed clipboard")
	}
	if _, err := core.Search("API workflow"); err != ErrLocked {
		t.Fatalf("search after lock: %v", err)
	}
}

func TestNativeApplyOwnershipTransferAndDiscard(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "ownership.kdbx")
	backend := vault.NewBackend()
	master := []byte("synthetic ownership master")
	seed, err := backend.CreateWithOptions(ctx, path, vault.Credentials{Password: master}, vault.CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	created, err := backend.Apply(ctx, seed, vault.Credentials{Password: master}, vault.Change{
		Kind: vault.AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "Ownership test"},
		PasswordChanged: true, Password: []byte("kept secret"),
	}, filepath.Join(dir, "backups"))
	seed.Clear()
	if err != nil {
		t.Fatal(err)
	}
	created.Clear()

	core := New(backend, nil, time.Minute)
	if err := core.Open(ctx, path, vault.Credentials{Password: append([]byte(nil), master...)}); err != nil {
		t.Fatal(err)
	}
	live := core.Database()
	entryID := live.Entries[0].UUID

	// A rejected result generated from a borrowed snapshot must not own or wipe
	// the live backend when discarded.
	snapshot := core.Snapshot()
	discarded, err := core.Apply(ctx, snapshot, vault.Change{Kind: vault.EditEntry, EntryUUID: entryID, Values: map[string]string{"Title": "discarded"}}, filepath.Join(dir, "backups"))
	snapshot.Clear()
	if err != nil {
		t.Fatal(err)
	}
	core.DiscardDatabase(discarded)
	if !live.Unlocked() || core.Database() != live {
		t.Fatal("discarded result cleared the live database")
	}
	refresh := core.Snapshot()
	reloaded, err := core.Reload(ctx, refresh)
	refresh.Clear()
	if err != nil {
		t.Fatal(err)
	}
	core.ReplaceDatabase(reloaded)

	// An accepted result must take ownership before the old model is cleared.
	snapshot = core.Snapshot()
	accepted, err := core.Apply(ctx, snapshot, vault.Change{Kind: vault.EditEntry, EntryUUID: entryID, Values: map[string]string{"Title": "accepted"}}, filepath.Join(dir, "backups"))
	snapshot.Clear()
	if err != nil {
		t.Fatal(err)
	}
	core.ReplaceDatabase(accepted)
	if core.Database() != accepted || !accepted.Unlocked() || len(live.Entries) != 0 {
		t.Fatal("accepted result did not take ownership cleanly")
	}
	next := core.Snapshot()
	fields, err := core.EntryDetails(ctx, next, entryID)
	if err != nil || len(fields) == 0 {
		t.Fatalf("accepted database is unusable: fields=%v err=%v", fields, err)
	}
	next.Clear()

	// Credential rotation also returns an owned result which the session must
	// accept before the old model is cleared.
	rotationSnapshot := core.Snapshot()
	newPassword := []byte("synthetic rotated master")
	rotated, err := core.ChangeCredentials(ctx, rotationSnapshot, vault.Credentials{Password: newPassword}, filepath.Join(dir, "backups"))
	rotationSnapshot.Clear()
	if err != nil {
		t.Fatal(err)
	}
	core.ReplaceDatabase(rotated)
	if core.Database() != rotated || !rotated.Unlocked() {
		t.Fatal("credential rotation result did not replace the live database")
	}
	verifier := New(backend, nil, time.Minute)
	if err := verifier.Open(ctx, path, vault.Credentials{Password: append([]byte(nil), newPassword...)}); err != nil {
		t.Fatalf("rotated credentials could not reopen the vault: %v", err)
	}
	verifier.Adopt(nil)
}

func TestReplaceNilClearsFallbackCredentials(t *testing.T) {
	core := New(nil, nil, 0)
	core.SetCredentials(vault.Credentials{Password: []byte("fallback password"), KeyFile: "/tmp/key"})
	core.ReplaceDatabase(nil)
	if len(core.creds.Password) != 0 || core.creds.KeyFile != "" {
		t.Fatal("nil replacement retained fallback credentials")
	}
}
