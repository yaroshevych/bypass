package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
