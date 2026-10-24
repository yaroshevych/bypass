package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestNineTabLimitAllowsExistingVaultAndRejectsNewVault(t *testing.T) {
	w := New()
	path := filepath.Join(t.TempDir(), "existing.kdbx")
	w.Open(database(path, "existing", "root"))
	for len(w.Views) < MaxTabs {
		w.Duplicate()
	}
	if w.Duplicate() || !w.CanOpen(path) {
		t.Fatal("limit must reject duplication but permit existing vault activation")
	}
	rejected := database(filepath.Join(t.TempDir(), "other.kdbx"), "other", "root2")
	if w.CanOpen(rejected.Path) {
		t.Fatal("limit allowed a new vault")
	}
	w.Open(rejected)
	if len(w.Views) != MaxTabs || len(w.Sessions) != 1 {
		t.Fatal("rejected vault changed workspace")
	}
	if !w.Open(database(path, "existing", "root")) || w.Active != MaxTabs-1 {
		t.Fatal("existing vault did not restore its last active view at capacity")
	}
}

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
	if !w.Open(db) || len(w.Views) != 2 || w.Active != 1 || w.ActiveSession().DB != db {
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
	w.ActiveSession().Stale = true
	if !w.Open(refreshed) || len(w.Sessions) != 1 || len(w.Views) != 2 {
		t.Fatal("opening locked path did not refresh existing session")
	}
	if w.ActiveSession().DB != refreshed || w.ActiveSession().Stale {
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

func TestSetCredentialsClonesAndReplacingCredentialsClearsOldCopy(t *testing.T) {
	w := New()
	w.Open(database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root"))
	session := w.ActiveSession()
	input := vault.Credentials{Password: []byte("first-secret"), KeyFile: "/keys/first"}
	if !w.SetCredentials(session.ID, input) {
		t.Fatal("SetCredentials failed")
	}
	input.Password[0] = 'X'
	if string(session.Credentials.Password) != "first-secret" {
		t.Fatal("session retained caller-owned password bytes")
	}
	oldPassword := session.Credentials.Password
	if !w.SetCredentials(session.ID, vault.Credentials{Password: []byte("next-secret")}) {
		t.Fatal("replacing credentials failed")
	}
	if string(oldPassword) != string(make([]byte, len(oldPassword))) {
		t.Fatal("replaced credential bytes were not cleared")
	}
	if string(session.Credentials.Password) != "next-secret" {
		t.Fatal("replacement credentials were not cloned")
	}
}

func TestReplaceDatabasePreservesViewUUIDsAndClearsOldDatabase(t *testing.T) {
	w := New()
	first := database(filepath.Join(t.TempDir(), "vault.kdbx"), "old", "root")
	w.Open(first)
	w.Duplicate()
	for i := range w.Views {
		w.Views[i].GroupUUID = "group-id"
		w.Views[i].SelectedUUID = "entry-id"
		w.Views[i].Query = "private query"
	}
	session := w.ActiveSession()
	session.Stale = true
	second := database(first.Path, "new", "new-root")
	if !w.ReplaceDatabase(session.ID, second) {
		t.Fatal("ReplaceDatabase failed")
	}
	if len(first.Entries) != 0 || session.DB != second || session.Stale {
		t.Fatal("database was not replaced and old content cleared")
	}
	for _, view := range w.Views {
		if view.GroupUUID != "group-id" || view.SelectedUUID != "entry-id" || view.Query != "private query" {
			t.Fatalf("replacement changed view identity/state: %#v", view)
		}
	}
	if w.ReplaceDatabase("missing", database("missing", "x", "x")) {
		t.Fatal("ReplaceDatabase accepted an unknown session")
	}
}

func TestLockSessionAndLockAllClearCredentialsAcrossViews(t *testing.T) {
	w := New()
	first := database(filepath.Join(t.TempDir(), "one.kdbx"), "one", "root-one")
	w.Open(first)
	w.Duplicate()
	firstSession := w.ActiveSession()
	firstID := firstSession.ID
	w.SetCredentials(firstID, vault.Credentials{Password: []byte("one-secret")})
	firstPassword := firstSession.Credentials.Password
	for i := range w.Views {
		w.Views[i].Query = "search term"
		w.Views[i].DetailOffset = 3
		w.Views[i].FieldIndex = 2
		w.Views[i].QueryScope = "query scope secret"
		w.Views[i].SelectedUUID = "selected-entry"
	}
	if !w.LockSession(firstID) {
		t.Fatal("LockSession failed")
	}
	if firstSession.DB != nil || len(first.Entries) != 0 || len(firstPassword) == 0 || string(firstPassword) != string(make([]byte, len(firstPassword))) {
		t.Fatal("LockSession did not clear DB and credential bytes")
	}
	for _, view := range w.Views {
		if view.SessionID == firstID && (view.Query != "" || view.QueryScope != "" || view.DetailOffset != 0 || view.FieldIndex != 0 || view.SelectedUUID != "selected-entry") {
			t.Fatalf("LockSession retained query/detail or erased selection UUID: %#v", view)
		}
	}

	second := database(filepath.Join(t.TempDir(), "two.kdbx"), "two", "root-two")
	w.Open(second)
	secondSession := w.ActiveSession()
	w.SetCredentials(secondSession.ID, vault.Credentials{Password: []byte("two-secret")})
	secondPassword := secondSession.Credentials.Password
	w.LockAll()
	if secondSession.DB != nil || len(second.Entries) != 0 || string(secondPassword) != string(make([]byte, len(secondPassword))) {
		t.Fatal("LockAll did not clear independent session data")
	}
	if firstSession.Credentials.Password != nil || secondSession.Credentials.Password != nil {
		t.Fatal("LockAll retained credentials")
	}
	if !firstSession.ReadOnly || !secondSession.ReadOnly {
		t.Fatal("sessions were not marked read-only")
	}
	if firstSession.LastActivity.IsZero() || secondSession.LastActivity.IsZero() {
		t.Fatal("sessions did not record activity")
	}
}

func TestRemovedSessionsClearCredentials(t *testing.T) {
	w := New()
	db := database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root")
	w.Open(db)
	session := w.ActiveSession()
	w.SetCredentials(session.ID, vault.Credentials{Password: []byte("close-secret")})
	password := session.Credentials.Password
	if !w.CloseVault() || session.Credentials.Password != nil || string(password) != string(make([]byte, len(password))) {
		t.Fatal("CloseVault retained removed-session credentials")
	}

	db = database(filepath.Join(t.TempDir(), "other.kdbx"), "other", "root")
	w.Open(db)
	session = w.ActiveSession()
	w.SetCredentials(session.ID, vault.Credentials{Password: []byte("shutdown-secret")})
	password = session.Credentials.Password
	w.Close()
	if session.Credentials.Password != nil || string(password) != string(make([]byte, len(password))) {
		t.Fatal("Close retained removed-session credentials")
	}
}

func TestActivityTimestampChangesOnCredentialAndDatabaseOperations(t *testing.T) {
	w := New()
	w.Open(database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root"))
	session := w.ActiveSession()
	initial := session.LastActivity
	if !w.SetCredentials(session.ID, vault.Credentials{Password: []byte("pw")}) {
		t.Fatal("SetCredentials failed")
	}
	if session.LastActivity.Before(initial) {
		t.Fatalf("activity timestamp moved backwards: %s < %s", session.LastActivity.Format(time.RFC3339Nano), initial.Format(time.RFC3339Nano))
	}
	old := session.DB
	if !w.ReplaceDatabase(session.ID, database(session.Path, "reloaded", "root")) || old.Entries != nil {
		t.Fatal("database activity did not replace and clear previous database")
	}
}

func TestPreviewSettingsDuplicateIndependently(t *testing.T) {
	w := New()
	w.Open(database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root"))
	view := w.ActiveView()
	view.NavKey = "favorites"
	view.NavFilter = "private nav filter"
	view.DetailFilter = "private detail filter"
	view.NavOffset, view.NavWidth, view.DetailWidth = 7, 31, 45
	view.Zoom = true
	view.QueryScope = "group-only"
	if !w.Duplicate() {
		t.Fatal("Duplicate failed")
	}
	duplicate := w.ActiveView()
	if duplicate.NavKey != view.NavKey || duplicate.NavFilter != view.NavFilter || duplicate.DetailFilter != view.DetailFilter ||
		duplicate.NavOffset != view.NavOffset || duplicate.NavWidth != view.NavWidth || duplicate.DetailWidth != view.DetailWidth || !duplicate.Zoom || duplicate.QueryScope != view.QueryScope {
		t.Fatalf("duplicate did not copy preview/view state: %#v", duplicate)
	}
	duplicate.NavFilter = "changed"
	if view.NavFilter != "private nav filter" {
		t.Fatalf("duplicated view state aliases original: %#v", view)
	}
}

func TestUsageAndLockCleanup(t *testing.T) {
	w := New()
	db := database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root")
	db.Entries = append(db.Entries, vault.Entry{UUID: "entry-two"})
	w.Open(db)
	session := w.ActiveSession()
	now := time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)
	if w.RecordAccess(session.ID, "missing", now) {
		t.Fatal("RecordAccess accepted a nonexistent entry")
	}
	if !w.RecordAccess(session.ID, "entry", now) || !w.RecordAccess(session.ID, "entry", now.Add(time.Minute)) {
		t.Fatal("RecordAccess failed for an existing entry")
	}
	access := session.Usage["entry"]
	if access.Count != 2 || !access.Last.Equal(now.Add(time.Minute)) {
		t.Fatalf("access stats = %#v", access)
	}
	if len(session.Usage) != 1 {
		t.Fatalf("nonexistent entry created access state: %#v", session.Usage)
	}
	view := w.ActiveView()
	view.NavFilter, view.DetailFilter = "nav secret", "detail secret"
	if !w.LockSession(session.ID) {
		t.Fatal("LockSession failed")
	}
	if len(session.Usage) != 0 || view.NavFilter != "" || view.DetailFilter != "" {
		t.Fatal("full session lock retained usage/filter state")
	}
}

func TestRefreshPreservesUsage(t *testing.T) {
	w := New()
	db := database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root")
	w.Open(db)
	session := w.ActiveSession()
	w.RecordAccess(session.ID, "entry", time.Now())
	updated := database(db.Path, "updated", "root")
	if !w.ReplaceDatabase(session.ID, updated) || session.Usage["entry"].Count != 1 {
		t.Fatal("database replacement lost session usage")
	}
}

func TestActivityCountSaturates(t *testing.T) {
	w := New()
	w.Open(database(filepath.Join(t.TempDir(), "vault.kdbx"), "vault", "root"))
	session := w.ActiveSession()
	session.Usage = map[string]Access{"entry": {Count: int(^uint(0) >> 1)}}
	if !w.RecordAccess(session.ID, "entry", time.Now()) || session.Usage["entry"].Count != int(^uint(0)>>1) {
		t.Fatal("access count overflowed")
	}
}
