package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeDuplicateRepair(t *testing.T) {
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("native CLI required")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "duplicates.kdbx")
	xmlPath := filepath.Join(dir, "fixture.xml")
	xml := `<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Fixture</DatabaseName></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Passwords</Name>`
	for i, title := range []string{"Amazon/web", "Amazon/web", "Amazon/web 1"} {
		id := []string{"AgICAgICAgICAgICAgICAg==", "AwMDAwMDAwMDAwMDAwMDAw==", "BAQEBAQEBAQEBAQEBAQEBA=="}[i]
		xml += `<Entry><UUID>` + id + `</UUID><String><Key>Title</Key><Value>` + title + `</Value></String><String><Key>UserName</Key><Value>` + id + `</Value></String><String><Key>Password</Key><Value>synthetic-secret</Value></String></Entry>`
	}
	xml += `</Group></Root></KeePassFile>`
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("keepassxc-cli", "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, path)
	cmd.Stdin = bytes.NewBufferString("fixture-password\nfixture-password\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture import: %v: %s", err, output)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backend := NewInteractiveCLI("")
	creds := Credentials{Password: []byte("fixture-password")}
	defer creds.Clear()
	openPlan := func() *DuplicateEntries {
		t.Helper()
		_, err := backend.Open(context.Background(), path, creds)
		var plan *DuplicateEntries
		if !errors.As(err, &plan) {
			t.Fatalf("expected repair offer: %v", err)
		}
		return plan
	}
	canceled := openPlan()
	canceled.Close()
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(original, unchanged) {
		t.Fatal("cancel changed vault")
	}
	failed := openPlan()
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := failed.Rename(context.Background(), blocked); !errors.Is(err, ErrBackup) {
		t.Fatal("expected backup failure", err)
	}
	unchanged, _ = os.ReadFile(path)
	if !bytes.Equal(original, unchanged) {
		t.Fatal("backup failure changed vault")
	}
	plan := openPlan()
	defer plan.Close()
	if len(plan.Renames) != 2 || plan.Renames[0].Title != "Amazon/web 2" || plan.Renames[1].Title != "Amazon/web 3" {
		t.Fatal("incorrect collision handling")
	}
	backup := filepath.Join(dir, "backups")
	db, err := plan.Rename(context.Background(), backup)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if len(db.Entries) != 3 {
		t.Fatal("lost entries")
	}
	users := map[string]bool{}
	for _, entry := range db.Entries {
		users[entry.Get("UserName")] = true
		secret, err := backend.ResolveField(context.Background(), db, Credentials{}, entry.UUID, "Password")
		if err != nil || string(secret) != "synthetic-secret" {
			t.Fatal("entry password changed", err)
		}
		zero(secret)
	}
	if len(users) != 3 {
		t.Fatal("distinct entries were not preserved")
	}
	files, err := os.ReadDir(backup)
	if err != nil || len(files) != 1 {
		t.Fatal("missing encrypted backup", err)
	}
	copied, err := os.ReadFile(filepath.Join(backup, files[0].Name()))
	if err != nil || !bytes.Equal(original, copied) {
		t.Fatal("backup differs from original")
	}
	reopened, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Clear()
}
