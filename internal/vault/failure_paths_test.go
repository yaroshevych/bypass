package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const failureFixturePassword = "synthetic failure-path master password"

func newFailureFixture(t *testing.T, cli *CLI) (string, Credentials, *Database, [32]byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "failure-path.kdbx")
	creds := Credentials{Password: []byte(failureFixturePassword)}
	created, err := cli.Create(context.Background(), path, creds)
	if err != nil {
		t.Fatalf("create synthetic vault: %v", err)
	}
	seeded, err := cli.Apply(context.Background(), created, creds, Change{
		Kind: AddEntry, GroupUUID: created.RootUUID,
		Values:   map[string]string{"Title": "Failure Seed", "UserName": "fixture-user"},
		Password: []byte("synthetic entry password"), PasswordChanged: true,
	}, filepath.Join(dir, "setup-backups"))
	created.Clear()
	if err != nil {
		t.Fatalf("seed synthetic vault: %v", err)
	}
	t.Cleanup(seeded.Clear)
	return path, creds, seeded, seeded.Revision
}

func requireFailureFixtureCLI(t *testing.T) *CLI {
	t.Helper()
	cli := NewCLI("")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Probe(ctx); err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	return cli
}

func assertFailureVaultUnchanged(t *testing.T, cli *CLI, path string, creds Credentials, expected [32]byte, expectedEntries int) {
	t.Helper()
	actual, err := Revision(path)
	if err != nil || actual != expected {
		t.Fatalf("vault revision changed: got %x, want %x (err %v)", actual, expected, err)
	}
	db, err := cli.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatalf("unchanged vault no longer opens: %v", err)
	}
	defer db.Clear()
	seedTitle := ""
	if len(db.Entries) > 0 {
		seedTitle = db.Entries[0].Get("Title")
	}
	if len(db.Entries) != expectedEntries || seedTitle != "Failure Seed" {
		t.Fatalf("failed operation changed vault data: entries=%d seed=%q", len(db.Entries), seedTitle)
	}
}

func TestCredentialChangeFailurePathsPreserveOriginalVault(t *testing.T) {
	cli := requireFailureFixtureCLI(t)
	newPassword := Credentials{Password: []byte("replacement synthetic password")}
	t.Run("backup failure", func(t *testing.T) {
		path, creds, db, revision := newFailureFixture(t, cli)
		blocker := filepath.Join(t.TempDir(), "backup-blocker")
		if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
		backupDir := filepath.Join(blocker, "backups")
		if _, err := cli.ChangeCredentials(context.Background(), db, creds, newPassword, backupDir); !errors.Is(err, ErrBackup) {
			t.Fatalf("credential change with blocked backup should fail safely, got %v", err)
		}
		assertFailureVaultUnchanged(t, cli, path, creds, revision, 1)
		if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			t.Fatal("failed backup unexpectedly created a backup directory")
		}
	})

	t.Run("read only file", func(t *testing.T) {
		path, creds, db, revision := newFailureFixture(t, cli)
		if err := os.Chmod(path, 0400); err != nil {
			t.Fatal(err)
		}
		_, err := cli.ChangeCredentials(context.Background(), db, creds, newPassword, filepath.Join(t.TempDir(), "backups"))
		if !errors.Is(err, ErrReadOnly) {
			t.Fatalf("read-only credential change should fail safely, got %v", err)
		}
		assertFailureVaultUnchanged(t, cli, path, creds, revision, 1)
	})

	t.Run("stale database snapshot", func(t *testing.T) {
		path, creds, staleDB, _ := newFailureFixture(t, cli)
		externalDB, err := cli.Open(context.Background(), path, creds)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := cli.Apply(context.Background(), externalDB, creds, Change{
			Kind: AddEntry, GroupUUID: externalDB.RootUUID,
			Values:   map[string]string{"Title": "External Update"},
			Password: []byte("external entry password"), PasswordChanged: true,
		}, filepath.Join(t.TempDir(), "external-backups"))
		externalDB.Clear()
		if err != nil {
			t.Fatalf("simulate valid external update: %v", err)
		}
		updated.Clear()
		currentRevision, err := Revision(path)
		if err != nil {
			t.Fatal(err)
		}
		backupDir := filepath.Join(t.TempDir(), "failed-change-backups")
		if _, err := cli.ChangeCredentials(context.Background(), staleDB, creds, newPassword, backupDir); !errors.Is(err, ErrExternalChanged) {
			t.Fatalf("stale credential change should be rejected, got %v", err)
		}
		actual, err := Revision(path)
		if err != nil || actual != currentRevision {
			t.Fatal("stale credential change modified the external revision")
		}
		db, err := cli.Open(context.Background(), path, creds)
		if err != nil {
			t.Fatalf("externally updated vault no longer opens: %v", err)
		}
		defer db.Clear()
		if len(db.Entries) != 2 || entryByTitle(db, "External Update") == nil {
			t.Fatal("stale credential change lost the external entry")
		}
		if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("stale credential change made a backup before rejecting the snapshot")
		}
	})

	t.Run("incorrect current credentials", func(t *testing.T) {
		path, creds, db, revision := newFailureFixture(t, cli)
		wrongCurrent := Credentials{Password: []byte("incorrect current password")}
		backupDir := filepath.Join(t.TempDir(), "failed-change-backups")
		if _, err := cli.ChangeCredentials(context.Background(), db, wrongCurrent, newPassword, backupDir); err == nil {
			t.Fatal("credential change accepted an incorrect current password")
		}
		assertFailureVaultUnchanged(t, cli, path, creds, revision, 1)
		if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("credential failure created a backup")
		}
	})

	t.Run("corrupt staged CLI output", func(t *testing.T) {
		cliReal := requireFailureFixtureCLI(t)
		path, creds, db, revision := newFailureFixture(t, cliReal)
		realPath, err := exec.LookPath("keepassxc-cli")
		if err != nil {
			t.Skip("keepassxc-cli is not installed")
		}
		wrapper := filepath.Join(t.TempDir(), "corrupt-edit-cli")
		script := "#!/bin/sh\nif [ \"$1\" = db-edit ]; then\n  for arg do stage=$arg; done\n  printf 'corrupt synthetic staging data' > \"$stage\"\n  exit 0\nfi\nexec " + shellQuoteFailure(realPath) + " \"$@\"\n"
		if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		backupDir := filepath.Join(t.TempDir(), "failed-change-backups")
		_, err = NewCLI(wrapper).ChangeCredentials(context.Background(), db, creds, newPassword, backupDir)
		if !errors.Is(err, ErrWrite) {
			t.Fatalf("corrupt staged output should fail credential rotation, got %v", err)
		}
		assertFailureVaultUnchanged(t, cliReal, path, creds, revision, 1)
		if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("corrupt staged output created an encrypted backup")
		}
	})
}

func TestAttachmentStageFailurePreservesOriginalVault(t *testing.T) {
	cli := requireFailureFixtureCLI(t)
	path, creds, db, _ := newFailureFixture(t, cli)
	entry := entryByTitle(db, "Failure Seed")
	if entry == nil {
		t.Fatal("synthetic attachment entry missing")
	}
	entryID := entry.UUID
	attachment := []byte{0, 0xff, 'x', 0, 0x80}
	withAttachment, err := cli.Apply(context.Background(), db, creds, Change{
		Kind: AddAttachment, EntryUUID: entryID,
		AttachmentName: "failure-fixture.bin", AttachmentData: attachment,
	}, filepath.Join(t.TempDir(), "setup-attachment-backups"))
	db.Clear()
	if err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	defer withAttachment.Clear()
	revision := withAttachment.Revision

	realPath, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	wrapper := filepath.Join(t.TempDir(), "corrupt-attachment-cli")
	script := "#!/bin/sh\nif [ \"$1\" = attachment-import ]; then\n  exit 1\nfi\nexec " + shellQuoteFailure(realPath) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "failed-attachment-backups")
	_, err = NewCLI(wrapper).Apply(context.Background(), withAttachment, creds, Change{
		Kind: ReplaceAttachment, EntryUUID: entryID,
		AttachmentName: "failure-fixture.bin", AttachmentData: []byte("replacement bytes"),
	}, backupDir)
	if !errors.Is(err, ErrWrite) {
		t.Fatalf("corrupt attachment staging should fail transactionally, got %v", err)
	}
	assertFailureVaultUnchanged(t, cli, path, creds, revision, 1)
	reopened, err := cli.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Clear()
	updatedEntry := entryByTitle(reopened, "Failure Seed")
	if updatedEntry == nil {
		t.Fatal("seed entry missing after failed attachment replacement")
	}
	got, err := cli.ExportAttachment(context.Background(), reopened, creds, updatedEntry.UUID, "failure-fixture.bin")
	if err != nil || !bytes.Equal(got, attachment) {
		clear(got)
		t.Fatalf("failed replacement changed original attachment: %v", err)
	}
	clear(got)
	if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed attachment staging created an encrypted backup")
	}
}

func TestCredentialChangeStalledCLIIsCanceledWithoutMutation(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX executable wrapper fixture")
	}
	cli := requireFailureFixtureCLI(t)
	path, creds, db, revision := newFailureFixture(t, cli)
	marker := filepath.Join(t.TempDir(), "started.fifo")
	if err := syscall.Mkfifo(marker, 0600); err != nil {
		t.Fatal(err)
	}

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "stall-edit-cli")
	script := "#!/bin/sh\nexec " + shellQuoteFailure(binary) + " -test.run=^TestLifecycleStallHelper$ -- " + shellQuoteFailure(marker) + "\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := NewCLI(wrapper).ChangeCredentials(ctx, db, creds, Credentials{Password: []byte("canceled replacement password")}, filepath.Join(t.TempDir(), "backups"))
		result <- err
	}()
	ready := make(chan error, 1)
	go func() {
		f, err := os.Open(marker)
		if err != nil {
			ready <- err
			return
		}
		defer f.Close()
		var buf [1]byte
		_, err = f.Read(buf[:])
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			cancel()
			t.Fatalf("stall helper did not start: %v", err)
		}
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("stall helper did not signal that db-edit started")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, ErrCanceled) {
			t.Fatalf("stalled db-edit did not return cancellation sentinel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("credential change did not stop after context cancellation")
	}
	assertFailureVaultUnchanged(t, cli, path, creds, revision, 1)
}

func TestLifecycleStallHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	address := os.Args[separator+1]
	f, err := os.OpenFile(address, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open stall marker: %v", err)
	}
	_, _ = f.Write([]byte("x"))
	_ = f.Close()
	select {}
}

func shellQuoteFailure(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func entryByTitle(db *Database, title string) *Entry {
	for i := range db.Entries {
		if db.Entries[i].Get("Title") == title {
			return &db.Entries[i]
		}
	}
	return nil
}
