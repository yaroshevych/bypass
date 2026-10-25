package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInteractiveNativeDatabaseInfoRecycleAndRemove(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	backup := filepath.Join(t.TempDir(), "backups")
	apply := func(change Change) {
		t.Helper()
		updated, err := backend.Apply(context.Background(), db, Credentials{}, change, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(updated)
		db.Clear()
		db = updated
	}
	info, err := backend.InfoDatabase(context.Background(), db)
	if err != nil || info.UUID == "" || info.Cipher == "" || info.KDF == "" || info.EntryCount != 0 {
		t.Fatal("native database info missing", err)
	}
	apply(Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Delete me"}})
	id := db.Entries[0].UUID
	info, err = backend.InfoDatabase(context.Background(), db)
	if err != nil || info.EntryCount != 1 {
		t.Fatal("database info count did not refresh", err)
	}
	apply(Change{Kind: DeleteEntry, EntryUUID: id})
	if len(db.Entries) != 1 || !db.Entries[0].Recycled || db.RecycleBinUUID == "" {
		t.Fatal("native recycled entry not reflected in UI model")
	}
	opened, err := NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Entries) != 1 || !opened.Entries[0].Recycled {
		t.Fatal("native rm did not recycle entry")
	}
	opened.Clear()
	apply(Change{Kind: DeleteEntry, EntryUUID: id})
	opened, err = NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Entries) != 0 {
		t.Fatal("native rm did not permanently remove recycled entry")
	}
	opened.Clear()
	apply(Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Delete group"})
	var groupID string
	for _, group := range db.Groups {
		if group.Name == "Delete group" {
			groupID = group.UUID
		}
	}
	apply(Change{Kind: DeleteGroup, GroupUUID: groupID})
	opened, err = NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	found := false
	for _, group := range opened.Groups {
		if group.Name == "Delete group" && group.Recycled {
			found = true
		}
	}
	if !found {
		t.Fatal("native rmdir did not recycle group")
	}
}

func TestInteractiveNativePasswordChangeAndOldPasswordBackup(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	session := db.interactive
	backup := filepath.Join(t.TempDir(), "backups")
	newCreds := Credentials{Password: []byte("new synthetic master password")}
	defer newCreds.Clear()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, err := backend.ChangeCredentials(ctx, db, Credentials{}, newCreds, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.TransferInteractive(updated)
	db.Clear()
	db = updated
	if db.interactive != session {
		t.Fatal("password change replaced authenticated process")
	}
	if _, err := NewCLI("").Open(context.Background(), path, creds); !errors.Is(err, ErrUnlock) {
		t.Fatal("old password still accepted", err)
	}
	opened, err := NewCLI("").Open(context.Background(), path, newCreds)
	if err != nil {
		t.Fatal("new password cannot reopen vault", err)
	}
	opened.Clear()
	backups, err := filepath.Glob(filepath.Join(backup, "*.kdbx"))
	if err != nil || len(backups) != 1 {
		t.Fatal("password-change backup missing")
	}
	old, err := NewCLI("").Open(context.Background(), backups[0], creds)
	if err != nil {
		t.Fatal("old credentials cannot open backup", err)
	}
	old.Clear()
	// A following operation uses only the authenticated session, not either password.
	next, err := backend.Apply(context.Background(), db, Credentials{}, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "After password change"}}, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.TransferInteractive(next)
	db.Clear()
	db = next
	opened, err = NewCLI("").Open(context.Background(), path, newCreds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	if len(opened.Entries) != 1 {
		t.Fatal("authenticated session unusable after password change")
	}
	for _, arg := range session.cmd.Args {
		if arg == string(newCreds.Password) {
			t.Fatal("new password in process arguments")
		}
	}
}

func TestInteractivePasswordChangeBackupFailureDoesNotWrite(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	newCreds := Credentials{Password: []byte("must not be applied")}
	defer newCreds.Clear()
	_, err = backend.ChangeCredentials(context.Background(), db, Credentials{}, newCreds, blocker)
	if !errors.Is(err, ErrBackup) {
		t.Fatal("expected backup failure", err)
	}
	opened, err := NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal("backup failure changed password", err)
	}
	opened.Clear()
}

func TestInteractiveNativeLoadsExistingRecycleBin(t *testing.T) {
	path, creds, seed := interactiveFixture(t)
	native := NewCLI("")
	backup := filepath.Join(t.TempDir(), "fixture-backups")
	populated, err := native.Apply(context.Background(), seed, creds, Change{Kind: AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "LinkedIn"}}, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer populated.Clear()
	recycled, err := native.Apply(context.Background(), populated, creds, Change{Kind: DeleteEntry, EntryUUID: populated.Entries[0].UUID}, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer recycled.Clear()
	db, err := NewInteractiveCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if db.RecycleBinUUID == "" || len(db.Entries) != 1 || !db.Entries[0].Recycled {
		t.Fatalf("existing recycled entry was treated as active: enabled=%v bin=%q groups=%+v", db.RecycleBinEnabled, db.RecycleBinUUID, db.Groups)
	}
	for _, group := range db.Groups {
		if group.UUID == db.RecycleBinUUID && !group.Recycled {
			t.Fatal("bin group was treated as active")
		}
	}
}
