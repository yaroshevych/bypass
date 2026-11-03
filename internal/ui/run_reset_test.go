package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
)

type resetPermissionBackend struct {
	failPath string
	writes   []string
}

func (*resetPermissionBackend) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	return nil, errors.New("unexpected open")
}

func (*resetPermissionBackend) ResolveField(_ context.Context, db *vault.Database, _ vault.Credentials, entry, _ string) ([]byte, error) {
	for _, item := range db.Entries {
		if item.UUID == entry {
			return []byte(item.Get("Notes")), nil
		}
	}
	return nil, errors.New("entry unavailable")
}

func (b *resetPermissionBackend) Apply(_ context.Context, db *vault.Database, _ vault.Credentials, change vault.Change, _ string) (*vault.Database, error) {
	if db.Path == b.failPath {
		return nil, errors.New("write failed")
	}
	if change.Kind != vault.DeleteEntry {
		return nil, errors.New("unexpected change")
	}
	b.writes = append(b.writes, db.Path)
	updated := db.Clone()
	for i := range updated.Entries {
		if updated.Entries[i].UUID == change.EntryUUID {
			updated.Entries[i].Recycled = true
		}
	}
	return updated, nil
}

func resetPermissionDatabase(path string) *vault.Database {
	grant := runpkg.Grant{Version: 2, Scope: "identity", Identity: "digest", Entry: "secret", Field: "Password"}
	return &vault.Database{Path: path, RootUUID: "root", Entries: []vault.Entry{
		{UUID: "permission", GroupPath: "Passwords/" + runpkg.GrantGroup, Fields: []vault.Field{{Name: "Title", Value: "Saved permission"}, {Name: "Notes", Value: grant.Encode()}}},
		{UUID: "ordinary", GroupPath: "Passwords/" + runpkg.GrantGroup, Fields: []vault.Field{{Name: "Title", Value: "Unrelated entry"}, {Name: "Notes", Value: "ordinary note"}}},
	}}
}

func TestRunResetPermissionsAcrossVaultsAndPreservesOtherEntries(t *testing.T) {
	backend := &resetPermissionBackend{}
	m := New(Options{Backend: backend})
	defer m.Close()
	m.settings.BackupDir = t.TempDir()
	m.runs = &runState{resetConfirm: true}
	for _, path := range []string{"/a", "/b"} {
		m.ws.Open(resetPermissionDatabase(path))
		m.ws.ActiveSession().ReadOnly = false
	}
	result := m.beginRunPermissionReset()().(runResetResult)
	if result.err != nil || result.count != 2 {
		t.Fatal("reset did not remove decisions from all unlocked vaults")
	}
	m.updateRunPermissionReset(result)
	for _, session := range m.ws.Sessions {
		if !session.DB.Entries[0].Recycled || session.DB.Entries[1].Recycled {
			t.Fatal("reset retained a decision or removed an unrelated entry")
		}
	}
	if m.runs.resetBusy || m.runs.resetConfirm || m.runs.permissionEpoch != 1 {
		t.Fatal("reset did not finish or invalidate prior grant checks")
	}
}

func TestRunResetPermissionsBlocksReadonlyAndReportsPartialWrites(t *testing.T) {
	backend := &resetPermissionBackend{failPath: "/b"}
	m := New(Options{Backend: backend})
	defer m.Close()
	m.settings.BackupDir = t.TempDir()
	m.runs = &runState{resetConfirm: true}
	m.ws.Open(resetPermissionDatabase("/a"))
	m.ws.ActiveSession().ReadOnly = false
	m.ws.Open(resetPermissionDatabase("/b"))
	readonly := m.ws.ActiveSession()
	readonly.ReadOnly = true
	if cmd := m.beginRunPermissionReset(); cmd != nil || len(backend.writes) != 0 {
		t.Fatal("reset started before all affected vaults were writable")
	}
	readonly.ReadOnly = false
	result := m.beginRunPermissionReset()().(runResetResult)
	if result.err == nil || result.count != 1 {
		t.Fatal("reset did not report partial failure")
	}
	m.updateRunPermissionReset(result)
	for _, session := range m.ws.Sessions {
		if session.DB.Entries[0].Recycled != (session.Path == "/a") {
			t.Fatal("partial reset left the workspace out of sync")
		}
	}
	if m.runs.resetErr == "" || !m.runs.resetConfirm {
		t.Fatal("partial failure was not shown in the confirmation")
	}
}

func TestRunResetLateResultDoesNotUnlockVault(t *testing.T) {
	m := New(Options{Backend: &resetPermissionBackend{}})
	defer m.Close()
	m.settings.BackupDir = t.TempDir()
	m.runs = &runState{resetConfirm: true}
	m.ws.Open(resetPermissionDatabase("/vault"))
	m.ws.ActiveSession().ReadOnly = false
	id := m.ws.ActiveSession().ID
	result := m.beginRunPermissionReset()().(runResetResult)
	m.ws.LockAll()
	m.cancelOperation()
	m.updateRunPermissionReset(result)
	if m.ws.Sessions[id].DB != nil {
		t.Fatal("late reset result restored a locked vault")
	}
}

func TestRunResetDiscardsPriorAutomaticApproval(t *testing.T) {
	m := New(Options{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p := &runpkg.Pending{Context: ctx}
	secrets := newSecretValues(ctx)
	value := []byte("synthetic")
	secrets.Put("field", value)
	m.runs = &runState{permissionEpoch: 1, pending: []*runpkg.Pending{p}}
	m.updateRun(runResult{pending: p, secrets: secrets, permissionEpoch: 0})
	for _, b := range value {
		if b != 0 {
			t.Fatal("stale automatic approval retained resolved bytes")
		}
	}
	if len(m.runs.pending) != 1 {
		t.Fatal("stale automatic approval completed a request after reset")
	}
}
