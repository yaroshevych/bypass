//go:build integration

package evals

import (
	"bypass/internal/vaulttest"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestKeyFileOnlyCreationUnlockAndPasswordAdditionTmux(t *testing.T) {
	t.Skip("non-password authentication UI is hidden in beta; retain this test for re-enablement")
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	keyFile := filepath.Join(e.root, "only.key")
	const key = "synthetic key-only creation fixture material"
	e.registerSecret(key)
	if err := os.WriteFile(keyFile, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	e.startWith(backend)
	e.mustShow("New Vault")
	e.key("N")
	e.mustShow("New .kdbx path")
	e.typeText(e.realDB)
	e.key("Tab")
	e.key("Tab")
	e.key("Tab")
	e.typeText(keyFile)
	e.key("Tab")
	e.typeText("100")
	e.key("C-s")
	e.mustShowWrite("Vault created and unlocked.", "No entries")
	e.key("C-l")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("Unlock Vault")
	e.key("Tab")
	e.typeText(keyFile)
	e.key("Tab")
	e.key("Tab")
	e.key("Space")
	e.mustShow("disabled")
	e.key("Enter")
	e.mustShow("No entries")
	cli := vaulttest.NewCLI(backend)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := cli.Open(ctx, e.realDB, vault.Credentials{NoPassword: true, KeyFile: keyFile})
	if err != nil {
		t.Fatal("independent key-only reopen failed:", err)
	}
	db.Clear()
	missing, err := cli.Open(ctx, e.realDB, vault.Credentials{NoPassword: true})
	if missing != nil {
		missing.Clear()
	}
	if !errors.Is(err, vault.ErrUnlock) {
		t.Fatal("key-only vault opened without its key file")
	}
	e.key("C-p")
	e.typeText("Change Vault Credentials")
	e.key("Enter")
	e.mustShow("New password")
	e.typeText(fixturePassword)
	e.key("Tab")
	e.typeText(fixturePassword)
	e.key("C-s")
	e.mustShow("Change the credentials for this vault?")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("Credentials changed and verified.")
	withPassword := vault.Credentials{Password: []byte(fixturePassword), KeyFile: keyFile}
	defer withPassword.Clear()
	db, err = cli.Open(ctx, e.realDB, withPassword)
	if err != nil {
		t.Fatal("password addition to key-only vault failed native verification:", err)
	}
	db.Clear()
	old, err := cli.Open(ctx, e.realDB, vault.Credentials{NoPassword: true, KeyFile: keyFile})
	if old != nil {
		old.Clear()
	}
	if !errors.Is(err, vault.ErrUnlock) {
		t.Fatal("password addition left the old factor combination valid")
	}
	e.key("q")
	e.waitForCleanQuit()
}
