//go:build integration

package evals

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestMilestone4VaultLifecycleTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals use Unix process and shell behavior")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	e.startWith(backend)
	e.mustShow("Bypass", "Open Vault", "New Vault")

	// Create a synthetic vault from the welcome screen, with a modest KDF target.
	e.key("N")
	e.mustShow("New Vault", "New .kdbx path")
	e.typeText(e.realDB)
	e.key("Tab")
	e.typeText(fixturePassword)
	e.key("Tab")
	e.typeText(fixturePassword)
	e.key("Tab") // Existing key file is optional.
	e.key("Tab")
	e.typeText("100")
	e.key("C-s")
	e.mustShowWrite("Vault created and unlocked.")
	e.mustShow("No entries")

	// Seed the database through the application and verify metadata through both
	// native lifecycle commands.
	e.key("n")
	e.mustShow("New Entry")
	e.typeText("Lifecycle Seed")
	e.key("Tab")
	e.typeText("lifecycle-user")
	e.key("Tab")
	e.typeText(secretSentinel)
	e.key("Tab")
	e.typeText("https://lifecycle.test")
	e.key("Tab")
	e.typeText("synthetic lifecycle note")
	e.key("C-s")
	e.mustShowWrite("Lifecycle Seed", "Saved. Encrypted backup created.")
	e.mustNotShow(secretSentinel)

	e.key("C-p")
	e.typeText("Database Info")
	e.key("Enter")
	e.mustShowWrite("Database Info", "Cipher:", "KDF:")
	e.key("q")
	e.key("C-p")
	e.typeText("Database Settings")
	e.key("Enter")
	e.mustShowWrite("Database Settings", "Cipher:", "KDF:", "read only in keepassxc-cli")
	e.key("Escape")

	// Cancel is the default selection in the credential confirmation dialog.
	e.key("C-p")
	e.typeText("Change Vault Credentials")
	e.key("Enter")
	e.mustShow("Change Vault Credentials", "New password", "Confirm password")
	e.typeText(fixtureEntryPassword)
	e.key("Tab")
	e.typeText(fixtureEntryPassword)
	e.key("C-s")
	e.mustShow("Change the credentials for this vault?", "Cancel", "Change Credentials")
	e.key("Enter")
	e.mustShow("Change Vault Credentials", "New password")
	e.key("C-s")
	e.mustShow("Change the credentials for this vault?")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("Credentials changed and verified. Encrypted backup created.")
	if strings.Contains(e.capture(), secretSentinel) {
		e.failWithScreen("entry secret appeared after credential rotation", e.capture())
	}

	// Full-lock, prove the old credential fails, then unlock with the new one.
	e.key("C-l")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("Unlock Vault", "Password")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Could not unlock")
	e.typeText(fixtureEntryPassword)
	e.key("Enter")
	e.mustShow("Lifecycle Seed")
	e.mustNotShow(secretSentinel)

	// Verify the saved entry using a fresh native CLI session and check that the
	// old-credential encrypted backup remains available.
	cli := vault.NewCLI(backend)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixtureEntryPassword)})
	if err != nil {
		t.Fatalf("independent CLI open with new credentials failed: %v", err)
	}
	defer db.Clear()
	found := false
	for _, entry := range db.Entries {
		if entry.Get("Title") == "Lifecycle Seed" && entry.Get("UserName") == "lifecycle-user" {
			found = true
		}
	}
	if !found {
		t.Fatal("independent CLI open did not retain the seeded entry")
	}
	backupDir := filepath.Join(e.root, "xdg-state", "bypass", "backups")
	backups, err := os.ReadDir(backupDir)
	if err != nil || len(backups) < 2 {
		t.Fatalf("expected encrypted write and credential backups, got %d, %v", len(backups), err)
	}
	oldCredentialsOpenBackup := false
	for _, backup := range backups {
		if !backup.Type().IsRegular() {
			continue
		}
		backupDB, openErr := cli.Open(ctx, filepath.Join(backupDir, backup.Name()), vault.Credentials{Password: []byte(fixturePassword)})
		if openErr != nil {
			continue
		}
		for _, entry := range backupDB.Entries {
			if entry.Get("Title") == "Lifecycle Seed" {
				oldCredentialsOpenBackup = true
			}
		}
		backupDB.Clear()
	}
	if !oldCredentialsOpenBackup {
		t.Fatal("no encrypted backup retained the old credential and seeded entry")
	}
	e.key("q")
	e.waitForCleanQuit()
}
