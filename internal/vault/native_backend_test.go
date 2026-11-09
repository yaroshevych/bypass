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

func TestUnsupportedMetadataConsentBacksUpBeforeLossyWrite(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	defer db.Clear()
	fields := unsupportedXMLFields([]byte(`<KeePassFile><Root><Group><PreviousParentGroup>old-parent</PreviousParentGroup></Group></Root></KeePassFile>`), false)
	if len(fields) != 1 || fields[0] != "PreviousParentGroup (previous parent group metadata)" {
		t.Fatalf("unsupported metadata summary = %v", fields)
	}
	// Bind this detected loss to the disposable vault state.
	db.native.lossFields, db.native.writeBlocked = fields, true
	change := Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Consent"}}
	backups := filepath.Join(t.TempDir(), "backups")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Apply(context.Background(), db, creds, change, backups)
	var loss *DataLossError
	if !errors.As(err, &loss) {
		t.Fatal("lossy write proceeded without consent")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("unconfirmed write changed the vault")
	}
	if entries, _ := os.ReadDir(backups); len(entries) != 0 {
		t.Fatal("unconfirmed write created a backup")
	}

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	change.AllowDataLoss = true
	change.DataLossConsent = loss.Operation
	differentOperation := change
	differentOperation.Values = map[string]string{"Title": "Different mutation"}
	if _, err = backend.Apply(context.Background(), db, creds, differentOperation, backups); !errors.As(err, &loss) {
		t.Fatal("consent was reused for a different operation")
	}
	if _, err = backend.Apply(context.Background(), db, creds, change, blocker); !errors.Is(err, ErrBackup) {
		t.Fatalf("backup failure = %v", err)
	}
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("backup failure changed the vault")
	}

	updated, err := backend.Apply(context.Background(), db, creds, change, backups)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	defer updated.Clear()
	if updated.native.writeBlocked || len(updated.native.lossFields) != 0 {
		t.Fatal("accepted save left stale metadata consent on the live database")
	}
	backupsFound, err := filepath.Glob(filepath.Join(backups, "*.kdbx"))
	if err != nil || len(backupsFound) != 1 {
		t.Fatalf("encrypted backup missing: %v %v", backupsFound, err)
	}
	backup, err := backend.Open(context.Background(), backupsFound[0], creds)
	if err != nil {
		t.Fatal("backup cannot be opened with original credentials")
	}
	defer backup.Clear()
	backupBytes, err := os.ReadFile(backupsFound[0])
	if err != nil || !bytes.Equal(before, backupBytes) {
		t.Fatal("encrypted backup did not preserve the exact original vault")
	}
}

func TestUnsupportedXMLFieldsAreContextAware(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		want string
	}{
		{
			name: "group tags",
			xml:  `<KeePassFile><Root><Group><Tags>plugin-tag</Tags></Group></Root></KeePassFile>`,
			want: "unsupported XML path: KeePassFile/Root/Group/Tags",
		},
		{
			name: "group custom data",
			xml:  `<KeePassFile><Root><Group><CustomData><Item><Key>plugin</Key><Value>value</Value></Item></CustomData></Group></Root></KeePassFile>`,
			want: "unsupported XML path: KeePassFile/Root/Group/CustomData",
		},
		{
			name: "custom icon name",
			xml:  `<KeePassFile><Meta><CustomIcons><Icon><UUID>icon-id</UUID><Name>icon-name</Name><Data>AA==</Data></Icon></CustomIcons></Meta><Root/></KeePassFile>`,
			want: "unsupported XML path: KeePassFile/Meta/CustomIcons/Icon/Name",
		},
		{
			name: "custom icon modification time",
			xml:  `<KeePassFile><Meta><CustomIcons><Icon><UUID>icon-id</UUID><LastModificationTime>2020-01-01T00:00:00Z</LastModificationTime><Data>AA==</Data></Icon></CustomIcons></Meta><Root/></KeePassFile>`,
			want: "unsupported XML path: KeePassFile/Meta/CustomIcons/Icon/LastModificationTime",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := unsupportedXMLFields([]byte(test.xml), false)
			if !containsString(fields, test.want) {
				t.Fatalf("unsupported fields = %v, want %q", fields, test.want)
			}
		})
	}

	valid := []byte(`<KeePassFile><Meta><CustomData><Item><Key>plugin</Key><Value>value</Value></Item></CustomData></Meta><Root><Group><Entry><Tags>entry-tag</Tags><Binary><Key>document.pdf</Key><Value Ref="0"/></Binary><History><Entry><Binary><Key>old.pdf</Key><Value Ref="1"/></Binary></Entry></History></Entry></Group></Root></KeePassFile>`)
	if fields := unsupportedXMLFields(valid, false); len(fields) != 0 {
		t.Fatalf("supported fields reported as unsupported: %v", fields)
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

func TestNativeAttachmentMutationRejectsAmbiguousNameAndSelectsOneReference(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	defer db.Clear()
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Duplicate attachments"}}, backup)
	_, entry := findNativeEntry(db.native.db, db.Entries[0].UUID)
	first := db.native.db.AddBinary([]byte("first payload"))
	second := db.native.db.AddBinary([]byte("second payload"))
	entry.Binaries = append(entry.Binaries, first.CreateReference("same.bin"), second.CreateReference("same.bin"))
	db.Entries[0].AttachmentNames = []string{"same.bin", "same.bin"}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Apply(context.Background(), db, creds, Change{Kind: RemoveAttachment, EntryUUID: db.Entries[0].UUID, AttachmentName: "same.bin"}, backup)
	if !errors.Is(err, ErrAmbiguousAttachment) {
		t.Fatalf("ambiguous remove error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("ambiguous attachment rejection changed vault bytes")
	}
	if len(entry.Binaries) != 2 {
		t.Fatalf("ambiguous remove changed references: %d", len(entry.Binaries))
	}
	index := 1
	updated, err := backend.Apply(context.Background(), db, creds, Change{Kind: RemoveAttachment, EntryUUID: db.Entries[0].UUID, AttachmentName: "same.bin", AttachmentIndex: &index}, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	defer updated.Clear()
	_, entry = findNativeEntry(updated.native.db, updated.Entries[0].UUID)
	if len(entry.Binaries) != 1 || entry.Binaries[0].Value.ID != first.ID {
		t.Fatalf("selected removal left references = %#v", entry.Binaries)
	}
}

func TestNativeApplyPreservesExistingVaultPermissionsAndBackupBytes(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	defer db.Clear()
	if err := os.Chmod(path, 0754); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backups")
	updated, err := backend.Apply(context.Background(), db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Permission check"}}, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	defer updated.Clear()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0754 {
		t.Fatalf("replacement mode = %v, err %v", info.Mode().Perm(), err)
	}
	backups, err := filepath.Glob(filepath.Join(backupDir, "*.kdbx"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, err %v", backups, err)
	}
	backupBytes, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(before, backupBytes) {
		t.Fatal("backup did not preserve original encrypted bytes")
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
