//go:build integration

package evals

import (
	"bypass/internal/vault"
	"bypass/internal/vaulttest"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestInteractiveBackendTmuxBrowseRevealAndLock(t *testing.T) {
	t.Parallel()
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			e := newEval(t)
			defer e.close()
			e.secrets = append(e.secrets, fixtureEntryPassword)
			createRealVault(e)
			e.writeConfig(map[string]any{"appearance": appearance})
			e.startWith("--read-only", e.realDB)
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShow("Seed Entry", "read only", "seed-user", "https://seed.test", "synthetic seed note")
			e.key("/")
			e.typeText("seed-user")
			e.key("Enter")
			e.mustShow("Seed Entry")
			e.mustNotShow("No matches")
			e.key("/")
			e.key("C-u")
			e.key("Enter")
			e.mustNotShow(fixtureEntryPassword)
			e.key("t")
			e.key("C-w")
			e.mustShow("Seed Entry")
			e.key("c")
			e.mustClipboardEqual([]byte(fixtureEntryPassword))
			// Outside the details pane, reveal/copy focus defaults to Password.
			e.key("v")
			e.mustShow(fixtureEntryPassword)
			e.key("v")
			e.waitFor("password remasked", func() bool { return !strings.Contains(e.capture(), fixtureEntryPassword) })
			e.mustShow("Seed Entry")
			e.mustNotShow(fixtureEntryPassword)
			e.key("n")
			e.mustShow("read only")
			e.key("L")
			e.mustShow("Vault locked")
			e.mustNotShow(fixtureEntryPassword)
			e.key("Enter")
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShow("Seed Entry")
			e.key("q")
			e.waitForCleanQuit()
		})
	}
}

func TestInteractiveBackendTmuxEditCreateAndReopen(t *testing.T) {
	t.Parallel()
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			e := newEval(t)
			defer e.close()
			const changedPassword = "interactive-updated-password"
			const createdPassword = "interactive-created-password"
			e.registerSecret(changedPassword)
			e.registerSecret(createdPassword)
			e.registerSecret(fixtureEntryPassword)
			createRealVault(e)
			e.writeConfig(map[string]any{"appearance": appearance})
			originalHash := vaultFileHash(t, e.realDB)
			e.startWith(e.realDB)
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShow("Seed Entry")
			e.key("Right")
			e.key("e")
			e.mustShow("Generate Password", "seed-user")
			e.key("C-u")
			e.typeText("Edited Seed")
			e.key("Tab")
			e.key("C-u")
			e.typeText("edited-user")
			e.key("Tab")
			e.key("C-u")
			e.typeText(changedPassword)
			e.key("Tab")
			e.key("C-u")
			e.typeText("https://edited.test")
			e.key("Tab")
			e.key("C-u")
			e.typeText("first edited line")
			e.key("Enter")
			e.typeText("second edited line")
			e.key("C-s")
			e.waitFor("interactive edit saved", func() bool {
				s := e.capture()
				return strings.Contains(s, "saved · backup created") && strings.Contains(s, "Edited Seed") && !strings.Contains(s, "Generate Password")
			})
			e.mustNotShow(changedPassword)
			assertInteractiveFields(t, e.realDB, "Edited Seed", map[string]string{"UserName": "edited-user", "Password": changedPassword, "URL": "https://edited.test", "Notes": "first edited line\nsecond edited line"})
			if vaultFileHash(t, e.realDB) == originalHash {
				t.Fatal("saved edit did not change ciphertext")
			}
			e.key("n")
			e.mustShow("New Entry")
			e.typeText("Created Interactively")
			e.key("C-l")
			e.mustShow("New Entry", "Created Interactively")
			e.mustNotShow("Vault locked")
			e.key("Tab")
			e.typeText("created-user")
			e.key("Tab")
			e.typeText(createdPassword)
			e.key("Tab")
			e.typeText("https://created.test")
			e.key("Tab")
			e.typeText("created notes")
			e.key("C-s")
			e.waitFor("interactive creation saved", func() bool {
				s := e.capture()
				return strings.Contains(s, "saved · backup created") && strings.Contains(s, "Created Interactively") && !strings.Contains(s, "Generate Password")
			})
			e.mustNotShow(createdPassword)
			assertInteractiveFields(t, e.realDB, "Created Interactively", map[string]string{"UserName": "created-user", "Password": createdPassword, "Notes": "created notes"})
			e.key("J")
			e.key("p")
			e.key("c")
			e.mustClipboardEqual([]byte(createdPassword))
			e.key("L")
			e.mustShow("Vault locked")
			e.mustClipboardEqual([]byte(createdPassword))
			e.key("Enter")
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShow("Created Interactively", "Edited Seed")
			e.key("q")
			e.waitForCleanQuit()
			assertInteractiveFields(t, e.realDB, "Edited Seed", map[string]string{"Password": changedPassword})
			backups, err := filepath.Glob(filepath.Join(e.root, "xdg-state", "bypass", "backups", "*.kdbx"))
			// Fixture setup writes to a separate directory; the app creates one
			// backup for deletion and one for credential rotation.
			if err != nil || len(backups) != 2 {
				t.Fatal("encrypted backups missing")
			}
			foundOriginal := false
			for _, backup := range backups {
				if vaultFileHash(t, backup) == originalHash {
					foundOriginal = true
				}
			}
			if !foundOriginal {
				t.Fatal("pre-edit ciphertext backup missing")
			}
		})
	}
}

func assertInteractiveFields(t *testing.T, path, title string, want map[string]string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := vaulttest.NewBackend().Open(ctx, path, vault.Credentials{Password: []byte(fixturePassword)})
	if err != nil {
		t.Fatal("independent saved-vault reopen failed", err)
	}
	defer db.Clear()
	for _, entry := range db.Entries {
		if entry.Get("Title") != title {
			continue
		}
		for field, value := range want {
			if entry.Get(field) != value {
				t.Fatalf("independent readback mismatch for %s", field)
			}
		}
		return
	}
	t.Fatal("saved entry missing on independent reopen")
}

func TestInteractiveBackendTmuxInfoDeletePasswordAndSearchShortcut(t *testing.T) {
	t.Parallel()
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			e := newEval(t)
			defer e.close()
			const newPassword = "interactive-new-master-password"
			e.registerSecret(newPassword)
			createRealVault(e)
			e.writeConfig(map[string]any{"appearance": appearance})
			e.startWith(e.realDB)
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShowWithin(10*time.Second, "Seed Entry")
			e.key("C-p")
			e.mustShow("COMMANDS")
			e.typeText("Search")
			e.waitFor("Search shortcut in palette", func() bool { return regexp.MustCompile(`Search\s+/`).MatchString(e.capture()) })
			e.key("Escape")
			e.waitFor("dialog closed", func() bool {
				s := e.capture()
				return !strings.Contains(s, "COMMANDS") && !strings.Contains(s, "DATABASE INFO")
			})
			e.key("C-p")
			e.mustShow("COMMANDS")
			e.typeText("Database Info")
			e.key("Enter")
			e.mustShow("DATABASE INFO", "UUID:", "KDF:", "Entries: 1")
			e.key("Escape")
			e.waitFor("dialog closed", func() bool {
				s := e.capture()
				return !strings.Contains(s, "COMMANDS") && !strings.Contains(s, "DATABASE INFO")
			})
			e.key("J")
			e.key("p")
			e.key("DC")
			e.mustShow("DELETE", "Cancel")
			e.key("Tab")
			e.key("Enter")
			e.waitFor("native deletion finished", func() bool {
				s := e.capture()
				return strings.Contains(s, "saved · backup created") && !strings.Contains(s, "KeePassXC moves")
			})
			old := vault.Credentials{Password: []byte(fixturePassword)}
			db, err := vaulttest.NewBackend().Open(context.Background(), e.realDB, old)
			if err != nil {
				t.Fatal(err)
			}
			if len(db.Entries) != 1 || !db.Entries[0].Recycled {
				db.Clear()
				t.Fatal("UI delete did not use native recycle behavior")
			}
			db.Clear()
			e.key("C-p")
			e.mustShow("COMMANDS")
			e.typeText("Change Password")
			e.key("Enter")
			e.mustShow("CHANGE PASSWORD", "New password")
			e.typeText(newPassword)
			e.key("Tab")
			e.typeText(newPassword)
			e.key("C-s")
			e.mustShow("Change the credentials for this vault?")
			e.key("Tab")
			e.key("Enter")
			e.mustShow("credentials changed · backup created")
			newCreds := vault.Credentials{Password: []byte(newPassword)}
			db, err = vaulttest.NewBackend().Open(context.Background(), e.realDB, newCreds)
			if err != nil {
				t.Fatal("new password cannot reopen UI-saved vault", err)
			}
			db.Clear()
			if opened, err := vaulttest.NewBackend().Open(context.Background(), e.realDB, old); err == nil {
				opened.Clear()
				t.Fatal("old password still opens vault")
			}
			e.key("L")
			e.mustShow("Vault locked")
			e.key("Enter")
			e.mustShow("UNLOCK VAULT")
			e.typeText(newPassword)
			e.key("Enter")
			e.waitFor("vault unlocked with changed password", func() bool {
				return !strings.Contains(e.capture(), "UNLOCK VAULT") && strings.Contains(e.capture(), "seed-vault.kdbx")
			})
			e.key("q")
			e.waitForCleanQuit()
			backups, err := filepath.Glob(filepath.Join(e.root, "xdg-state", "bypass", "backups", "*.kdbx"))
			if err != nil || len(backups) != 2 {
				t.Fatalf("expected delete and password-change backups, got %d (%v): %v", len(backups), backups, err)
			}
			for _, path := range backups {
				db, err := vaulttest.NewBackend().Open(context.Background(), path, old)
				if err != nil {
					t.Fatal("old password cannot open backup", err)
				}
				db.Clear()
			}
		})
	}
}
