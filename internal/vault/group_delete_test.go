package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyGroupRejectsDuplicateChildrenBeforeMovingEntries(t *testing.T) {
	binary, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("native CLI required")
	}
	dir := t.TempDir()
	path, xmlPath := filepath.Join(dir, "duplicate.kdbx"), filepath.Join(dir, "fixture.xml")
	xml := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Fixture</DatabaseName><RecycleBinEnabled>True</RecycleBinEnabled><RecycleBinUUID>AgICAgICAgICAgICAgICAg==</RecycleBinUUID></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Passwords</Name><Group><UUID>AgICAgICAgICAgICAgICAg==</UUID><Name>Recycle Bin</Name></Group><Group><UUID>AwMDAwMDAwMDAwMDAwMDAw==</UUID><Name>Work</Name><Group><UUID>BAQEBAQEBAQEBAQEBAQEBA==</UUID><Name>Child</Name><Entry><UUID>BQUFBQUFBQUFBQUFBQUFBQ==</UUID><String><Key>Title</Key><Value>First</Value></String></Entry></Group><Group><UUID>BgYGBgYGBgYGBgYGBgYGBg==</UUID><Name>Child</Name><Entry><UUID>BwcHBwcHBwcHBwcHBwcHBw==</UUID><String><Key>Title</Key><Value>Second</Value></String></Entry></Group></Group></Group></Root></KeePassFile>`
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, path)
	cmd.Stdin = bytes.NewBufferString("fixture-password\nfixture-password\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture import: %v: %s", err, output)
	}
	creds := Credentials{Password: []byte("fixture-password")}
	defer creds.Clear()
	s, err := startInteractive(context.Background(), binary, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	before, err := Revision(path)
	if err != nil {
		t.Fatal(err)
	}
	budget := 100
	err = s.emptyGroup(context.Background(), "/Work/", "/Recycle Bin/", 0, false, &budget)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected ambiguous duplicate children, got %v", err)
	}
	after, err := Revision(path)
	if err != nil || before != after {
		t.Fatalf("ambiguous traversal changed vault: %v", err)
	}
}

func TestGroupDeletionRejectsChangedVaultAfterReview(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	backup := filepath.Join(t.TempDir(), "backups")
	for _, change := range []Change{{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Work"}, {Kind: PrepareDeleteGroup, GroupUUID: groupID("/Work/")}} {
		next, err := backend.Apply(context.Background(), db, Credentials{}, change, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(next)
		db.Clear()
		db = next
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("external change")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = backend.Apply(context.Background(), db, Credentials{}, Change{Kind: DeleteGroup, GroupUUID: groupID("/Work/")}, backup)
	if !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("expected external change rejection, got %v", err)
	}
}

func TestGroupDeletionWithEmptyMatchingGroups(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mkdir", "--", "/Work"}, {"rmdir", "--", "/Work"}, {"mkdir", "--", "/Work"}} {
		out, err := db.interactive.command(context.Background(), args...)
		if err != nil || !strings.HasPrefix(string(out), "Successfully ") {
			t.Fatalf("fixture command failed: %v", err)
		}
		zero(out)
	}
	db.Clear()
	db, err = backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	backup := filepath.Join(t.TempDir(), "backups")
	for _, kind := range []ChangeKind{PrepareDeleteGroup, DeleteGroup} {
		next, err := backend.Apply(context.Background(), db, Credentials{}, Change{Kind: kind, GroupUUID: groupID("/Work/")}, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(next)
		db.Clear()
		db = next
		if kind == PrepareDeleteGroup && (db.GroupDeletePlan.MatchingGroups != 1 || db.GroupDeletePlan.MatchingEntries != 0) {
			t.Fatal("incorrect empty group plan")
		}
	}
}

func TestGroupDeletionFindsRenamedRecycleBin(t *testing.T) {
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("native CLI required")
	}
	dir := t.TempDir()
	path, xmlPath := filepath.Join(dir, "renamed.kdbx"), filepath.Join(dir, "fixture.xml")
	xml := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Fixture</DatabaseName><RecycleBinEnabled>True</RecycleBinEnabled><RecycleBinUUID>AgICAgICAgICAgICAgICAg==</RecycleBinUUID></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Passwords</Name><Group><UUID>AgICAgICAgICAgICAgICAg==</UUID><Name>Old Items</Name></Group><Group><UUID>AwMDAwMDAwMDAwMDAwMDAw==</UUID><Name>Work</Name><Entry><UUID>BAQEBAQEBAQEBAQEBAQEBA==</UUID><String><Key>Title</Key><Value>Keep</Value></String></Entry></Group></Group></Root></KeePassFile>`
	xml = strings.Replace(xml, "</Group></Root>", `<Group><UUID>BQUFBQUFBQUFBQUFBQUFBQ==</UUID><Name>Recycle Bin</Name></Group></Group></Root>`, 1)
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("keepassxc-cli", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, path)
	cmd.Stdin = bytes.NewBufferString("fixture-password\nfixture-password\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture import: %v: %s", err, output)
	}
	creds := Credentials{Password: []byte("fixture-password")}
	defer creds.Clear()
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	backup := filepath.Join(dir, "backups")
	for _, kind := range []ChangeKind{PrepareDeleteGroup, DeleteGroup} {
		next, err := backend.Apply(context.Background(), db, Credentials{}, Change{Kind: kind, GroupUUID: groupID("/Work/")}, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(next)
		db.Clear()
		db = next
	}
	if db.RecycleBinUUID != groupID("/Old Items/") || len(db.Entries) != 1 || !db.Entries[0].Recycled || db.Entries[0].Get("Title") != "Keep" {
		t.Fatalf("renamed bin not used: %+v", db.Groups)
	}
	for _, group := range db.Groups {
		if group.Name == "Recycle Bin" && group.Recycled {
			t.Fatal("ordinary group was mistaken for the actual recycle bin")
		}
	}
}

func TestGroupDeletionWithoutRecycleBinIsPermanent(t *testing.T) {
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("native CLI required")
	}
	dir := t.TempDir()
	path, xmlPath := filepath.Join(dir, "disabled.kdbx"), filepath.Join(dir, "fixture.xml")
	xml := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Fixture</DatabaseName><RecycleBinEnabled>False</RecycleBinEnabled></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Passwords</Name><Group><UUID>AwMDAwMDAwMDAwMDAwMDAw==</UUID><Name>Work</Name><Entry><UUID>BAQEBAQEBAQEBAQEBAQEBA==</UUID><String><Key>Title</Key><Value>Remove</Value></String></Entry></Group></Group></Root></KeePassFile>`
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("keepassxc-cli", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, path)
	cmd.Stdin = bytes.NewBufferString("fixture-password\nfixture-password\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture import: %v: %s", err, output)
	}
	creds := Credentials{Password: []byte("fixture-password")}
	defer creds.Clear()
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if db.RecycleBinEnabled {
		t.Fatal("fixture unexpectedly enabled recycle bin")
	}
	updated, err := backend.Apply(context.Background(), db, Credentials{}, Change{Kind: DeleteGroup, GroupUUID: groupID("/Work/")}, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	db.TransferInteractive(updated)
	defer updated.Clear()
	if len(updated.Entries) != 0 || len(updated.Groups) != 1 {
		t.Fatal("group and contents were not permanently deleted")
	}
}

func TestEntryDeletionVerifiesRenamedBinBeforePermanentDelete(t *testing.T) {
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("native CLI required")
	}
	dir := t.TempDir()
	path, xmlPath := filepath.Join(dir, "renamed.kdbx"), filepath.Join(dir, "fixture.xml")
	xml := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Fixture</DatabaseName><RecycleBinEnabled>True</RecycleBinEnabled><RecycleBinUUID>AgICAgICAgICAgICAgICAg==</RecycleBinUUID></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Passwords</Name><Group><UUID>AgICAgICAgICAgICAgICAg==</UUID><Name>Old Items</Name><Entry><UUID>BAQEBAQEBAQEBAQEBAQEBA==</UUID><String><Key>Title</Key><Value>Remove</Value></String></Entry></Group></Group></Root></KeePassFile>`
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("keepassxc-cli", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, path)
	cmd.Stdin = bytes.NewBufferString("fixture-password\nfixture-password\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture import: %v: %s", err, output)
	}
	creds := Credentials{Password: []byte("fixture-password")}
	defer creds.Clear()
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	id := db.Entries[0].UUID
	backup := filepath.Join(dir, "backups")
	if _, err := backend.Apply(context.Background(), db, Credentials{}, Change{Kind: DeleteEntry, EntryUUID: id}, backup); err == nil {
		t.Fatal("unverified deletion was accepted")
	}
	for _, kind := range []ChangeKind{PrepareRecycleBin, DeleteEntry} {
		next, err := backend.Apply(context.Background(), db, Credentials{}, Change{Kind: kind, EntryUUID: id}, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(next)
		db.Clear()
		db = next
		if kind == PrepareRecycleBin && (len(db.Entries) != 1 || !db.Entries[0].Recycled) {
			t.Fatal("entry was not identified as recycled")
		}
	}
	if len(db.Entries) != 0 {
		t.Fatal("entry was not permanently deleted")
	}
}

func TestGroupDeletionFlattensMatchingGroupsAndTarget(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	ctx := context.Background()
	backend := NewInteractiveCLI("")
	db, err := backend.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"mkdir", "--", "/Work"}, {"add", "--", "/Work/Same"}, {"mkdir", "--", "/Work/Nested"}, {"add", "--", "/Work/Nested/Old"}, {"rmdir", "--", "/Work"},
		{"mkdir", "--", "/Work"}, {"add", "--", "/Work/Same"}, {"rmdir", "--", "/Work"},
		{"mkdir", "--", "/Work"}, {"add", "--", "/Work/Same"}, {"mkdir", "--", "/Work/Child"}, {"add", "--", "/Work/Child/New"},
	} {
		out, err := db.interactive.command(ctx, args...)
		if err != nil || !strings.HasPrefix(string(out), "Successfully ") {
			t.Fatalf("native fixture command %q failed: %v", args[0], err)
		}
		zero(out)
	}
	db.Clear()
	db, err = backend.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	id := groupID("/Work/")
	backup := filepath.Join(t.TempDir(), "backups")
	apply := func(kind ChangeKind) {
		t.Helper()
		next, err := backend.Apply(ctx, db, Credentials{}, Change{Kind: kind, GroupUUID: id}, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(next)
		db.Clear()
		db = next
	}
	apply(PrepareDeleteGroup)
	plan := db.GroupDeletePlan
	if plan == nil || plan.TargetEntries != 2 || plan.TargetGroups != 2 || plan.MatchingGroups != 2 || plan.MatchingEntries != 3 {
		t.Fatalf("unexpected deletion plan: %+v", plan)
	}
	apply(DeleteGroup)
	want := map[string]bool{"Same": true, "Same 1": true, "Same 2": true, "Old": true, "New": true}
	for _, e := range db.Entries {
		if !e.Recycled || e.GroupUUID != db.RecycleBinUUID || !want[e.Get("Title")] {
			t.Fatalf("unexpected entry after delete: %+v", e)
		}
		delete(want, e.Get("Title"))
	}
	if len(want) != 0 {
		t.Fatalf("missing entries: %v", want)
	}
	for _, g := range db.Groups {
		if g.Name == "Work" || g.Name == "Child" || g.Name == "Nested" {
			t.Fatalf("group remained: %+v", g)
		}
	}
}
