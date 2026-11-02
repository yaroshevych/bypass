package run

import "testing"

func TestIdentityFieldGrantAcrossRecipients(t *testing.T) {
	grant := Grant{Version: 2, Scope: "identity", Identity: "agent-digest", Agent: "dev-rw", Entry: "entry", Name: "Database", Group: "Passwords/Dev", Field: "Password"}
	decoded, ok := DecodeGrant(grant.Encode())
	if !ok {
		t.Fatal("identity grant did not round trip")
	}
	want := Grant{Version: 1, Identity: grant.Identity, Entry: grant.Entry, Name: grant.Name, Group: grant.Group, Field: grant.Field, Executable: "/bin/sh", Directory: "/different-project"}
	if !decoded.Allows(want) {
		t.Fatal("identity grant was restricted to a recipient or directory")
	}
	for _, change := range []func(*Grant){
		func(g *Grant) { g.Identity = "other-agent" },
		func(g *Grant) { g.Identity = "" },
		func(g *Grant) { g.Entry = "other-entry" },
		func(g *Grant) { g.Field = "Notes" },
		func(g *Grant) { g.Name = "Renamed" },
		func(g *Grant) { g.Group = "Passwords/Prod" },
	} {
		other := want
		change(&other)
		if decoded.Allows(other) {
			t.Fatal("identity grant exceeded its agent or field scope")
		}
	}
}

func TestMalformedIdentityGrantsFailClosed(t *testing.T) {
	base := Grant{Version: 2, Scope: "identity", Identity: "agent-digest", Entry: "entry", Field: "Password"}
	for _, change := range []func(*Grant){
		func(g *Grant) { g.Identity = "" },
		func(g *Grant) { g.Scope = "unknown" },
		func(g *Grant) { g.Version = 1 },
		func(g *Grant) { g.Executable = "/command" },
		func(g *Grant) { g.Directory = "/project" },
	} {
		malformed := base
		change(&malformed)
		if _, ok := DecodeGrant(malformed.Encode()); ok || malformed.Allows(base) {
			t.Fatal("malformed identity grant was accepted")
		}
	}
}
