//go:build integration

package evals

import (
	"bypass/internal/vaulttest"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestPINMasksNativeSaveAndFullLockCancelsNextSaveTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)
	started, release := filepath.Join(e.root, "edit-started"), filepath.Join(e.root, "edit-release")
	wrapper := filepath.Join(e.root, "controlled-edit-cli")
	script := "#!/bin/sh\nset -eu\numask 077\nif [ \"$1\" = edit ]; then\n  : > " + shellQuote(started) + "\n  while [ ! -f " + shellQuote(release) + " ]; do sleep 0.02; done\nfi\nexec " + shellQuote(backend) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	e.startWith(wrapper, e.realDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.key("c")
	e.mustClipboardEqual([]byte(fixtureEntryPassword))
	e.key("C-p")
	e.typeText("Change PIN")
	e.key("Enter")
	e.mustShow("Set Session PIN")
	e.typeText("2468")
	e.key("Enter")
	e.mustShow("Confirm Session PIN")
	e.typeText("2468")
	e.key("Enter")
	e.mustShow("Session PIN set")
	initial := vaultFileHash(t, e.realDB)
	e.key("e")
	e.mustShow("Edit Entry")
	e.typeText(" Protected")
	e.key("C-s")
	e.waitFor("native save reached controlled boundary", func() bool { _, err := os.Stat(started); return err == nil })
	e.key("C-k")
	e.mustShow("Screen protected")
	e.mustNotShow("Seed Entry")
	e.mustNotShow("real-cli.kdbx")
	e.mustClipboardEqual(nil)
	if vaultFileHash(t, e.realDB) != initial {
		t.Fatal("controlled save committed before release")
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.waitForWithin(20*time.Second, "authenticated save completed behind PIN", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		creds := vault.Credentials{Password: []byte(fixturePassword)}
		defer creds.Clear()
		db, err := vaulttest.NewCLI(backend).Open(ctx, e.realDB, creds)
		if err != nil {
			return false
		}
		defer db.Clear()
		for _, entry := range db.Entries {
			if entry.Get("Title") == "Seed Entry Protected" && entry.Get("Password") == fixtureEntryPassword {
				return true
			}
		}
		return false
	})
	e.mustShow("Screen protected")
	e.typeText("2468")
	e.key("Enter")
	e.mustShow("Seed Entry Protected")
	e.mustNotShow(fixtureEntryPassword)
	committed := vaultFileHash(t, e.realDB)
	if err := os.Remove(release); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(started); err != nil {
		t.Fatal(err)
	}
	e.key("e")
	e.mustShow("Edit Entry")
	e.typeText(" Discarded")
	e.key("C-s")
	e.waitFor("second native save reached controlled boundary", func() bool { _, err := os.Stat(started); return err == nil })
	e.key("C-l")
	e.mustShow("Vault locked")
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if vaultFileHash(t, e.realDB) != committed {
		t.Fatal("full lock allowed the stalled save to change ciphertext")
	}
	assertNativeEntry(t, backend, e.realDB, "Seed Entry Protected", fixtureEntryPassword)
	e.mustClipboardEqual(nil)
	e.key("q")
	e.waitForCleanQuit()
}
