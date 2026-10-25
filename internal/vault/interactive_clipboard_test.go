package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNativePasswordCopyUsesCurrentEntryAndLeavesClipboardUntouchedOnClose(t *testing.T) {
	path, creds, seed := interactiveFixture(t)
	cli := NewCLI("")
	for _, title := range []string{"First", "Second"} {
		updated, err := cli.Apply(context.Background(), seed, creds, Change{
			Kind: AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": title},
			PasswordChanged: true, Password: []byte("disposable-" + title),
		}, filepath.Join(t.TempDir(), "backups"))
		if err != nil {
			t.Fatal(err)
		}
		seed.Clear()
		seed = updated
	}
	defer seed.Clear()

	// Native clipboard programs write to a private fixture, never the desktop.
	directory := t.TempDir()
	clipboard := filepath.Join(directory, "clipboard")
	t.Setenv("BYPASS_NATIVE_CLIPBOARD_TEST", clipboard)
	for _, program := range []string{"pbcopy", "wl-copy", "xclip"} {
		if err := os.WriteFile(filepath.Join(directory, program), []byte("#!/bin/sh\ncat > \"$BYPASS_NATIVE_CLIPBOARD_TEST\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	for _, title := range []string{"First", "Second", "First"} {
		var entryID string
		for _, entry := range db.Entries {
			if entry.Get("Password") != "" {
				t.Fatal("session cached a plaintext password")
			}
			if entry.Get("Title") == title {
				entryID = entry.UUID
			}
		}
		if err := backend.CopyPassword(context.Background(), db, entryID); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(clipboard)
		if err != nil || string(data) != "disposable-"+title {
			t.Fatal("native clipboard did not contain the selected entry password")
		}
	}
	db.Clear()
	data, err := os.ReadFile(clipboard)
	if err != nil || string(data) != "disposable-First" {
		t.Fatal("closing the session changed the clipboard")
	}
}
