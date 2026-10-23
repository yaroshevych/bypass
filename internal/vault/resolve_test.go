package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFieldUsesNativeShowAndPreservesValue(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	dbPath := filepath.Join(dir, "vault.kdbx")
	argCapture := filepath.Join(dir, "args.txt")
	if err := os.WriteFile(dbPath, []byte("encrypted fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	canonicalPath, err := canonicalPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	entryUUID := "02020202-0202-0202-0202-020202020202"
	script := `#!/bin/sh
printf '<%s>\n' "$@" > "` + argCapture + `"
[ "$#" -eq 7 ] && [ "$1" = show ] && [ "$2" = -q ] && [ "$3" = --attributes=Notes ] || exit 10
[ "$4" = -k ] && [ "$5" = test-keyfile ] && [ "$6" = "` + canonicalPath + `" ] && [ "$7" = /group/entry ] || exit 11
IFS= read -r master || exit 12
[ "$master" = master-secret ] || exit 13
printf '  first line\nsecond line  \n'
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	revision, err := Revision(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db := &Database{Path: dbPath, Revision: revision,
		Groups:  []Group{{UUID: "01010101-0101-0101-0101-010101010101", Name: "Root"}, {UUID: "03030303-0303-0303-0303-030303030303", ParentUUID: "01010101-0101-0101-0101-010101010101", Name: "group"}},
		Entries: []Entry{{UUID: entryUUID, GroupUUID: "03030303-0303-0303-0303-030303030303", Fields: []Field{{Name: "Title", Value: "entry"}, {Name: "Notes", Value: "raw"}}}},
	}
	got, err := NewCLI(bin).ResolveField(context.Background(), db, Credentials{Password: []byte("master-secret"), KeyFile: "test-keyfile"}, entryUUID, "Notes")
	if err != nil {
		args, _ := os.ReadFile(argCapture)
		t.Fatalf("ResolveField failed: %v args=%s", err, args)
	}
	defer zero(got)
	if !bytes.Equal(got, []byte("  first line\nsecond line  ")) {
		t.Fatalf("ResolveField did not preserve whitespace/newlines: bytes=%d", len(got))
	}
}

func TestResolveFieldDetectsExternalChange(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	dbPath := filepath.Join(dir, "vault.kdbx")
	if err := os.WriteFile(dbPath, []byte("encrypted fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	entryUUID := "02020202-0202-0202-0202-020202020202"
	script := `#!/bin/sh
printf x >> "$5"
printf 'value\n'
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	revision, err := Revision(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db := &Database{Path: dbPath, Revision: revision,
		Groups:  []Group{{UUID: "01010101-0101-0101-0101-010101010101", Name: "Root"}},
		Entries: []Entry{{UUID: entryUUID, GroupUUID: "01010101-0101-0101-0101-010101010101", Fields: []Field{{Name: "Title", Value: "entry"}, {Name: "Password", Value: "raw"}}}},
	}
	if _, err := NewCLI(bin).ResolveField(context.Background(), db, Credentials{NoPassword: true}, entryUUID, "Password"); !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("expected external change sentinel, got %v", err)
	}
}
