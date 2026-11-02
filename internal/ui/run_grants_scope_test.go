package ui

import (
	"bytes"
	"context"
	"errors"
	"testing"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
)

type identityGrantBackend struct{ grant runpkg.Grant }

func (*identityGrantBackend) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	return nil, errors.New("unexpected open")
}

func (b *identityGrantBackend) ResolveField(_ context.Context, _ *vault.Database, _ vault.Credentials, entry, field string) ([]byte, error) {
	if entry == "permission" && field == "Notes" {
		return []byte(b.grant.Encode()), nil
	}
	if entry == "target" && field == "Password" {
		return []byte("synthetic"), nil
	}
	return nil, errors.New("unexpected field")
}

func TestRunIdentityGrantAuthorizesFieldsForInterpreters(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity string
		legacy   bool
		allowed  bool
	}{{"identity", "digest", false, true}, {"other identity", "other", false, false}, {"anonymous", "", false, false}, {"legacy interpreter", "digest", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			grant := runpkg.Grant{Version: 2, Scope: "identity", Identity: "digest", Entry: "target", Name: "Target", Group: "Passwords", Field: "Password"}
			if tc.legacy {
				grant.Version, grant.Scope, grant.Executable = 1, "", "/bin/sh"
			}
			backend := &identityGrantBackend{grant: grant}
			m := New(Options{Backend: backend})
			defer m.Close()
			db := &vault.Database{Path: "/vault", RootUUID: "root", Entries: []vault.Entry{
				{UUID: "target", GroupPath: "Passwords", Fields: []vault.Field{{Name: "Title", Value: "Target"}, {Name: "Password", Protected: true}}},
				{UUID: "permission", GroupPath: "Passwords/" + runpkg.GrantGroup},
			}}
			m.ws.Open(db)
			id := m.ws.ActiveSession().ID
			p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev-rw", Digest: tc.identity}, Request: runpkg.Request{Command: []string{"/bin/sh"}, Directory: "/project"}}
			result := m.runGrantJob(p, []runTarget{{ref: "reference", session: id, entry: "target", field: "Password", database: db}}, "check")().(runResult)
			if result.err != nil || result.needsApproval == tc.allowed {
				t.Fatal("saved approval did not respect its identity and recipient scope")
			}
			values := result.secrets.Take()
			defer clearRunValues(values)
			if tc.allowed && !bytes.Equal(values["reference"], []byte("synthetic")) {
				t.Fatal("authorized identity did not receive its field")
			}
		})
	}
}
