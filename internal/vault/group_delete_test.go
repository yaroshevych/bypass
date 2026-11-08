package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
