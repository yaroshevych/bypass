package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIPCExchangeAndExclusiveServer(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "bypass-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	root, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	// macOS Unix socket addresses are short; fixture location must fit.
	if len(root) > 65 {
		t.Fatal("IPC fixture path too long")
	}
	path := filepath.Join(root, "run.sock")
	server, err := Listen(path, func(p *Pending) { p.Reply(Response{Values: map[string]string{"ref": "synthetic"}}) })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if second, err := Listen(path, func(*Pending) {}); err == nil {
		second.Close()
		t.Fatal("second server displaced active server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := Exchange(ctx, path, Request{Command: []string{"test"}, References: []string{"ref"}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Values["ref"] != "synthetic" {
		t.Fatal("response lost")
	}
}
func TestGrantScopeAndRename(t *testing.T) {
	grant := Grant{Version: 1, Entry: "uuid", Name: "name", Group: "group", Field: "Password", Executable: "/app", Directory: "/project"}
	decoded, ok := DecodeGrant(grant.Encode())
	if !ok || !decoded.Allows(grant) {
		t.Fatal("grant did not round trip")
	}
	for _, change := range []func(*Grant){func(g *Grant) { g.Name = "new" }, func(g *Grant) { g.Group = "other" }, func(g *Grant) { g.Directory = "/other" }, func(g *Grant) { g.Field = "Notes" }, func(g *Grant) { g.Executable = "/other" }, func(g *Grant) { g.Entry = "new" }} {
		want := grant
		change(&want)
		if grant.Allows(want) {
			t.Fatal("grant escaped scope")
		}
	}
	global := grant
	global.Directory = ""
	if !global.Allows(grant) {
		t.Fatal("global grant did not apply")
	}
}
