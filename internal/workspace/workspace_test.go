package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"keepassui/internal/vault"
)

func database(path, name, root string) *vault.Database {
	return &vault.Database{
		Path: path, Name: name, RootUUID: root,
		Entries: []vault.Entry{{UUID: "entry", Fields: []vault.Field{{Name: "Password", Value: "secret", Protected: true}}}},
	}
}

func TestOpenDeduplicatesCanonicalPathAndSeparatesSameBasename(t *testing.T) {
	root := t.TempDir()
	firstDir := filepath.Join(root, "one")
	secondDir := filepath.Join(root, "two")
	if err := os.MkdirAll(firstDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondDir, 0o700); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(firstDir, "vault.kdbx")
	secondPath := filepath.Join(secondDir, "vault.kdbx")
	if err := os.WriteFile(firstPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias.kdbx")
	if err := os.Symlink(firstPath, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	w := New()
	firstDB := database(firstPath, "first", "root-a")
	if w.Open(firstDB) {
		t.Fatal("first open unexpectedly reused a session")
	}
	aliasDB := database(link, "first", "root-a")
	if !w.Open(aliasDB) {
		t.Fatal("symlink open did not reuse session")
	}
	if len(aliasDB.Entries) != 0 {
		t.Fatal("duplicate decrypted database copy was not cleared")
	}
	if len(w.Sessions) != 1 || len(w.Views) != 1 {
		t.Fatalf("same canonical path was not shared: %#v %#v", w.Sessions, w.Views)
	}
	if w.Active != 0 {
		t.Fatalf("reopening an existing vault did not switch to its existing view: active=%d", w.Active)
	}
	if w.Open(database(secondPath, "second", "root-b")) {
		t.Fatal("different path unexpectedly reused a session")
	}
	if len(w.Sessions) != 2 || w.ActiveSession().DB.Name != "second" {
		t.Fatalf("different same-basename vaults were not independent: %#v", w.Sessions)
	}
}

func TestOpenLockedSessionRefreshesAndDuplicateCopiesView(t *testing.T) {
	w := New()
	db := database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root")
	w.Open(db)
	view := w.ActiveView()
	view.Query, view.SelectedUUID, view.DetailOffset = "secret query", "entry-uuid", 4
	if !w.Duplicate() {
		t.Fatal("Duplicate failed")
	}
	if len(w.Views) != 2 || w.Views[0].ID == w.Views[1].ID || w.Views[0].Query != w.Views[1].Query {
		t.Fatalf("duplicate did not copy view state with new identity: %#v", w.Views)
	}
	if !w.Open(db) || len(w.Views) != 2 || w.Active != 0 || w.ActiveSession().DB != db {
		t.Fatal("reopening the same DB did not switch views without clearing the shared pointer")
	}
	if !w.LockActive() {
		t.Fatal("LockActive failed")
	}
	if w.ActiveSession().DB != nil || len(db.Entries) != 0 {
		t.Fatal("lock retained database secrets")
	}
	for _, v := range w.Views {
		if v.Query != "" || v.DetailOffset != 0 {
			t.Errorf("locked view retained query/detail state: %#v", v)
		}
		if v.GroupUUID != "root" || v.SelectedUUID != "entry-uuid" {
			t.Errorf("lock did not preserve navigation UUIDs: %#v", v)
		}
	}
	refreshed := database(db.Path, "refreshed", "root")
	if !w.Open(refreshed) || len(w.Sessions) != 1 || len(w.Views) != 2 {
		t.Fatal("opening locked path did not refresh existing session")
	}
	if w.ActiveSession().DB != refreshed {
		t.Fatal("locked session was not refreshed with supplied database")
	}
}

func TestCloseViewCleansLastSessionAndCloseVaultRemovesAllSharedViews(t *testing.T) {
	w := New()
	db := database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root")
	w.Open(db)
	w.Duplicate()
	if !w.CloseView() || len(w.Views) != 1 || len(w.Sessions) != 1 {
		t.Fatal("closing one of multiple shared views removed the session")
	}
	if !w.CloseView() || len(w.Views) != 0 || len(w.Sessions) != 0 || len(db.Entries) != 0 {
		t.Fatal("closing last view did not clear and remove session")
	}

	db = database(filepath.Join(t.TempDir(), "other.kdbx"), "other", "root")
	w.Open(db)
	w.Duplicate()
	if !w.CloseVault() || len(w.Views) != 0 || len(w.Sessions) != 0 || len(db.Entries) != 0 {
		t.Fatal("CloseVault did not clear the entire shared session")
	}
}

func TestCloseOperationsZeroRemovedViewSlots(t *testing.T) {
	w := New()
	db := database(filepath.Join(t.TempDir(), "first.kdbx"), "first", "root")
	w.Open(db)
	w.ActiveView().Query = "query secret"
	w.Duplicate()
	w.ActiveView().Query = "second query secret"
	backing := w.Views
	if !w.CloseView() {
		t.Fatal("CloseView failed")
	}
	if backing[1] != (View{}) {
		t.Fatalf("removed view backing slot retained data: %#v", backing[1])
	}

	w.Duplicate()
	other := database(filepath.Join(t.TempDir(), "second.kdbx"), "second", "root")
	w.Open(other)
	w.Switch(0)
	backing = w.Views
	if !w.CloseVault() {
		t.Fatal("CloseVault failed")
	}
	if backing[1] != (View{}) || backing[2] != (View{}) {
		t.Fatalf("removed vault view slots retained data: %#v %#v", backing[1], backing[2])
	}
	if len(w.Views) != 1 || w.ActiveView().SessionID == "" {
		t.Fatalf("CloseVault removed the unrelated view: %#v", w.Views)
	}
}

func TestCloseClearsEverySession(t *testing.T) {
	w := New()
	first := database(filepath.Join(t.TempDir(), "one.kdbx"), "one", "root")
	second := database(filepath.Join(t.TempDir(), "two.kdbx"), "two", "root")
	w.Open(first)
	w.Open(second)
	w.Close()
	if len(w.Views) != 0 || len(w.Sessions) != 0 || len(first.Entries) != 0 || len(second.Entries) != 0 {
		t.Fatal("Close left session data behind")
	}
}
