package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeDatabaseInfoRecycleAndDelete(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Remove"}}, backup)
	info, err := backend.InfoDatabase(context.Background(), db)
	if err != nil || info.EntryCount != 1 || info.UUID == "" || info.Cipher == "" || info.KDF == "" {
		t.Fatalf("database info incomplete: count=%d err=%v", info.EntryCount, err)
	}
	entryID := db.Entries[0].UUID
	applyFixtureChange(t, backend, &db, creds, Change{Kind: PrepareRecycleBin, EntryUUID: entryID}, backup)
	if !db.RecycleBinVerified {
		t.Fatal("recycle bin inspection did not mark the bin verified")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: DeleteEntry, EntryUUID: entryID}, backup)
	if len(db.Entries) != 1 || !db.Entries[0].Recycled || db.RecycleBinUUID == "" {
		t.Fatal("entry was not moved into the recycle bin")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: PrepareRecycleBin, EntryUUID: entryID}, backup)
	applyFixtureChange(t, backend, &db, creds, Change{Kind: DeleteEntry, EntryUUID: entryID}, backup)
	if len(db.Entries) != 0 {
		t.Fatal("recycled entry was not permanently deleted")
	}

	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Temporary"}, backup)
	var groupID string
	for _, group := range db.Groups {
		if group.Name == "Temporary" {
			groupID = group.UUID
		}
	}
	if groupID == "" {
		t.Fatal("group creation was not reflected in the database")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: PrepareDeleteGroup, GroupUUID: groupID}, backup)
	if db.GroupDeletePlan == nil || db.GroupDeletePlan.GroupUUID != groupID {
		t.Fatal("group deletion review did not produce a plan")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: DeleteGroup, GroupUUID: groupID}, backup)
	var recycledGroup bool
	for _, group := range db.Groups {
		if group.UUID == groupID {
			recycledGroup = group.Recycled && group.ParentUUID == db.RecycleBinUUID
		}
	}
	if !recycledGroup {
		t.Fatal("deleted group was not moved into the recycle bin")
	}
	opened, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	if len(opened.Entries) != 0 {
		t.Fatal("deleted data reappeared after reopening the vault")
	}
}

func TestNativeCredentialChangeBacksUpAndReopens(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	backup := filepath.Join(t.TempDir(), "backups")
	newCreds := Credentials{Password: []byte("synthetic replacement master password")}
	defer newCreds.Clear()
	updated, err := backend.ChangeCredentials(context.Background(), db, creds, newCreds, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	db = updated
	defer db.Clear()
	if _, err := backend.Open(context.Background(), path, creds); !errors.Is(err, ErrUnlock) {
		t.Fatal("old password still opens the vault")
	}
	opened, err := backend.Open(context.Background(), path, newCreds)
	if err != nil {
		t.Fatal("new password did not open the vault")
	}
	opened.Clear()
	backups, err := filepath.Glob(filepath.Join(backup, "*.kdbx"))
	if err != nil || len(backups) != 1 {
		t.Fatal("credential change backup is missing")
	}
	oldBackup, err := backend.Open(context.Background(), backups[0], creds)
	if err != nil {
		t.Fatal("old password did not open the encrypted backup")
	}
	oldBackup.Clear()
}

func TestNativeCredentialChangeBackupFailureDoesNotWrite(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	newCreds := Credentials{Password: []byte("synthetic rejected master password")}
	defer newCreds.Clear()
	if _, err := backend.ChangeCredentials(context.Background(), db, creds, newCreds, blocker); !errors.Is(err, ErrBackup) {
		t.Fatal("credential change should fail when backup creation fails")
	}
	opened, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal("backup failure changed the vault credentials")
	}
	opened.Clear()
}

func TestNativeCredentialChangeCancellationKeepsOriginalCredentials(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	defer db.Clear()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	next := Credentials{Password: []byte("synthetic canceled credential change")}
	defer next.Clear()
	ctx := &cancelAfterFirstCheck{Context: context.Background()}
	if _, err := backend.ChangeCredentials(ctx, db, creds, next, filepath.Join(t.TempDir(), "backups")); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled credential change error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("canceled credential change modified the persisted vault")
	}
	opened, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal("old credentials no longer open after cancellation")
	}
	opened.Clear()
	if _, err := backend.Open(context.Background(), path, next); !errors.Is(err, ErrUnlock) {
		t.Fatal("new credentials were installed despite cancellation")
	}
}
