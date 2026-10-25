//go:build integration

package evals

import (
	"bypass/internal/vaulttest"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

const secondVaultPassword = "workspace-second-vault-password"

func TestWorkspaceNativeViewsDraftsAndSessionLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	e.registerSecret(secondVaultPassword)

	firstDB := filepath.Join(e.root, "workspace-first.kdbx")
	secondDB := filepath.Join(e.root, "workspace-second.kdbx")
	createWorkspaceVault(e, backend, firstDB, fixturePassword, "First Native Entry", "first-user", fixtureEntryPassword)
	createWorkspaceVault(e, backend, secondDB, secondVaultPassword, "Second Native Entry", "second-user", fixtureWrongPassword)

	e.startWith(backend)
	e.mustShow("bypass", "VAULTS", "ctrl+o")
	openWorkspaceVault(e, firstDB, fixturePassword)
	e.mustShow("First Native Entry")
	e.mustNotShow(secretSentinel)
	e.mustNotShow(fixtureEntryPassword)

	// Exercise a representative browse and search path in each supported
	// responsive layout. The same native entry remains reachable throughout.
	for _, size := range []struct{ width, height int }{{120, 35}, {90, 25}, {60, 20}, {48, 14}} {
		e.resize(size.width, size.height)
		e.mustDimensions(size.width, size.height)
		e.mustShow("First")
		e.key("/")
		e.key("C-u")
		e.typeText("First Native Entry")
		e.key("Enter")
		e.waitFor("responsive search to finish", func() bool {
			screen := e.capture()
			return strings.Contains(screen, "First") && !strings.Contains(screen, "enter apply")
		})
		e.key("/")
		e.key("C-u")
		e.typeText("First Native Entry")
		e.key("Enter")
		e.mustShow("First")
	}
	e.resize(120, 35)
	e.mustDimensions(120, 35)

	// Each duplicate view retains its own filter while both views share the
	// same underlying native database and observe saved edits.
	e.key("/")
	e.key("C-u")
	e.typeText("First")
	e.key("Enter")
	e.waitFor("first view search to finish", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "First Native Entry") && !strings.Contains(screen, "enter apply")
	})
	e.key("t")
	e.key("/")
	e.key("C-u")
	e.typeText("NoSuchEntry")
	e.key("Enter")
	e.waitFor("second view search to finish", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "No matches") && !strings.Contains(screen, "enter apply")
	})
	e.key("1")
	e.waitFor("first view filter restored", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "First Native Entry") && !strings.Contains(screen, "No matches") && !strings.Contains(screen, "enter apply")
	})
	e.key("/")
	e.key("C-u")
	e.key("Enter")
	e.waitFor("first view filter cleared", func() bool { return !strings.Contains(e.capture(), "enter apply") })
	e.key("e")
	e.mustShow("Generate Password")
	e.typeText(" Edited")
	e.key("C-s")
	e.mustShowWrite("First Native Entry Edited", "saved · backup created")
	e.key("2")
	e.mustShow("No matches")
	e.key("/")
	e.key("C-u")
	e.typeText("First Native Entry Edited")
	e.key("Enter")
	e.waitFor("shared entry search to finish", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "First Native Entry Edited") && !strings.Contains(screen, "enter apply")
	})
	if screen := e.capture(); strings.Contains(screen, secretSentinel) || strings.Contains(screen, fixtureEntryPassword) {
		e.failWithScreen("shared edit exposed a protected value", screen)
	}

	// Save, Discard, and Cancel are reachable from the dirty-draft navigation
	// prompt. Enter on the default Cancel choice keeps the draft open.
	e.key("/")
	e.key("C-u")
	e.key("Enter")
	e.waitFor("search reset before draft", func() bool { return !strings.Contains(e.capture(), "enter apply") })
	e.key("n")
	e.mustShow("New Entry")
	e.typeText("Saved Through Navigation")
	e.key("Escape")
	e.mustShow("UNSAVED ENTRY")
	e.key("Enter")
	e.waitFor("cancel closes unsaved dialog", func() bool { return !strings.Contains(e.capture(), "UNSAVED ENTRY") })
	e.mustShow("New Entry", "Saved Through Navigation")
	e.key("Escape")
	e.mustShow("UNSAVED ENTRY")
	e.key("Left")
	e.key("Left")
	e.key("Enter")
	e.mustShowWrite("Saved Through Navigation", "saved · backup created")

	e.key("n")
	e.mustShow("New Entry")
	e.typeText("Discarded Through Navigation")
	e.key("Escape")
	e.mustShow("UNSAVED ENTRY")
	e.key("Left")
	e.key("Enter")
	e.waitFor("discard closes unsaved dialog", func() bool { return !strings.Contains(e.capture(), "UNSAVED ENTRY") })
	e.mustNotShow("Discarded Through Navigation")

	// Open a second, independently encrypted native vault. Locking it leaves
	// both first-vault views available; unlocking it after switching requires
	// its own credential. The reverse lock direction is checked too.
	openWorkspaceVault(e, secondDB, secondVaultPassword)
	e.mustShow("Second Native Entry")
	e.key("C-p")
	e.typeText(`"Lock Vault"`)
	e.key("Enter")
	e.mustShow("Vault locked", "workspace-second.kdbx")
	e.key("1")
	e.mustShow("First Native Entry Edited", "2 entries")
	e.key("3")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixtureWrongPassword)
	e.key("Enter")
	e.mustShow("Could not unlock")
	e.typeText(secondVaultPassword)
	e.key("Enter")
	e.mustShow("Second Native Entry")
	e.key("1")
	e.mustShow("First Native Entry Edited")
	e.key("C-p")
	e.typeText(`"Lock Vault"`)
	e.key("Enter")
	e.mustShow("Vault locked", "workspace-first.kdbx")
	e.key("2")
	e.mustShow("Second Native Entry")
	e.key("1")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("First Native Entry Edited", "2 entries")

	// Closing the last view clears its session and returns to the welcome
	// screen without leaving a dead pane or a stale locked tab.
	e.key("3")
	e.key("C-w")
	e.key("C-w")
	e.key("C-w")
	e.mustShow("bypass", "VAULTS", "ctrl+o")
	e.key("q")
	e.waitForCleanQuit()
}

func TestWorkspaceTinyResizePreservesNativeDraftAndGuardsInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	e.registerSecret(fixtureEntryPassword)
	createWorkspaceVault(e, backend, e.realDB, fixturePassword, "Tiny Seed Entry", "tiny-user", fixtureEntryPassword)
	e.startWith(backend, e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Tiny Seed Entry")

	e.key("n")
	e.mustShow("New Entry")
	e.typeText("Preserved Tiny Draft")
	e.key("Tab")
	e.typeText("draft-user")
	e.key("Tab")
	e.typeText(fixtureEntryPassword)
	e.mustShow("PASSWORD *")
	e.resize(20, 5)
	e.mustDimensions(20, 5)
	e.mustShow("Terminal too small")
	e.mustNotShow(fixtureEntryPassword)
	for _, key := range []string{"n", "e", "1", "C-p", "C-o", "C-f", "q"} {
		e.key(key)
	}
	e.mustShow("Terminal too small")
	e.mustNotShow(fixtureEntryPassword)

	e.resize(120, 35)
	e.mustDimensions(120, 35)
	e.mustShow("New Entry", "Preserved Tiny Draft", fixtureEntryPassword)
	e.key("C-s")
	e.mustShowWrite("Preserved Tiny Draft", "saved · backup created")
	e.mustNotShow(fixtureEntryPassword)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	creds := vault.Credentials{Password: []byte(fixturePassword)}
	defer creds.Clear()
	db, err := vaulttest.NewCLI(backend).Open(ctx, e.realDB, creds)
	if err != nil {
		t.Fatal("open temporary native vault for draft verification:", err)
	}
	defer db.Clear()
	foundTitle, foundUser, foundPassword, seedPreserved := false, false, false, false
	passwordLength := 0
	for _, entry := range db.Entries {
		if entry.Get("Title") == "Tiny Seed Entry" {
			seedPreserved = entry.Get("UserName") == "tiny-user" && entry.Get("Password") == fixtureEntryPassword
		}
		if entry.Get("Title") == "Preserved Tiny Draft" {
			foundTitle = true
			foundUser = entry.Get("UserName") == "draft-user"
			passwordLength = len(entry.Get("Password"))
			foundPassword = entry.Get("Password") == fixtureEntryPassword
		}
	}
	if !seedPreserved || !foundTitle || !foundUser || !foundPassword {
		t.Fatalf("native vault resize verification failed (entries=%d, seed-preserved=%t, draft-title=%t, draft-username=%t, draft-password-match=%t, draft-password-length=%d)", len(db.Entries), seedPreserved, foundTitle, foundUser, foundPassword, passwordLength)
	}
}

func createWorkspaceVault(e *evalSession, backend, path, masterPassword, title, username, entryPassword string) {
	e.t.Helper()
	e.registerSecret(masterPassword)
	e.registerSecret(entryPassword)
	runCLI(e, backend, masterPassword+"\n"+masterPassword+"\n", "db-create", "-q", "--set-password", "--decryption-time", "100", path)
	runCLI(e, backend, masterPassword+"\n"+entryPassword+"\n", "add", "-q", "--username", username, "--notes", "synthetic workspace note", "--password-prompt", path, title)
}

func openWorkspaceVault(e *evalSession, path, password string) {
	e.t.Helper()
	screen := e.capture()
	if strings.Contains(screen, "bypass") && strings.Contains(screen, "OPEN VAULT") {
		e.key("o")
	} else {
		e.key("C-o")
	}
	e.key("C-u")
	e.typeText(path)
	e.key("Enter")
	e.waitFor("native vault password field", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "Password") || strings.Contains(screen, "Unlock")
	})
	e.typeText(password)
	e.key("Enter")
	e.mustActiveVault(filepath.Base(path))
}
