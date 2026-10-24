//go:build integration

package evals

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"keepassui/internal/vault"
)

func TestMilestone4EntryExtrasNativeTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	const seed = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	const historySecret = "HISTORICAL-SECRET-SYNTHETIC"
	sourceVault := filepath.Join(e.root, "extras-source.kdbx")
	runCLI(e, backend, fixturePassword+"\n"+fixturePassword+"\n", "db-create", "-q", "--set-password", "--decryption-time", "100", sourceVault)
	runCLI(e, backend, fixturePassword+"\n"+historySecret+"\n", "add", "-q", "--password-prompt", "--username", "old-user", "--notes", "Earlier notes\nSecond note line", sourceVault, "Earlier Entry")
	runCLI(e, backend, fixturePassword+"\n"+secretSentinel+"\n", "edit", "-q", "--password-prompt", "--title", "Extras Entry", "--username", "extras-user", sourceVault, "Earlier Entry")
	exportCmd := exec.Command(backend, "export", "-q", "-f", "xml", sourceVault)
	exportCmd.Stdin = strings.NewReader(fixturePassword + "\n")
	exportedXML, err := exportCmd.Output()
	if err != nil {
		t.Fatal("export native fixture:", err)
	}
	xml := strings.Replace(string(exportedXML), "<History>", "<String><Key>otp</Key><Value>otpauth://totp/Test?secret="+seed+"&amp;period=30</Value></String><History>", 1)
	clear(exportedXML)
	if !strings.Contains(xml, seed) {
		t.Fatal("native fixture has no history to augment")
	}
	xmlPath := filepath.Join(e.root, "extras-fixture.xml")
	if err := os.WriteFile(xmlPath, []byte(xml), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(e, backend, fixturePassword+"\n"+fixturePassword+"\n", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, e.realDB)
	e.startWith(backend, e.realDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Extras Entry", "TOTP configured", "History snapshots:")
	e.mustNotShow(seed)
	e.mustNotShow(secretSentinel)

	palette := func(query string) { e.key("C-p"); e.typeText(query); e.key("Enter") }
	palette("Show Copy TOTP")
	e.mustShowWrite("Authenticator Code", "Refreshes in", "Code")
	e.mustNotShow(seed)
	e.mustNotShow(secretSentinel)
	e.key("Escape")

	palette("Entry History")
	e.mustShow("Entry History", "Earlier Entry")
	e.key("Enter")
	e.mustShow("History Snapshot", "Read-only snapshot")
	e.mustNotShow(historySecret)
	e.mustNotShow(seed)
	e.key("Down")
	e.mustShow("Username", "old-user")
	e.key("Down")
	e.mustShow("Password", "••••••••")
	e.mustNotShow(historySecret)
	e.key("Down")
	e.mustShow("URL")
	e.key("Down")
	e.mustShow("Notes", "Earlier notes", "Second note line")
	e.key("Escape")
	e.mustShow("Entry History")
	e.key("Escape")

	savedToWorkspace := func() {
		t.Helper()
		e.waitForWithin(20*time.Second, "attachment saved and popup closed", func() bool {
			screen := e.capture()
			return strings.Contains(screen, "Saved. Encrypted backup created.") && !strings.Contains(screen, "┌") && !strings.Contains(screen, "Working…")
		})
	}
	// Binary attachments are written only after explicit import/export actions.
	source := filepath.Join(e.root, "binary.dat")
	original := []byte{0, 255, 65, 0, 128, 120}
	if err := os.WriteFile(source, original, 0o600); err != nil {
		t.Fatal(err)
	}
	palette("Attachments")
	e.mustShow("Attachments", "Add Attachment")
	e.key("a")
	e.mustShow("Import source path")
	e.typeText(source)
	e.key("Enter")
	savedToWorkspace()
	palette("Attachments")
	e.mustShow("binary.dat")
	e.key("Down")
	e.key("e")
	e.mustShow("Plaintext export destination")
	exported := filepath.Join(e.root, "exported.dat")
	e.typeText(exported)
	e.key("Enter")
	e.mustShow("Export attachment as plaintext", "Cancel")
	e.key("Enter") // Cancel must not export.
	e.mustShow("Add Attachment")
	if _, err := os.Stat(exported); !os.IsNotExist(err) {
		t.Fatal("default export confirmation wrote plaintext")
	}
	e.key("e")
	e.mustShow("Plaintext export destination")
	e.typeText(exported)
	e.key("Enter")
	e.mustShow("Export attachment as plaintext")
	e.key("Right")
	e.key("Enter")
	e.waitFor("binary attachment export", func() bool { data, err := os.ReadFile(exported); return err == nil && bytes.Equal(data, original) })
	if info, err := os.Stat(exported); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("export was not private")
	}
	e.key("Escape")
	e.mustShow("Extras Entry")

	// Replace and remove require an explicit final choice.
	newData := []byte{0, 254, 66, 0, 129, 121}
	if err := os.WriteFile(source, newData, 0o600); err != nil {
		t.Fatal(err)
	}
	palette("Attachments")
	e.mustShow("binary.dat")
	e.key("Down")
	e.key("r")
	e.mustShow("Import source path")
	e.typeText(source)
	e.key("Enter")
	e.mustShow("Replace attachment", "Cancel")
	e.key("Right")
	e.key("Enter")
	savedToWorkspace()

	cli := vault.NewCLI(backend)
	creds := vault.Credentials{Password: []byte(fixturePassword)}
	defer creds.Clear()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := cli.Open(ctx, e.realDB, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Entries) != 1 || len(db.Entries[0].History) < 1 || !db.Entries[0].HasTOTP {
		db.Clear()
		t.Fatal("attachment changes lost history or TOTP")
	}
	data, err := cli.ExportAttachment(ctx, db, creds, db.Entries[0].UUID, "binary.dat")
	db.Clear()
	if err != nil || !bytes.Equal(data, newData) {
		clear(data)
		t.Fatal("native reopen did not preserve replacement bytes")
	}
	clear(data)
	palette("Attachments")
	e.mustShow("binary.dat")
	e.key("Down")
	e.key("d")
	e.mustShow("Remove attachment", "Cancel")
	e.key("Enter")
	e.mustShow("binary.dat", "Add Attachment")
	e.key("d")
	e.mustShow("Remove attachment")
	e.key("Right")
	e.key("Enter")
	savedToWorkspace()
	db, err = cli.Open(ctx, e.realDB, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Entries) != 1 || len(db.Entries[0].AttachmentNames) != 0 || !strings.Contains(db.Entries[0].Get("otp"), seed) {
		db.Clear()
		t.Fatal("remove altered unrelated data or retained attachment")
	}
	db.Clear()
	e.mustNotShow(seed)
	e.mustNotShow(secretSentinel)
	e.key("q")
	e.waitForCleanQuit()
}
