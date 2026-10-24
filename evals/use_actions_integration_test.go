//go:build integration

package evals

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestClipboardCopyExpiryAndExternalReplacementTmux(t *testing.T) {
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
	e.startWith(backend, "--clipboard-timeout=1s", e.realDB)
	e.mustShow("Unlock Vault", "Password")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.mustNotShow(secretSentinel)

	e.key("J")
	e.key("p")
	e.key("C-p")
	e.typeText("Copy")
	e.key("Enter")
	e.mustClipboardEqual([]byte(fixtureEntryPassword))
	e.registerSecret(fixtureEntryPassword)
	e.mustShow("Clipboard clears in")

	const replacement = "EXTERNAL-CLIPBOARD-REPLACEMENT"
	e.replaceClipboard([]byte(replacement))
	e.waitFor("owned clipboard timeout", func() bool {
		return !strings.Contains(e.capture(), "Clipboard clears in")
	})
	e.mustClipboardEqual([]byte(replacement))
	// Copy again without replacing the clipboard to exercise normal expiry of
	// a value the application still owns.
	e.key("J")
	e.key("p")
	e.key("C-p")
	e.typeText("Copy")
	e.key("Enter")
	e.mustClipboardEqual([]byte(fixtureEntryPassword))
	e.mustShow("Clipboard clears in")
	e.mustClipboardEqual(nil)
	e.waitFor("owned clipboard expiry notice", func() bool {
		return !strings.Contains(e.capture(), "Clipboard clears in")
	})
	e.replaceClipboard([]byte(replacement))
	e.key("q")
	e.waitForCleanQuit()
	e.mustClipboardEqual([]byte(replacement))
}

func TestHistoryCopyRevealAndRemaskTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	const historySecret = "HISTORY-SECRET-EVAL-ONLY"
	e.registerSecret(historySecret)
	runCLI(e, backend, fixturePassword+"\n"+fixturePassword+"\n", "db-create", "-q", "--set-password", "--decryption-time", "100", e.realDB)
	runCLI(e, backend, fixturePassword+"\n"+historySecret+"\n", "add", "-q", "--password-prompt", "--username", "history-user", "--notes", "earlier synthetic note", e.realDB, "Earlier Entry")
	runCLI(e, backend, fixturePassword+"\n"+secretSentinel+"\n", "edit", "-q", "--password-prompt", "--title", "History Entry", "--username", "current-user", e.realDB, "Earlier Entry")
	exportCmd := exec.Command(backend, "export", "-q", "-f", "xml", e.realDB)
	exportCmd.Stdin = strings.NewReader(fixturePassword + "\n")
	exportedXML, err := exportCmd.Output()
	if err != nil {
		t.Fatal("export synthetic history fixture:", err)
	}
	xml := strings.Replace(string(exportedXML), "<History>", "<String><Key>Fixture Marker</Key><Value>history-fixture</Value></String><History>", 1)
	clear(exportedXML)
	if !strings.Contains(xml, historySecret) {
		t.Fatal("native XML fixture did not contain historical credential data")
	}
	xmlPath := filepath.Join(e.root, "history-fixture.xml")
	importedDB := filepath.Join(e.root, "history-fixture.kdbx")
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		t.Fatal("write synthetic history fixture:", err)
	}
	runCLI(e, backend, fixturePassword+"\n"+fixturePassword+"\n", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, importedDB)
	e.startWith(backend, importedDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("History Entry")

	e.key("C-p")
	e.typeText("Entry History")
	e.key("Enter")
	e.mustShow("Entry History", "Earlier Entry")
	e.key("Enter")
	e.key("Down")
	e.mustShow("Username", "history-user")
	e.key("Down")
	e.mustShow("Password", "••••••••")
	e.key("v")
	e.mustShow(historySecret)
	e.key("Down")
	e.waitFor("history field remask after focus change", func() bool {
		return !strings.Contains(e.capture(), historySecret)
	})
	e.key("Up")
	e.key("c")
	e.mustClipboardEqual([]byte(historySecret))
	e.key("Escape")
	e.key("Escape")
	e.mustNotShow(historySecret)
	e.key("q")
	e.waitForCleanQuit()
}

func TestTOTPClipboardAndRefreshUseControlledBackendTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	e := newEval(t)
	defer e.close()
	marker := filepath.Join(e.root, "totp-next-code")
	xmlPath := filepath.Join(e.root, "totp-fixture.xml")
	fakeCLI := filepath.Join(e.root, "controlled-totp-cli")
	const seed = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	e.registerSecret(seed)
	e.registerSecret("111111")
	e.registerSecret("222222")
	entryXML := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Controlled TOTP</DatabaseName></Meta><Root><Group><UUID>01010101-0101-0101-0101-010101010101</UUID><Name>Root</Name><Entry><UUID>02020202-0202-0202-0202-020202020202</UUID><String><Key>Title</Key><Value>Controlled TOTP Entry</Value></String><String><Key>UserName</Key><Value>totp-user</Value></String><String><Key>Password</Key><Value Protected="True">` + secretSentinel + `</Value></String><String><Key>otp</Key><Value Protected="True">otpauth://totp/Eval?secret=` + seed + `&amp;period=1</Value></String></Entry></Group></Root></KeePassFile>`
	if err := os.WriteFile(xmlPath, []byte(entryXML), 0600); err != nil {
		t.Fatal("write synthetic TOTP XML:", err)
	}
	if err := os.WriteFile(e.realDB, []byte("synthetic controlled backend revision"), 0600); err != nil {
		t.Fatal("write synthetic TOTP vault path:", err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then printf '%s\\n' 'keepassxc-cli 2.8.0'; exit 0; fi\n" +
		"if [ \"$1\" = \"show\" ]; then IFS= read -r password || true; [ \"$password\" = " + shellQuote(fixturePassword) + " ] || exit 1; if [ -f " + shellQuote(marker) + " ]; then printf '%s\\n' '222222'; else printf '%s\\n' '111111'; fi; exit 0; fi\n" +
		"if [ \"$1\" = \"export\" ]; then IFS= read -r password || true; [ \"$password\" = " + shellQuote(fixturePassword) + " ] || exit 1; cat " + shellQuote(xmlPath) + "; exit 0; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(fakeCLI, []byte(script), 0700); err != nil {
		t.Fatal("write controlled fake CLI:", err)
	}
	e.startWith(fakeCLI, e.realDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Controlled TOTP Entry")
	e.mustNotShow(seed)
	e.key("C-p")
	e.typeText("Show Copy TOTP")
	e.key("Enter")
	e.mustShow("Authenticator Code", "111111", "Refreshes in")
	e.mustNotShow(seed)
	e.key("c")
	e.mustClipboardEqual([]byte("111111"))
	if err := os.WriteFile(marker, []byte("refresh"), 0600); err != nil {
		t.Fatal("switch synthetic TOTP code:", err)
	}
	e.waitFor("controlled TOTP refresh", func() bool {
		return strings.Contains(e.capture(), "222222")
	})
	e.mustNotShow(seed)
	e.key("c")
	e.mustClipboardEqual([]byte("222222"))
	e.key("Escape")
	e.mustNotShow(seed)
	e.mustNotShow(secretSentinel)
	e.key("q")
	e.waitForCleanQuit()
}

func TestKeyFileUnlockAndPasswordKeyFileRotationTmux(t *testing.T) {
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
	keyFile := filepath.Join(e.root, "synthetic.key")
	const keyMaterial = "synthetic key-file material for a temporary KeePass fixture"
	e.registerSecret(keyMaterial)
	if err := os.WriteFile(keyFile, []byte(keyMaterial), 0600); err != nil {
		t.Fatal("write synthetic key file:", err)
	}
	e.startWith(backend, e.realDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")

	e.key("C-p")
	e.typeText("Change Vault Credentials")
	e.key("Enter")
	e.mustShow("New password", "Confirm password")
	e.typeText(fixtureWrongPassword)
	e.registerSecret(fixtureWrongPassword)
	e.key("Tab")
	e.typeText(fixtureWrongPassword)
	e.key("Tab")
	e.typeText(keyFile)
	e.key("C-s")
	e.mustShow("Change the credentials for this vault?", "Change Credentials")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("Credentials changed and verified. Encrypted backup created.")

	e.key("C-l")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("Unlock Vault", "Key file (optional)")
	e.typeText(fixtureWrongPassword)
	e.key("Tab")
	e.typeText(keyFile)
	e.key("Enter")
	e.mustShow("Seed Entry")
	cli := vault.NewCLI(backend)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	withoutKey, err := cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixtureWrongPassword)})
	if withoutKey != nil {
		withoutKey.Clear()
	}
	if !errors.Is(err, vault.ErrUnlock) {
		t.Fatalf("native open without the required key file returned %v", err)
	}
	withKey, err := cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixtureWrongPassword), KeyFile: keyFile})
	if err != nil {
		t.Fatalf("native open with required key file failed: %v", err)
	}
	withKey.Clear()

	e.key("C-p")
	e.typeText("Change Vault Credentials")
	e.key("Enter")
	e.mustShow("New password", "Confirm password")
	e.typeText(fixtureEntryPassword)
	e.registerSecret(fixtureEntryPassword)
	e.key("Tab")
	e.typeText(fixtureEntryPassword)
	e.key("Tab") // The existing key-file path remains selected and is preserved.
	e.key("C-s")
	e.mustShow("Change the credentials for this vault?")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("Credentials changed and verified. Encrypted backup created.")
	e.key("C-l")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("Unlock Vault")
	e.typeText(fixtureEntryPassword)
	e.key("Tab")
	e.typeText(keyFile)
	e.key("Enter")
	e.mustShow("Seed Entry")
	rotated, err := cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixtureEntryPassword), KeyFile: keyFile})
	if err != nil {
		t.Fatalf("native reopen after password/key-file rotation failed: %v", err)
	}
	rotated.Clear()
	// Remove the existing key-file factor through the UI, retaining password
	// authentication. The old key file should then be unnecessary and, when
	// supplied as an extra factor, should not unlock the vault.
	e.key("C-p")
	e.typeText("Change Vault Credentials")
	e.key("Enter")
	e.mustShow("New password", "Confirm password")
	e.typeText(fixtureEntryPassword)
	e.key("Tab")
	e.typeText(fixtureEntryPassword)
	e.key("Tab")
	e.key("C-u")
	e.key("C-s")
	e.mustShow("Change the credentials for this vault?")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("Credentials changed and verified. Encrypted backup created.")
	passwordOnly, err := cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixtureEntryPassword)})
	if err != nil {
		t.Fatalf("native reopen after removing key file failed: %v", err)
	}
	passwordOnly.Clear()
	withOldKey, err := cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixtureEntryPassword), KeyFile: keyFile})
	if withOldKey != nil {
		withOldKey.Clear()
	}
	if !errors.Is(err, vault.ErrUnlock) {
		t.Fatalf("native open with removed key file returned %v", err)
	}
	e.key("C-l")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("Unlock Vault")
	e.typeText(fixtureEntryPassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.mustNotShow(secretSentinel)
	e.key("q")
	e.waitForCleanQuit()
}

func TestReadOnlyBlocksHighImpactPaletteActionsTmux(t *testing.T) {
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
	const attachment = "synthetic read-only attachment"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cli := vault.NewCLI(backend)
	db, err := cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixturePassword)})
	if err != nil {
		t.Fatalf("open synthetic readonly fixture for setup: %v", err)
	}
	entryUUID := evalEntryIDByTitle(db, db.RootUUID, "Seed Entry")
	updated, err := cli.Apply(ctx, db, vault.Credentials{Password: []byte(fixturePassword)}, vault.Change{Kind: vault.AddAttachment, EntryUUID: entryUUID, AttachmentName: "readonly.bin", AttachmentData: []byte(attachment)}, filepath.Join(e.root, "setup-backups"))
	db.Clear()
	if err != nil {
		t.Fatalf("seed synthetic readonly attachment: %v", err)
	}
	updated.Clear()
	originalRevision, err := vault.Revision(e.realDB)
	if err != nil {
		t.Fatal("record readonly fixture revision:", err)
	}
	e.startWith(backend, "--read-only", e.realDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry", "Read only")
	e.key("C-p")
	e.typeText("New Vault")
	e.mustShow("New Vault", "Restart without --read-only to create a vault")
	e.key("Escape")
	e.key("n")
	e.mustShow("Read only: enable writes with R first.")
	e.mustNotShow("New Entry")
	e.key("C-p")
	e.typeText("Attachments")
	e.key("Enter")
	e.mustShow("Attachments", "readonly.bin", "Read only")
	e.key("a")
	e.mustShow("Attachment changes require write access and an encrypted backup location")
	e.key("Down")
	e.key("r")
	e.mustShow("Select an attachment and enable writes first.")
	e.key("d")
	e.mustShow("Select an attachment and enable writes first.")
	e.key("Escape")
	e.mustShow("Seed Entry")
	e.key("C-p")
	e.typeText("Change Vault Credentials")
	e.mustShow("Change Vault Credentials", "Vault is read only")
	e.key("Escape")
	e.key("q")
	e.waitForCleanQuit()
	finalRevision, err := vault.Revision(e.realDB)
	if err != nil || finalRevision != originalRevision {
		t.Fatalf("readonly actions changed the encrypted vault: err=%v", err)
	}
	db, err = cli.Open(ctx, e.realDB, vault.Credentials{Password: []byte(fixturePassword)})
	if err != nil {
		t.Fatalf("reopen readonly fixture: %v", err)
	}
	defer db.Clear()
	entryUUID = evalEntryIDByTitle(db, db.RootUUID, "Seed Entry")
	data, err := cli.ExportAttachment(ctx, db, vault.Credentials{Password: []byte(fixturePassword)}, entryUUID, "readonly.bin")
	if err != nil || string(data) != attachment {
		clear(data)
		t.Fatalf("readonly action changed existing attachment: %v", err)
	}
	clear(data)
}

func evalEntryIDByTitle(db *vault.Database, groupUUID, title string) string {
	if db == nil {
		return ""
	}
	for _, entry := range db.Entries {
		if entry.GroupUUID == groupUUID && entry.Get("Title") == title {
			return entry.UUID
		}
	}
	return ""
}
