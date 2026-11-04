package ui

import (
	"context"
	"testing"

	runpkg "bypass/internal/run"
)

func TestResetRunSessionPermissions(t *testing.T) {
	m := New(Options{})
	key := runSessionGrant{session: "vault", identity: "agent", entry: "entry", field: "Password"}
	m.runs = &runState{grants: map[runSessionGrant]string{key: "agent"}}
	m.beginRunPermissionReset()
	if len(m.runs.grants) != 0 || m.runs.permissionEpoch != 1 {
		t.Fatal("session reset did not clear grants")
	}
}

func TestRunResetDiscardsPriorAutomaticApproval(t *testing.T) {
	m := New(Options{})
	p := &runpkg.Pending{Context: context.Background()}
	secrets := newSecretValues(context.Background())
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
		t.Fatal("stale automatic approval completed a request")
	}
}
