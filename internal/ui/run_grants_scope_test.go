package ui

import (
	"bytes"
	"context"
	"errors"
	"testing"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
)

type identityGrantBackend struct{}

func (*identityGrantBackend) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	return nil, errors.New("unexpected open")
}
func (*identityGrantBackend) ResolveField(_ context.Context, _ *vault.Database, _ vault.Credentials, entry, field string) ([]byte, error) {
	if entry == "target" && field == "Password" {
		return []byte("synthetic"), nil
	}
	return nil, errors.New("unexpected field")
}

func TestRunSessionGrantScopeAndVaultLock(t *testing.T) {
	m := New(Options{Backend: &identityGrantBackend{}})
	defer m.Close()
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	id := m.ws.ActiveSession().ID
	m.runs = &runState{grants: map[runSessionGrant]string{{id, "digest", "target", "Password"}: "dev"}}
	target := runTarget{ref: "reference", session: id, entry: "target", field: "Password", database: db}
	for _, tc := range []struct {
		identity string
		field    string
		allowed  bool
	}{{"digest", "Password", true}, {"other", "Password", false}, {"", "Password", false}, {"digest", "UserName", false}} {
		p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev", Digest: tc.identity}, Request: runpkg.Request{Command: []string{"/bin/sh"}}}
		requested := target
		requested.field = tc.field
		result := m.runGrantJob(p, []runTarget{requested}, "check")().(runResult)
		if result.err != nil || result.needsApproval == tc.allowed {
			t.Fatal("session approval scope mismatch")
		}
		values := result.secrets.Take()
		if tc.allowed && !bytes.Equal(values["reference"], []byte("synthetic")) {
			t.Fatal("authorized field missing")
		}
		clearRunValues(values)
	}
	m.lockSession(id)
	if len(m.runs.grants) != 0 {
		t.Fatal("locking retained session permissions")
	}
}

func TestOldSavedPermissionsDoNotAuthorizeRun(t *testing.T) {
	m := New(Options{Backend: &identityGrantBackend{}})
	db := &vault.Database{Path: "/vault", RootUUID: "root", Entries: []vault.Entry{{UUID: "permission", GroupPath: "Passwords/" + runpkg.GrantGroup}}}
	m.ws.Open(db)
	m.runs = &runState{}
	p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Digest: "digest"}}
	result := m.runGrantJob(p, []runTarget{{session: m.ws.ActiveSession().ID, entry: "target", field: "Password", database: db}}, "check")().(runResult)
	if !result.needsApproval {
		t.Fatal("old persisted permission authorized a request")
	}
}

func TestLockClearsOnlyThatVaultsSessionPermissions(t *testing.T) {
	m := New(Options{})
	m.ws.Open(&vault.Database{Path: "/one", RootUUID: "root"})
	first := m.ws.ActiveSession().ID
	m.ws.Open(&vault.Database{Path: "/two", RootUUID: "root"})
	second := m.ws.ActiveSession().ID
	a := runSessionGrant{first, "digest", "entry", "Password"}
	b := runSessionGrant{second, "digest", "entry", "Password"}
	m.runs = &runState{grants: map[runSessionGrant]string{a: "agent", b: "agent"}}
	m.lockSession(first)
	if _, ok := m.runs.grants[a]; ok {
		t.Fatal("locked vault retained permission")
	}
	if _, ok := m.runs.grants[b]; !ok {
		t.Fatal("locking another vault cleared permission")
	}
	m.Close()
	if len(m.runs.grants) != 0 {
		t.Fatal("close retained session permissions")
	}
}
