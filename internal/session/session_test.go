package session

import (
	"context"
	"errors"
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

type openOnlyBackend struct {
	native *vault.NativeBackend
	opens  int
}

func (b *openOnlyBackend) Open(ctx context.Context, path string, credentials vault.Credentials) (*vault.Database, error) {
	b.opens++
	return b.native.Open(ctx, path, credentials)
}

func TestNilCapabilityChecksAreSafe(t *testing.T) {
	var core *Session
	if core.CanEntryDetails() || core.CanResolveField() || core.CanTOTP() || core.CanExportAttachment() {
		t.Fatal("nil session reported a supported capability")
	}
	if New(nil, nil, 0).CanEntryDetails() || New(nil, nil, 0).CanResolveField() || New(nil, nil, 0).CanTOTP() || New(nil, nil, 0).CanExportAttachment() {
		t.Fatal("session with nil backend reported a supported capability")
	}
	var service *Service
	if service.CanOpen() {
		t.Fatal("nil service reported that it can open vaults")
	}
}

func TestReloadUnlockedBackendWithoutReloadIsUnsupported(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "open-only.kdbx")
	native := vault.NewBackend()
	credentials := vault.Credentials{Password: []byte("synthetic open-only master")}
	defer credentials.Clear()
	seed, err := native.CreateWithOptions(ctx, path, credentials, vault.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seed.Clear()

	backend := &openOnlyBackend{native: native}
	core := New(backend, nil, time.Minute)
	if err := core.Open(ctx, path, credentials.Clone()); err != nil {
		t.Fatal(err)
	}
	defer core.Lock(ctx)
	snapshot := core.Snapshot()
	defer snapshot.Clear()
	if !snapshot.db.Unlocked() || len(snapshot.credentials.Password) != 0 {
		t.Fatal("test did not create an unlocked snapshot without retained credentials")
	}
	if _, err := core.Reload(ctx, snapshot); err != ErrUnsupported {
		t.Fatalf("Reload error = %v, want ErrUnsupported", err)
	}
	if backend.opens != 1 {
		t.Fatalf("Reload attempted Open with empty credentials; Open calls = %d", backend.opens)
	}
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

	readSnapshot := core.ReadSnapshot()
	if readSnapshot.complete || len(readSnapshot.db.Entries) != 0 || len(readSnapshot.db.Groups) != 0 {
		t.Fatal("native read snapshot retained the presentation model")
	}
	fields, err = core.EntryDetails(ctx, readSnapshot, entryID)
	if err != nil || len(fields) == 0 {
		t.Fatalf("native read snapshot could not resolve details: fields=%v err=%v", fields, err)
	}
	if _, err := core.Apply(ctx, readSnapshot, vault.Change{Kind: vault.EditEntry, EntryUUID: entryID}, filepath.Join(dir, "backups")); !errors.Is(err, ErrFullSnapshotRequired) {
		t.Fatalf("Apply accepted a lean read snapshot: %v", err)
	}
	readSnapshot.Clear()
}

func TestReplaceNilClearsFallbackCredentials(t *testing.T) {
	core := New(nil, nil, 0)
	core.SetCredentials(vault.Credentials{Password: []byte("fallback password"), KeyFile: "/tmp/key"})
	core.ReplaceDatabase(nil)
	if len(core.creds.Password) != 0 || core.creds.KeyFile != "" {
		t.Fatal("nil replacement retained fallback credentials")
	}
}

func TestCredentialBytesAreWipedOnLockAndAdoptNil(t *testing.T) {
	tests := []struct {
		name   string
		retire func(*Session)
	}{
		{name: "Lock", retire: func(core *Session) { _ = core.Lock(context.Background()) }},
		{name: "Adopt nil", retire: func(core *Session) { core.Adopt(nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := New(nil, nil, 0)
			core.Adopt(&vault.Database{Path: "fixture.kdbx"})
			core.SetCredentials(vault.Credentials{Password: []byte("synthetic stored credential")})
			stored := core.creds.Password
			if len(stored) == 0 {
				t.Fatal("test did not capture stored credential bytes")
			}

			tt.retire(core)

			for i, b := range stored {
				if b != 0 {
					t.Fatalf("stored credential byte %d was not wiped", i)
				}
			}
		})
	}
}
