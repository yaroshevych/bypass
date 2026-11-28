//go:build integration

package evals

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"bypass/internal/vault"
	"bypass/internal/vaulttest"
)

func TestInteractiveAttachmentImportRenameExportAndReopen(t *testing.T) {
	t.Parallel()
	e := newEval(t)
	defer e.close()
	createRealVault(e)

	const originalName = "attachment-source.bin"
	const renamedName = "attachment-renamed.bin"
	original := []byte{0, 255, 65, 0, 128, 120}
	importPath := filepath.Join(e.root, "attachment-source.bin")
	if err := os.WriteFile(importPath, original, 0600); err != nil {
		t.Fatal("write import fixture:", err)
	}

	e.startWith(e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.key("Right")
	e.key("e")
	e.mustShow("Generate Password", "Seed Entry")
	clickEditorAttachments(t, e, "Attachments (0)")
	e.mustShow("No attachments", "Add New…")
	e.key("a")
	e.mustShow("Attachment", "File name")
	e.typeText(importPath)
	e.key("C-s")
	e.mustShow(originalName)
	e.key("Down") // Add New
	e.key("Down") // Save
	e.key("Enter")
	e.mustShow("Seed Entry", "Attachments (1)")
	e.key("C-s")
	e.waitFor("attachment import saved", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "saved · backup created") && !strings.Contains(screen, "Generate Password")
	})

	// Rename the imported attachment in the editor while preserving its bytes.
	e.key("Right")
	e.key("e")
	e.mustShow("Generate Password", "Seed Entry")
	clickEditorAttachments(t, e, "Attachments (1)")
	e.mustShow(originalName)
	e.key("Enter")
	e.mustShow("Attachment", "File name")
	e.key("C-u")
	e.typeText(renamedName)
	e.key("C-s")
	e.mustShow(renamedName)
	e.key("Down")
	e.key("Down")
	e.key("Enter")
	e.mustShow("Seed Entry", "Attachments (1)")
	e.key("C-s")
	e.waitFor("attachment rename saved", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "saved · backup created") && !strings.Contains(screen, "Generate Password")
	})

	// Export from the focused attachment field in the entry details pane.
	clickVisibleText(t, e, renamedName)
	e.mustShow(renamedName)
	e.key("x")
	e.mustShow("SAVE ATTACHMENT", "Save to")
	exportPath := filepath.Join(e.root, "attachment-export.bin")
	e.key("C-u")
	e.typeText(exportPath)
	e.key("Enter")
	e.waitFor("attachment export", func() bool {
		data, err := os.ReadFile(exportPath)
		return err == nil && bytes.Equal(data, original)
	})
	e.mustShow("Seed Entry")

	// Lock and unlock the running session, then independently reopen the vault.
	e.key("L")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.key("q")
	e.waitForCleanQuit()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	creds := vault.Credentials{Password: []byte(fixturePassword)}
	defer creds.Clear()
	backend := vaulttest.NewBackend()
	db, err := backend.Open(ctx, e.realDB, creds)
	if err != nil {
		t.Fatal("independent reopen after lock:", err)
	}
	defer db.Clear()
	if len(db.Entries) != 1 || db.Entries[0].Get("Title") != "Seed Entry" || db.Entries[0].Get("UserName") != "seed-user" || db.Entries[0].Get("Notes") != "synthetic seed note" {
		t.Fatal("attachment workflow changed unrelated entry data")
	}
	if len(db.Entries[0].AttachmentNames) != 1 || db.Entries[0].AttachmentNames[0] != renamedName {
		t.Fatalf("persisted attachment names: %v", db.Entries[0].AttachmentNames)
	}
	data, err := backend.ExportAttachment(ctx, db, creds, db.Entries[0].UUID, renamedName)
	if err != nil || !bytes.Equal(data, original) {
		clear(data)
		t.Fatal("independent reopen did not preserve attachment bytes")
	}
	clear(data)
}

func clickEditorAttachments(t *testing.T, e *evalSession, label string) {
	t.Helper()
	clickVisibleText(t, e, label)
}

func clickVisibleText(t *testing.T, e *evalSession, label string) {
	t.Helper()
	rows := strings.Split(e.capture(), "\n")
	for y, row := range rows {
		if x := strings.Index(row, label); x >= 0 {
			column := utf8.RuneCountInString(row[:x]) + 1
			mouse := "\x1b[<0;" + strconv.Itoa(column) + ";" + strconv.Itoa(y+1)
			e.typeText(mouse + "M" + mouse + "m")
			return
		}
	}
	t.Fatalf("editor attachment action %q missing from screen", label)
}
