package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gok "github.com/tobischo/gokeepasslib/v3"
)

func TestNativeGroupDeletePreservesSubtreeInRecycleBin(t *testing.T) {
	_, creds, db, backend := nativeFixture(t)
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Work"}, backup)
	var workID string
	for _, group := range db.Groups {
		if group.Name == "Work" {
			workID = group.UUID
		}
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: workID, NewName: "Child"}, backup)
	var childID string
	for _, group := range db.Groups {
		if group.Name == "Child" {
			childID = group.UUID
		}
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: AddEntry, GroupUUID: childID, Values: map[string]string{"Title": "Keep"}}, backup)
	applyFixtureChange(t, backend, &db, creds, Change{Kind: PrepareDeleteGroup, GroupUUID: workID}, backup)
	if db.GroupDeletePlan == nil || db.GroupDeletePlan.TargetEntries != 1 || db.GroupDeletePlan.TargetGroups != 2 {
		t.Fatal("group deletion plan omitted nested contents")
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: DeleteGroup, GroupUUID: workID}, backup)
	var work, child *Group
	for i := range db.Groups {
		if db.Groups[i].UUID == workID {
			work = &db.Groups[i]
		}
		if db.Groups[i].UUID == childID {
			child = &db.Groups[i]
		}
	}
	if work == nil || child == nil || work.ParentUUID != db.RecycleBinUUID || !work.Recycled || !child.Recycled {
		t.Fatal("group deletion did not preserve the subtree under the recycle bin")
	}
	if len(db.Entries) != 1 || db.Entries[0].GroupUUID != childID || !db.Entries[0].Recycled {
		t.Fatal("entry was not preserved in the recycled child group")
	}
}

func TestNativeGroupDeleteRejectsExternalChangeAfterReview(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Work"}, backup)
	var groupID string
	for _, group := range db.Groups {
		if group.Name == "Work" {
			groupID = group.UUID
		}
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: PrepareDeleteGroup, GroupUUID: groupID}, backup)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("external change")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = backend.Apply(context.Background(), db, creds, Change{Kind: DeleteGroup, GroupUUID: groupID}, backup)
	if !errors.Is(err, ErrExternalChanged) {
		t.Fatal("group deletion ignored a file change after review")
	}
}

func TestNativeGroupDeleteWithoutRecycleBinIsPermanent(t *testing.T) {
	_, creds, db, backend := nativeFixture(t)
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Work"}, backup)
	var groupID string
	for _, group := range db.Groups {
		if group.Name == "Work" {
			groupID = group.UUID
		}
	}
	db.native.db.Content.Meta.RecycleBinEnabled.Bool = false
	db.RecycleBinEnabled = false
	updated, err := backend.Apply(context.Background(), db, creds, Change{Kind: DeleteGroup, GroupUUID: groupID}, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	db = updated
	defer db.Clear()
	for _, group := range db.Groups {
		if group.UUID == groupID {
			t.Fatal("permanently deleted group remains")
		}
	}
}

func TestNativeGroupDeleteHandlesXMLRootSiblingsAndProtectsModelRoot(t *testing.T) {
	_, creds, db, backend := nativeFixture(t)
	defer db.Clear()
	backup := filepath.Join(t.TempDir(), "backups")
	rootGroup := db.native.db.Content.Root.Groups[0]
	sibling := gok.NewGroup()
	sibling.Name = "Extra top-level group"
	later := gok.NewGroup()
	later.Name = "Later top-level group"
	db.native.db.Content.Root.Groups = append(db.native.db.Content.Root.Groups, sibling, later)
	refreshed, err := nativeModel(db.native)
	if err != nil {
		t.Fatal(err)
	}
	db.Groups = refreshed.Groups
	db.RootUUID = refreshed.RootUUID
	var siblingID string
	for _, group := range db.Groups {
		if group.Name == sibling.Name {
			siblingID = group.UUID
		}
	}
	if siblingID == "" {
		t.Fatal("top-level sibling was not modeled")
	}
	if _, err := backend.Apply(context.Background(), db, creds, Change{Kind: DeleteGroup, GroupUUID: db.RootUUID}, backup); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("deleting actual database root error = %v", err)
	}
	applyFixtureChange(t, backend, &db, creds, Change{Kind: PrepareDeleteGroup, GroupUUID: siblingID}, backup)
	updated, err := backend.Apply(context.Background(), db, creds, Change{Kind: DeleteGroup, GroupUUID: siblingID}, backup)
	if err != nil {
		t.Fatalf("delete XML-root sibling: %v", err)
	}
	db.Clear()
	defer updated.Clear()
	if len(updated.native.db.Content.Root.Groups) != 2 || uuidString(updated.native.db.Content.Root.Groups[0].UUID) != uuidString(rootGroup.UUID) || updated.native.db.Content.Root.Groups[1].Name != later.Name {
		t.Fatal("deleting top-level sibling removed or replaced another root group")
	}
	bin := findNativeGroup(updated.native.db, updated.RecycleBinUUID)
	if bin == nil || len(bin.Groups) != 1 || bin.Groups[0].Name != sibling.Name {
		t.Fatal("deleting a top-level sibling moved the wrong group")
	}
}
