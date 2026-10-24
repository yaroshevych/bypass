package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDatabaseAndCredentialsCloneAreIndependent(t *testing.T) {
	db := &Database{Groups: []Group{{Name: "group"}}, Entries: []Entry{{Fields: []Field{{Name: "Password", Value: "secret"}}, Tags: []string{"tag"}, AttachmentNames: []string{"file"}}}}
	clone := db.Clone()
	clone.Groups[0].Name = "changed"
	clone.Entries[0].Fields[0].Value = "changed"
	clone.Entries[0].Tags[0] = "changed"
	clone.Entries[0].AttachmentNames[0] = "changed"
	if db.Groups[0].Name != "group" || db.Entries[0].Fields[0].Value != "secret" || db.Entries[0].Tags[0] != "tag" || db.Entries[0].AttachmentNames[0] != "file" {
		t.Fatal("database clone shares mutable slices")
	}
	creds := Credentials{Password: []byte("secret"), KeyFile: "key", YubiKey: "slot"}
	copy := creds.Clone()
	copy.Password[0] = 'x'
	copy.Clear()
	if string(creds.Password) != "secret" || len(copy.Password) != 0 || copy.KeyFile != "" || copy.YubiKey != "" {
		t.Fatal("credential clone/clear did not isolate the source")
	}
}

func TestSelectorsRejectAmbiguousAndUnsupportedPaths(t *testing.T) {
	root := "01010101-0101-0101-0101-010101010101"
	g1 := "02020202-0202-0202-0202-020202020202"
	g2 := "03030303-0303-0303-0303-030303030303"
	db := &Database{
		Groups: []Group{
			{UUID: root, Name: "Root"},
			{UUID: g1, ParentUUID: root, Name: "same"},
			{UUID: g2, ParentUUID: root, Name: "same"},
		},
		Entries: []Entry{
			{UUID: "04040404-0404-0404-0404-040404040404", GroupUUID: g1, Fields: []Field{{Name: "Title", Value: "entry"}}},
			{UUID: "05050505-0505-0505-0505-050505050505", GroupUUID: g2, Fields: []Field{{Name: "Title", Value: "entry"}}},
		},
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectors.selectChange(Change{Kind: DeleteGroup, GroupUUID: g1}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected ambiguous group path, got %v", err)
	}
	if _, err := selectors.selectChange(Change{Kind: DeleteEntry, EntryUUID: db.Entries[0].UUID}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected ambiguous entry path due to duplicate sibling group, got %v", err)
	}
	db.Groups[1].Name = "bad/name"
	selectors, _ = selectorsFor(db)
	if _, err := selectors.selectChange(Change{Kind: DeleteEntry, EntryUUID: db.Entries[0].UUID}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported slash path, got %v", err)
	}
}

func TestSelectorsIndexUnrepresentablePathAliases(t *testing.T) {
	root := "01010101-0101-0101-0101-010101010101"
	slashGroup := "02020202-0202-0202-0202-020202020202"
	groupA := "03030303-0303-0303-0303-030303030303"
	nestedB := "04040404-0404-0404-0404-040404040404"
	db := &Database{
		Groups: []Group{
			{UUID: root, Name: "Root"},
			{UUID: slashGroup, ParentUUID: root, Name: "a/b"},
			{UUID: groupA, ParentUUID: root, Name: "a"},
			{UUID: nestedB, ParentUUID: groupA, Name: "b"},
		},
		Entries: []Entry{
			{UUID: "05050505-0505-0505-0505-050505050505", GroupUUID: slashGroup, Fields: []Field{{Name: "Title", Value: "same"}}},
			{UUID: "06060606-0606-0606-0606-060606060606", GroupUUID: nestedB, Fields: []Field{{Name: "Title", Value: "same"}, {Name: "Password", Value: "same-password", Protected: true}}},
		},
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectors.selectChange(Change{Kind: MoveEntry, EntryUUID: db.Entries[1].UUID, GroupUUID: groupA}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected aliased entry selector rejection, got %v", err)
	}
	if _, err := selectors.selectChange(Change{Kind: EditEntry, EntryUUID: db.Entries[1].UUID, Password: []byte("same-password"), PasswordChanged: true}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected same-password edit alias rejection, got %v", err)
	}
	if _, err := selectors.uniqueGroup(nestedB); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected aliased group destination rejection, got %v", err)
	}
	if _, err := selectors.selectChange(Change{Kind: CreateGroup, GroupUUID: groupA, NewName: "b"}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected aliased group creation rejection, got %v", err)
	}
}

func TestSelectorsRejectRenameAndMovePathCollisions(t *testing.T) {
	root := "01010101-0101-0101-0101-010101010101"
	groupA := "02020202-0202-0202-0202-020202020202"
	groupB := "03030303-0303-0303-0303-030303030303"
	target := "04040404-0404-0404-0404-040404040404"
	renamedCollision := "05050505-0505-0505-0505-050505050505"
	moveCollision := "06060606-0606-0606-0606-060606060606"
	db := &Database{
		Groups: []Group{{UUID: root, Name: "Root"}, {UUID: groupA, ParentUUID: root, Name: "A"}, {UUID: groupB, ParentUUID: root, Name: "B"}},
		Entries: []Entry{
			{UUID: target, GroupUUID: groupA, Fields: []Field{{Name: "Title", Value: "target"}, {Name: "Password", Value: "same", Protected: true}}},
			{UUID: renamedCollision, GroupUUID: groupA, Fields: []Field{{Name: "Title", Value: "occupied"}}},
			{UUID: moveCollision, GroupUUID: groupB, Fields: []Field{{Name: "Title", Value: "target"}}},
		},
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectors.selectChange(Change{Kind: EditEntry, EntryUUID: target, Values: map[string]string{"Title": "occupied"}}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected rename collision rejection, got %v", err)
	}
	if _, err := selectors.selectChange(Change{Kind: MoveEntry, EntryUUID: target, GroupUUID: groupB}); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected move collision rejection, got %v", err)
	}
}

func TestEnsurePrivateBackupDirDoesNotChangeExistingPath(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "encrypted.kdbx")
	if err := os.WriteFile(source, []byte("encrypted test content"), 0600); err != nil {
		t.Fatal(err)
	}
	revision, err := Revision(source)
	if err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(base, "shared")
	if err := os.Mkdir(shared, 0755); err != nil {
		t.Fatal(err)
	}
	if err := createBackup(shared, source, revision); err == nil {
		t.Fatal("expected non-private backup directory rejection")
	}
	info, err := os.Stat(shared)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("existing directory mode changed: mode=%v", info.Mode().Perm())
	}

	target := filepath.Join(base, "private-target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "backup-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := createBackup(link, source, revision); err == nil {
		t.Fatal("expected final symlink backup path rejection")
	}
	info, err = os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("symlink target mode changed: mode=%v", info.Mode().Perm())
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target received backup files: count=%d err=%v", len(entries), err)
	}
}

func TestCLIGenerateBoundary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n[ \"$1\" = generate ] && [ \"$2\" = --length ] && [ \"$3\" = 8 ] || exit 1\nprintf 'A1b2C3d4\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := NewCLI(bin).Generate(context.Background(), 8)
	if err != nil || string(got) != "A1b2C3d4" {
		t.Fatalf("Generate() = %q, %v", got, err)
	}
	zero(got)
}

func TestRunChangeKeepsMasterAndNewPasswordOffArguments(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	stage := filepath.Join(dir, "staging.kdbx")
	script := `#!/bin/sh
[ "$#" -eq 5 ] && [ "$1" = edit ] && [ "$2" = -q ] && [ "$3" = --password-prompt ] || exit 10
[ "$4" = "` + stage + `" ] && [ "$5" = /group/entry ] || exit 11
IFS= read -r master || exit 12
IFS= read -r updated || exit 13
[ "$master" = master-secret ] && [ "$updated" = entry-secret ] || exit 14
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cli := NewCLI(bin)
	err := cli.runChange(context.Background(), stage, Credentials{Password: []byte("master-secret")}, Change{Kind: EditEntry, EntryUUID: "entry-uuid", Password: []byte("entry-secret"), PasswordChanged: true}, selectedChange{entryPath: "/group/entry"})
	if err != nil {
		t.Fatalf("runChange failed: %v", err)
	}
}

func TestRunChangeSkipsUnchangedNonPasswordFields(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	stage := filepath.Join(dir, "staging.kdbx")
	script := `#!/bin/sh
[ "$#" -eq 6 ] && [ "$1" = edit ] && [ "$2" = -q ] && [ "$3" = --no-password ] || exit 10
[ "$4" = --notes=changed ] || exit 11
[ "$5" = "` + stage + `" ] && [ "$6" = /group/entry ] || exit 12
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	current := Entry{Fields: []Field{{Name: "URL", Value: "same"}, {Name: "Notes", Value: "old"}}}
	err := NewCLI(bin).runChange(context.Background(), stage, Credentials{NoPassword: true}, Change{Kind: EditEntry, Values: map[string]string{"URL": "same", "Notes": "changed"}}, selectedChange{entryPath: "/group/entry", current: current})
	if err != nil {
		t.Fatalf("runChange failed: %v", err)
	}
}

func TestApplyRealVaultCopyOptIn(t *testing.T) {
	original := os.Getenv("BYPASS_TEST_VAULT")
	password := os.Getenv("BYPASS_TEST_PASSWORD")
	if original == "" || password == "" {
		t.Skip("set BYPASS_TEST_VAULT and BYPASS_TEST_PASSWORD to run mutation integration check")
	}
	originalRevision, err := Revision(original)
	if err != nil {
		t.Fatal("could not fingerprint integration vault")
	}
	pass := []byte(password)
	defer zero(pass)
	creds := Credentials{Password: pass}
	cli := NewCLI("")
	tmp := t.TempDir()
	working := filepath.Join(tmp, "working.kdbx")
	copyFileForTest(t, original, working)
	backupDir := filepath.Join(tmp, "backups")

	db, err := cli.Open(context.Background(), working, creds)
	if err != nil {
		t.Fatalf("could not open working copy: %v", err)
	}
	defer func() {
		if db != nil {
			db.Clear()
		}
	}()

	apply := func(change Change) {
		t.Helper()
		next, applyErr := cli.Apply(context.Background(), db, creds, change, backupDir)
		if applyErr != nil {
			t.Fatalf("change %q failed: %v", change.Kind, applyErr)
		}
		db.Clear()
		db = next
	}

	rootUUID := ""
	for _, group := range db.Groups {
		if group.ParentUUID == "" {
			rootUUID = group.UUID
			break
		}
	}
	if rootUUID == "" {
		t.Fatal("working copy has no root group")
	}
	name := "VaultAdapterTest-" + time.Now().UTC().Format("20060102T150405.000000000")
	apply(Change{Kind: CreateGroup, GroupUUID: rootUUID, NewName: name + "-A"})
	groupA := groupIDByName(db, rootUUID, name+"-A")
	apply(Change{Kind: CreateGroup, GroupUUID: rootUUID, NewName: name + "-B"})
	groupB := groupIDByName(db, rootUUID, name+"-B")
	if groupA == "" || groupB == "" {
		t.Fatal("created group UUID was not present after refresh")
	}
	entryPassword := []byte("TemporaryEntryPassword-8x")
	defer zero(entryPassword)
	entryTitle := name + "-entry"
	apply(Change{Kind: AddEntry, GroupUUID: groupA, Values: map[string]string{"Title": entryTitle, "UserName": "test user", "URL": "https://example.invalid", "Notes": "first line\nsecond line"}, Password: entryPassword, PasswordChanged: true})
	entryID := entryIDByTitle(db, groupA, entryTitle)
	if entryID == "" {
		t.Fatal("created entry UUID was not present after refresh")
	}
	referencePassword := []byte("{REF:P@I:" + strings.ToUpper(strings.ReplaceAll(entryID, "-", "")) + "}")
	defer zero(referencePassword)
	referenceTitle := name + "-reference"
	apply(Change{Kind: AddEntry, GroupUUID: groupA, Values: map[string]string{"Title": referenceTitle}, Password: referencePassword, PasswordChanged: true})
	referenceID := entryIDByTitle(db, groupA, referenceTitle)
	resolvedPassword, err := cli.ResolveField(context.Background(), db, creds, referenceID, "Password")
	if err != nil {
		t.Fatalf("could not resolve referenced password: %v", err)
	}
	if !bytes.Equal(resolvedPassword, entryPassword) {
		zero(resolvedPassword)
		t.Fatalf("resolved reference length mismatch: got=%d want=%d", len(resolvedPassword), len(entryPassword))
	}
	zero(resolvedPassword)
	occupiedTitle := name + "-occupied-title"
	apply(Change{Kind: AddEntry, GroupUUID: groupA, Values: map[string]string{"Title": occupiedTitle}})
	apply(Change{Kind: AddEntry, GroupUUID: groupB, Values: map[string]string{"Title": entryTitle}})
	beforeCollisions, err := Revision(working)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cli.Apply(context.Background(), db, creds, Change{Kind: EditEntry, EntryUUID: entryID, Values: map[string]string{"Title": occupiedTitle}}, backupDir)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected rename collision rejection, got %v", err)
	}
	if after, err := Revision(working); err != nil || after != beforeCollisions {
		t.Fatal("rename collision changed the working vault")
	}
	_, err = cli.Apply(context.Background(), db, creds, Change{Kind: MoveEntry, EntryUUID: entryID, GroupUUID: groupB}, backupDir)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected move collision rejection, got %v", err)
	}
	if after, err := Revision(working); err != nil || after != beforeCollisions {
		t.Fatal("move collision changed the working vault")
	}
	occupiedEntryID := entryIDByTitle(db, groupB, entryTitle)
	apply(Change{Kind: DeleteEntry, EntryUUID: occupiedEntryID})
	apply(Change{Kind: DeleteEntry, EntryUUID: occupiedEntryID})
	if entryByID(db, occupiedEntryID) != nil {
		t.Fatal("occupied title setup entry could not be removed from recycle bin")
	}
	newPassword := []byte("TemporaryEntryPassword-Changed")
	defer zero(newPassword)
	apply(Change{Kind: EditEntry, EntryUUID: entryID, Values: map[string]string{"URL": "https://example.invalid/changed", "Notes": "updated notes"}, Password: newPassword, PasswordChanged: true})
	apply(Change{Kind: MoveEntry, EntryUUID: entryID, GroupUUID: groupB})
	if entryIDByTitle(db, groupB, entryTitle) != entryID {
		t.Fatal("move did not preserve entry identity")
	}
	apply(Change{Kind: DeleteEntry, EntryUUID: entryID})
	if entry := entryByID(db, entryID); entry == nil || !entry.Recycled {
		t.Fatal("first delete did not move entry to recycle bin")
	}
	apply(Change{Kind: DeleteEntry, EntryUUID: entryID})
	if entryByID(db, entryID) != nil {
		t.Fatal("second delete did not permanently remove recycled entry")
	}
	apply(Change{Kind: DeleteGroup, GroupUUID: groupA})
	if groupByID(db, groupA) == nil || !groupByID(db, groupA).Recycled {
		t.Fatal("first group delete did not recycle group")
	}
	apply(Change{Kind: DeleteGroup, GroupUUID: groupA})
	if groupByID(db, groupA) != nil {
		t.Fatal("second group delete did not permanently delete recycled group")
	}
	backupEntries, err := os.ReadDir(backupDir)
	if err != nil || len(backupEntries) == 0 || len(backupEntries) > 5 {
		t.Fatalf("backup retention check failed: count=%d err=%v", len(backupEntries), err)
	}
	backupInfo, err := os.Stat(filepath.Join(backupDir, backupEntries[len(backupEntries)-1].Name()))
	if err != nil || backupInfo.Mode().Perm() != 0600 {
		t.Fatal("encrypted backup is not private")
	}
	if current, err := Revision(working); err != nil || current != db.Revision {
		t.Fatal("returned database revision does not match installed vault")
	}

	beforeFailure, err := Revision(working)
	if err != nil {
		t.Fatal(err)
	}
	backupBlocker := filepath.Join(tmp, "backup-blocker")
	if err := os.WriteFile(backupBlocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = cli.Apply(context.Background(), db, creds, Change{Kind: CreateGroup, GroupUUID: rootUUID, NewName: name + "-failure"}, filepath.Join(backupBlocker, "backups"))
	if !errors.Is(err, ErrBackup) {
		t.Fatalf("expected backup failure, got %v", err)
	}
	if current, revisionErr := Revision(working); revisionErr != nil || current != beforeFailure {
		t.Fatal("original working copy changed after backup failure")
	}
	for _, item := range mustReadDir(t, tmp) {
		if strings.Contains(item.Name(), ".bypass-") {
			t.Fatal("staging file was not cleaned after backup failure")
		}
	}
	apply(Change{Kind: CreateGroup, GroupUUID: rootUUID, NewName: name + "-retry"})

	conflictPath := filepath.Join(tmp, "external-change.kdbx")
	copyFileForTest(t, working, conflictPath)
	staleDB, err := cli.Open(context.Background(), conflictPath, creds)
	if err != nil {
		t.Fatal("could not open conflict test copy")
	}
	externalFile, err := os.OpenFile(conflictPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		staleDB.Clear()
		t.Fatal("could not simulate external change")
	}
	_, writeErr := externalFile.Write([]byte("external"))
	closeErr := externalFile.Close()
	if writeErr != nil || closeErr != nil {
		staleDB.Clear()
		t.Fatal("could not complete external change simulation")
	}
	conflictRevision, err := Revision(conflictPath)
	if err != nil {
		staleDB.Clear()
		t.Fatal(err)
	}
	_, err = cli.Apply(context.Background(), staleDB, creds, Change{Kind: CreateGroup, GroupUUID: rootUUID, NewName: name + "-conflict"}, backupDir)
	staleDB.Clear()
	if !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	if afterConflict, revisionErr := Revision(conflictPath); revisionErr != nil || afterConflict != conflictRevision {
		t.Fatal("conflicting external vault was overwritten")
	}

	readOnly := filepath.Join(tmp, "readonly.kdbx")
	copyFileForTest(t, working, readOnly)
	readOnlyDB, err := cli.Open(context.Background(), readOnly, creds)
	if err != nil {
		t.Fatal("could not open read-only copy")
	}
	defer readOnlyDB.Clear()
	if err := os.Chmod(readOnly, 0400); err != nil {
		t.Fatal(err)
	}
	_, err = cli.Apply(context.Background(), readOnlyDB, creds, Change{Kind: CreateGroup, GroupUUID: rootUUID, NewName: name + "-readonly"}, backupDir)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected read-only sentinel, got %v", err)
	}
	if got, err := Revision(original); err != nil || got != originalRevision {
		t.Fatal("source integration vault changed during copy mutations")
	}
	if _, err := os.Stat(working); err != nil {
		t.Fatal("temporary working copy disappeared")
	}
	if strings.Contains(name, "\n") {
		t.Fatal("unexpected integration name")
	}
}

func mustReadDir(t *testing.T, path string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func copyFileForTest(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal("could not read encrypted test vault")
	}
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatal("could not create encrypted test copy")
	}
	if got, err := Revision(dst); err != nil || got != sha256.Sum256(data) {
		t.Fatal("encrypted test copy fingerprint mismatch")
	}
}

func groupIDByName(db *Database, parentUUID, name string) string {
	for _, group := range db.Groups {
		if group.ParentUUID == parentUUID && group.Name == name {
			return group.UUID
		}
	}
	return ""
}

func entryIDByTitle(db *Database, groupUUID, title string) string {
	for _, entry := range db.Entries {
		if entry.GroupUUID == groupUUID && entry.Get("Title") == title {
			return entry.UUID
		}
	}
	return ""
}

func entryByID(db *Database, uuid string) *Entry {
	for i := range db.Entries {
		if db.Entries[i].UUID == uuid {
			return &db.Entries[i]
		}
	}
	return nil
}

func groupByID(db *Database, uuid string) *Group {
	for i := range db.Groups {
		if db.Groups[i].UUID == uuid {
			return &db.Groups[i]
		}
	}
	return nil
}
