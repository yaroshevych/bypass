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
	m.runs = &runState{grants: map[runSessionGrant]string{{id, "agent:dev", "target", "Password"}: "dev"}}
	target := runTarget{ref: "reference", session: id, entry: "target", field: "Password", database: db}
	for _, tc := range []struct {
		identity runpkg.Identity
		field    string
		allowed  bool
	}{{runpkg.Identity{Name: "dev", Digest: "digest"}, "Password", true}, {runpkg.Identity{Name: "other", Digest: "digest"}, "Password", false}, {runpkg.Identity{}, "Password", false}, {runpkg.Identity{Name: "dev"}, "UserName", false}} {
		p := &runpkg.Pending{Context: context.Background(), Identity: tc.identity, Request: runpkg.Request{Command: []string{"/bin/sh"}}}
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

func TestNamedSessionGrantsCombineAcrossRequests(t *testing.T) {
	m := New(Options{Backend: &identityGrantBackend{}})
	defer m.Close()
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	session := m.ws.ActiveSession()
	identity := runpkg.Identity{Name: "dev-rw", Digest: "first-token"}
	first := []runTarget{
		{ref: "one", session: session.ID, entry: "one", field: "Password", database: db},
		{ref: "two", session: session.ID, entry: "two", field: "Password", database: db},
	}
	second := []runTarget{
		{ref: "three", session: session.ID, entry: "three", field: "Password", database: db},
		{ref: "four", session: session.ID, entry: "four", field: "Password", database: db},
	}
	m.runs = &runState{grants: make(map[runSessionGrant]string)}
	for _, target := range append(append([]runTarget(nil), first...), second...) {
		m.runs.grants[runSessionGrant{target.session, runSessionPrincipal(identity, []runTarget{target}), target.entry, target.field}] = identity.Name
	}
	// A rotated bearer with the same agent name can request any combination
	// drawn from the individually approved fields.
	identity.Digest = "rotated-token"
	requested := []runTarget{first[0], second[0]}
	if !runTargetsHaveSessionGrants(m.runs.grants, runSessionPrincipal(identity, requested), requested) {
		t.Fatal("combination of approved fields was not authorized")
	}
}

func TestUnnamedSessionGrantUsesExactItemCombination(t *testing.T) {
	session := "vault-session"
	targets := []runTarget{
		{session: session, entry: "one", field: "Password"},
		{session: session, entry: "two", field: "Password"},
	}
	principal := runSessionPrincipal(runpkg.Identity{}, targets)
	grants := map[runSessionGrant]string{
		{session, principal, "one", "Password"}: "",
		{session, principal, "two", "Password"}: "",
	}
	if !runTargetsHaveSessionGrants(grants, runSessionPrincipal(runpkg.Identity{}, targets), targets) {
		t.Fatal("same unnamed item combination was not recognized")
	}
	if runTargetsHaveSessionGrants(grants, runSessionPrincipal(runpkg.Identity{}, targets[:1]), targets[:1]) {
		t.Fatal("subset of unnamed item combination was authorized")
	}
}

func TestRunCheckRetriesWhenSessionApprovalArrivesWhileChecking(t *testing.T) {
	m := New(Options{Backend: &identityGrantBackend{}})
	defer m.Close()
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	session := m.ws.ActiveSession()
	grant := runSessionGrant{session.ID, "agent:dev", "target", "Password"}
	m.runs = &runState{grants: make(map[runSessionGrant]string)}
	p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev", Digest: "digest"}}
	targets := []runTarget{{ref: "reference", session: session.ID, entry: "target", field: "Password", database: db}}

	// Model the initial async check finishing just after another request's
	// session approval was recorded.
	initial := m.runGrantJob(p, targets, "check")().(runResult)
	if !initial.needsApproval {
		t.Fatal("initial check should have needed approval")
	}
	m.runs.grants[grant] = "dev"

	cmd, handled := m.updateRun(initial)
	if !handled || cmd == nil {
		t.Fatal("late session approval did not trigger a fresh check")
	}
	result := cmd().(runResult)
	if result.needsApproval || result.err != nil {
		t.Fatalf("fresh check did not use session approval: needsApproval=%v err=%v", result.needsApproval, result.err)
	}
	values := result.secrets.Take()
	if !bytes.Equal(values["reference"], []byte("synthetic")) {
		t.Fatal("authorized secret was not resolved")
	}
	clearRunValues(values)
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
