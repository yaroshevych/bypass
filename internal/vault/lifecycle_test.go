package vault

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func installedLifecycleCLI(t *testing.T) *CLI {
	t.Helper()
	cli := NewCLI("")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Probe(ctx); err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	return cli
}

func TestLifecycleCreateIsEncryptedAndNeverClobbers(t *testing.T) {
	cli := installedLifecycleCLI(t)
	path := filepath.Join(t.TempDir(), "created vault.kdbx")
	password := []byte("lifecycle create test password")
	db, err := cli.CreateWithOptions(context.Background(), path, Credentials{Password: password}, CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	defer db.Clear()
	canonical, err := canonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if db.Path != canonical || db.Revision == ([32]byte{}) || len(db.Groups) == 0 {
		t.Fatal("created vault was not opened and returned with its final path/revision")
	}
	mode, err := os.Stat(path)
	if err != nil || mode.Mode().Perm()&0077 != 0 {
		t.Fatal("created vault does not have private permissions")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("could not read created encrypted vault")
	}
	if _, err := cli.Create(context.Background(), path, Credentials{Password: password}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second create should preserve existing vault, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("no-clobber create changed the existing vault")
	}
	info, err := cli.Info(context.Background(), path, Credentials{Password: password})
	if err != nil {
		t.Fatalf("database info: %v", err)
	}
	if info.UUID == "" || info.Cipher == "" || info.KDF == "" {
		t.Fatalf("info omitted expected safe fields: %#v", info)
	}
}

func TestLifecycleChangeCredentialsBacksUpAndVerifiesNewKey(t *testing.T) {
	cli := installedLifecycleCLI(t)
	path := filepath.Join(t.TempDir(), "change-credentials.kdbx")
	oldPassword := []byte("old lifecycle password")
	newPassword := []byte("new lifecycle password")
	db, err := cli.Create(context.Background(), path, Credentials{Password: oldPassword})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	defer db.Clear()
	canonical, err := canonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	initialRevision := db.Revision
	backupDir := filepath.Join(t.TempDir(), "backups")
	updated, err := cli.ChangeCredentials(context.Background(), db,
		Credentials{Password: oldPassword}, Credentials{Password: newPassword}, backupDir)
	if err != nil {
		t.Fatalf("change credentials: %v", err)
	}
	defer updated.Clear()
	if updated.Revision == initialRevision || updated.Path != canonical {
		t.Fatal("credential change did not update final database metadata")
	}
	if _, err := cli.Open(context.Background(), path, Credentials{Password: oldPassword}); !errors.Is(err, ErrUnlock) {
		t.Fatalf("old password still opened database: %v", err)
	}
	check, err := cli.Open(context.Background(), path, Credentials{Password: newPassword})
	if err != nil {
		t.Fatalf("new password could not open database: %v", err)
	}
	check.Clear()
	backups, err := os.ReadDir(backupDir)
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one encrypted backup, got %d, %v", len(backups), err)
	}
	backupRevision, err := Revision(filepath.Join(backupDir, backups[0].Name()))
	if err != nil || backupRevision != initialRevision {
		t.Fatal("credential change backup does not preserve the original encrypted vault")
	}
}

func TestLifecycleCredentialChangeRejectsStaleRevisionWithoutBackup(t *testing.T) {
	cli := installedLifecycleCLI(t)
	path := filepath.Join(t.TempDir(), "stale-credentials.kdbx")
	db, err := cli.Create(context.Background(), path, Credentials{Password: []byte("stale test password")})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	defer db.Clear()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("external change")); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backups")
	_, err = cli.ChangeCredentials(context.Background(), db,
		Credentials{Password: []byte("stale test password")}, Credentials{Password: []byte("replacement password")}, backupDir)
	if !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("stale database should reject credential change, got %v", err)
	}
	if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale credential change created a backup directory")
	}
}

func TestLifecycleCredentialChangePreservesExistingKeyFileFactor(t *testing.T) {
	cli := installedLifecycleCLI(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "key-file-factor.kdbx")
	keyFile := filepath.Join(dir, "existing-key-file")
	if err := os.WriteFile(keyFile, []byte("synthetic key file contents"), 0600); err != nil {
		t.Fatal(err)
	}
	oldCreds := Credentials{Password: []byte("key factor old password"), KeyFile: keyFile}
	newCreds := Credentials{Password: []byte("key factor new password"), KeyFile: keyFile}
	db, err := cli.Create(context.Background(), path, oldCreds)
	if err != nil {
		t.Fatalf("create key-file protected vault: %v", err)
	}
	defer db.Clear()
	updated, err := cli.ChangeCredentials(context.Background(), db, oldCreds, newCreds, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatalf("rotate password while preserving key-file factor: %v", err)
	}
	defer updated.Clear()
	opened, err := cli.Open(context.Background(), path, newCreds)
	if err != nil {
		t.Fatalf("new password plus existing key file did not open vault: %v", err)
	}
	opened.Clear()
	if _, err := cli.Open(context.Background(), path, Credentials{Password: newCreds.Password}); !errors.Is(err, ErrUnlock) {
		t.Fatalf("vault opened without its required existing key file: %v", err)
	}
}

func TestLifecycleCredentialChangeAddsAndRemovesKeyFile(t *testing.T) {
	cli := installedLifecycleCLI(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "key-file-changes.kdbx")
	keyFile := filepath.Join(dir, "new-key-file")
	if err := os.WriteFile(keyFile, []byte("synthetic key file to add and remove"), 0600); err != nil {
		t.Fatal(err)
	}
	password := []byte("key-file add remove password")
	db, err := cli.Create(context.Background(), path, Credentials{Password: password})
	if err != nil {
		t.Fatalf("create password vault: %v", err)
	}
	defer db.Clear()
	backupDir := filepath.Join(dir, "backups")
	withKey, err := cli.ChangeCredentials(context.Background(), db,
		Credentials{Password: password}, Credentials{Password: password, KeyFile: keyFile}, backupDir)
	if err != nil {
		t.Fatalf("add existing key-file factor: %v", err)
	}
	defer withKey.Clear()
	if _, err := cli.Open(context.Background(), path, Credentials{Password: password}); !errors.Is(err, ErrUnlock) {
		t.Fatalf("vault opened after adding key file without that factor: %v", err)
	}
	openedWithKey, err := cli.Open(context.Background(), path, Credentials{Password: password, KeyFile: keyFile})
	if err != nil {
		t.Fatalf("password plus new key file did not open vault: %v", err)
	}
	openedWithKey.Clear()
	withoutKey, err := cli.ChangeCredentials(context.Background(), withKey,
		Credentials{Password: password, KeyFile: keyFile}, Credentials{Password: password}, backupDir)
	if err != nil {
		t.Fatalf("remove key-file factor: %v", err)
	}
	defer withoutKey.Clear()
	openedWithoutKey, err := cli.Open(context.Background(), path, Credentials{Password: password})
	if err != nil {
		t.Fatalf("password did not open vault after key-file removal: %v", err)
	}
	openedWithoutKey.Clear()
}

func TestLifecycleCredentialChangePreservesExistingEntry(t *testing.T) {
	cli := installedLifecycleCLI(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "entry-preservation.kdbx")
	oldPassword := []byte("entry preservation old password")
	newPassword := []byte("entry preservation new password")
	db, err := cli.Create(context.Background(), path, Credentials{Password: oldPassword})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	db.Clear()
	add := exec.Command(cli.binary, "add", "-q", "--username", "seed-user", "--password-prompt", path, "Lifecycle Seed")
	add.Stdin = bytes.NewBufferString(string(oldPassword) + "\nseed-entry-password\n")
	add.Stdout, add.Stderr = io.Discard, io.Discard
	if err := add.Run(); err != nil {
		t.Fatalf("seed entry before credential rotation: %v", err)
	}
	db, err = cli.Open(context.Background(), path, Credentials{Password: oldPassword})
	if err != nil {
		t.Fatalf("reopen seeded vault: %v", err)
	}
	defer db.Clear()
	if len(db.Entries) != 1 || db.Entries[0].Get("Title") != "Lifecycle Seed" {
		t.Fatal("seed entry did not open from native CLI")
	}
	updated, err := cli.ChangeCredentials(context.Background(), db,
		Credentials{Password: oldPassword}, Credentials{Password: newPassword}, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatalf("change credentials with existing entry: %v", err)
	}
	defer updated.Clear()
	if len(updated.Entries) != 1 || updated.Entries[0].Get("Title") != "Lifecycle Seed" {
		t.Fatal("credential rotation did not preserve the seeded entry")
	}
}

func TestLifecycleCreateRejectsUnsupportedFactorsAndSettings(t *testing.T) {
	cli := NewCLI("missing-cli")
	if _, err := cli.CreateWithOptions(context.Background(), filepath.Join(t.TempDir(), "db.kdbx"), Credentials{Password: []byte("pw")}, CreateOptions{DecryptionTimeMS: 99}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("invalid decryption time should be rejected before invocation, got %v", err)
	}
	if _, err := cli.Create(context.Background(), filepath.Join(t.TempDir(), "db.kdbx"), Credentials{Password: []byte("pw"), YubiKey: "slot"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("hardware factor creation should be unsupported, got %v", err)
	}
}

func TestCredentialVerificationTreatsEmptySlicesAsEquivalent(t *testing.T) {
	original := &Database{
		Name:     "synthetic",
		RootUUID: "root",
		Groups:   []Group{{UUID: "root", Name: "Root"}},
		Entries: []Entry{{
			UUID: "entry", GroupUUID: "root", Fields: []Field{{Name: "Title", Value: "entry"}},
			Tags: []string{}, AttachmentNames: []string{}, History: []Entry{},
		}},
	}
	clone := original.Clone()
	if err := verifyCredentialChange(original, clone); err != nil {
		t.Fatalf("nil and empty optional slices should compare equal: %v", err)
	}
	clone.Entries[0].Fields[0].Value = "changed"
	if err := verifyCredentialChange(original, clone); !errors.Is(err, ErrWrite) {
		t.Fatalf("credential verification accepted changed entry content: %v", err)
	}
}
