package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type cancelAfterFirstCheck struct {
	context.Context
	calls int
}

func (c *cancelAfterFirstCheck) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

func TestNativeBackendProtectedValueRoundTrip(t *testing.T) {
	backend := NewBackend()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	creds := Credentials{Password: []byte("native-test-master")}
	db, err := backend.CreateWithOptions(ctx, path, creds, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	entryPassword := []byte("synthetic UI entry password")
	updated, err := backend.Apply(ctx, db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Round trip", "Tags": "test"}, PasswordChanged: true, Password: entryPassword}, filepath.Join(t.TempDir(), "backups"))
	db.Clear()
	if err != nil {
		t.Fatal(err)
	}
	updated.Clear()
	db, err = backend.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if len(db.Entries) != 1 || db.Entries[0].Get("Title") != "Round trip" {
		t.Fatalf("entries = %#v", db.Entries)
	}
	got, err := backend.ResolveField(ctx, db, Credentials{}, db.Entries[0].UUID, "Password")
	if err != nil {
		t.Fatal(err)
	}
	defer zero(got)
	if string(got) != string(entryPassword) {
		t.Fatalf("resolved protected field = %q, want test value", got)
	}
	if !containsString(db.Entries[0].Tags, "test") {
		t.Fatalf("tags = %v", db.Entries[0].Tags)
	}
	updated, err = backend.Apply(ctx, db, creds, Change{Kind: EditEntry, EntryUUID: db.Entries[0].UUID, Values: map[string]string{"Notes": "new"}}, filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	updated.Clear()
	db.Clear()
	db, err = backend.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if len(db.Entries) != 1 || db.Entries[0].HistoryCount != 1 || len(db.Entries[0].History) != 1 {
		t.Fatalf("history summary=%#v", db.Entries)
	}
}

func TestNativeApplyCancellationKeepsLiveAndPersistedDatabaseUnchanged(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	defer db.Clear()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	revision := db.Revision
	ctx := &cancelAfterFirstCheck{Context: context.Background()}
	_, err = backend.Apply(ctx, db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "must roll back"}}, filepath.Join(t.TempDir(), "backups"))
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled apply error = %v", err)
	}
	if len(db.Entries) != 0 || db.Revision != revision {
		t.Fatalf("live database changed after canceled apply: entries=%d revision changed=%t", len(db.Entries), db.Revision != revision)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("canceled apply changed the persisted database")
	}
	opened, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	if len(opened.Entries) != 0 {
		t.Fatal("canceled entry was persisted")
	}
}

func TestNativeBackendUsesUUIDsForDuplicateNames(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	defer func() { db.Clear() }()
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Same"}, backup)
	var firstGroup string
	for _, group := range db.Groups {
		if group.Name == "Same" {
			firstGroup = group.UUID
		}
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Same"}, backup)
	var groups []string
	for _, group := range db.Groups {
		if group.Name == "Same" {
			groups = append(groups, group.UUID)
		}
	}
	if len(groups) != 2 || groups[0] == groups[1] || firstGroup == "" {
		t.Fatal("duplicate group names did not retain distinct UUIDs")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddEntry, GroupUUID: groups[0], Values: map[string]string{"Title": "Duplicate"}}, backup)
	firstEntry := db.Entries[len(db.Entries)-1].UUID
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddEntry, GroupUUID: groups[1], Values: map[string]string{"Title": "Duplicate"}}, backup)
	secondEntry := db.Entries[len(db.Entries)-1].UUID
	if firstEntry == secondEntry {
		t.Fatal("duplicate entry titles did not retain distinct UUIDs")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: EditEntry, EntryUUID: firstEntry, Values: map[string]string{"Notes": "updated by UUID"}}, backup)
	for _, entry := range db.Entries {
		if entry.UUID == firstEntry && entry.Get("Notes") != "updated by UUID" {
			t.Fatal("the selected duplicate entry was not updated")
		}
		if entry.UUID == secondEntry && entry.Get("Notes") != "" {
			t.Fatal("editing one duplicate entry changed the other")
		}
	}
	opened, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	if len(opened.Entries) != 2 || opened.Entries[0].Get("Title") != "Duplicate" || opened.Entries[1].Get("Title") != "Duplicate" {
		t.Fatal("duplicate titles did not survive save and reopen")
	}
}

func TestNativeBackendAttachmentRoundTrip(t *testing.T) {
	_, creds, db, backend := nativeFixture(t)
	defer func() { db.Clear() }()
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "With attachment"}}, backup)
	entryID := db.Entries[0].UUID
	data := []byte("synthetic attachment bytes\x00payload")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddAttachment, EntryUUID: entryID, AttachmentName: "sample.bin", AttachmentData: data}, backup)
	exported, err := backend.ExportAttachment(context.Background(), db, creds, entryID, "sample.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer zero(exported)
	if !bytes.Equal(exported, data) {
		t.Fatal("attachment bytes changed during save and export")
	}
}

func TestNativeBackendMoveEntryPersistsByUUID(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Move me"}, PasswordChanged: true, Password: []byte("move-secret")}, backup)
	entryID := db.Entries[0].UUID
	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Archive"}, backup)
	var groupID string
	for _, group := range db.Groups {
		if group.Name == "Archive" {
			groupID = group.UUID
		}
	}
	if groupID == "" {
		t.Fatal("destination group missing")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: MoveEntry, EntryUUID: entryID, GroupUUID: groupID}, backup)
	var moved *Entry
	for i := range db.Entries {
		if db.Entries[i].UUID == entryID {
			moved = &db.Entries[i]
		}
	}
	if moved == nil || moved.GroupUUID != groupID || moved.GroupPath != "/Archive/" {
		t.Fatalf("moved entry=%#v", moved)
	}
	got, err := backend.ResolveField(context.Background(), db, Credentials{}, entryID, "Password")
	if err != nil {
		t.Fatal(err)
	}
	defer zero(got)
	if string(got) != "move-secret" {
		t.Fatal("moving entry changed protected password")
	}
	opened, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	if len(opened.Entries) != 1 || opened.Entries[0].GroupUUID != groupID {
		t.Fatal("entry move did not survive reopen")
	}
}

func TestNativeBackendTotpURLAndUnsupportedXMLDetection(t *testing.T) {
	backend := NewBackend()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "totp.kdbx")
	creds := Credentials{Password: []byte("totp-test-master")}
	db, err := backend.CreateWithOptions(ctx, path, creds, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := backend.Apply(ctx, db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "TOTP", "otp": "otpauth://totp/Fixture?secret=JBSWY3DPEHPK3PXP&period=30&digits=6"}}, filepath.Join(t.TempDir(), "backups"))
	db.Clear()
	if err != nil {
		t.Fatal(err)
	}
	updated.Clear()
	db, err = backend.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	result, err := backend.TOTP(ctx, db, Credentials{}, db.Entries[0].UUID)
	if err != nil {
		t.Fatal(err)
	}
	defer zero(result.Code)
	if len(result.Code) != 6 || result.PeriodSeconds != 30 || result.RemainingSeconds < 1 || result.RemainingSeconds > 30 {
		t.Fatalf("TOTP result=%+v", result)
	}
	if !hasUnsupportedXML([]byte(`<?xml version="1.0"?><KeePassFile><Root><Group><PreviousParentGroup>abc</PreviousParentGroup></Group></Root></KeePassFile>`), false) {
		t.Fatal("unmodeled KDBX 4.1 data must block writes")
	}
	if hasUnsupportedXML([]byte(`<?xml version="1.0"?><KeePassFile><Root><Group><Entry><String><Key>Title</Key><Value>x</Value></String></Entry></Group></Root></KeePassFile>`), false) {
		t.Fatal("modeled standard data was rejected")
	}
}
