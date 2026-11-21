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
