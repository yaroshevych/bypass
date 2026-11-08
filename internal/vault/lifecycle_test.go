package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeCreateIsEncryptedPrivateAndNeverClobbers(t *testing.T) {
	backend := NewBackend()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "created vault.kdbx")
	password := []byte("synthetic lifecycle test password")
	creds := Credentials{Password: password}
	db, err := backend.CreateWithOptions(ctx, path, creds, CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	defer db.Clear()
	info, err := backend.InfoDatabase(ctx, db)
	if err != nil {
		t.Fatalf("database info: %v", err)
	}
	if info.UUID == "" || info.Cipher == "" || info.KDF == "" {
		t.Fatalf("info omitted expected fields: %#v", info)
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if db.Path != canonical || db.Revision == ([32]byte{}) || len(db.Groups) == 0 {
		t.Fatal("created vault was not opened and returned with its path and revision")
	}
	fileInfo, err := os.Stat(path)
	if err != nil || fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("new vault permissions = %v, %v", fileInfo, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(before, password) || bytes.Contains(before, []byte("created vault")) {
		t.Fatal("vault file contains plaintext credentials or metadata")
	}
	if _, err := backend.CreateWithOptions(ctx, path, creds, CreateOptions{}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second create should preserve existing vault, got %v", err)
	}
	db.Clear()
	if db.Unlocked() {
		t.Fatal("cleared database still reports an unlocked backend")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("no-clobber create changed the existing vault")
	}
}

func TestNativeCreateRejectsUnsupportedFactorsAndSettings(t *testing.T) {
	backend := NewBackend()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.kdbx")
	if _, err := backend.CreateWithOptions(ctx, path, Credentials{Password: []byte("synthetic password")}, CreateOptions{DecryptionTimeMS: 99}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("invalid KDF duration should be rejected, got %v", err)
	}
	if _, err := backend.CreateWithOptions(ctx, path, Credentials{Password: []byte("synthetic password"), YubiKey: "slot"}, CreateOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("hardware factor creation should be unsupported, got %v", err)
	}
}

func TestNativeKeyOnlyCredentialsRoundTrip(t *testing.T) {
	backend := NewBackend()
	ctx := context.Background()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	key := bytes.Repeat([]byte{0x5a}, 32)
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	creds := Credentials{KeyFile: keyPath, NoPassword: true}
	vaultPath := filepath.Join(dir, "key-only.kdbx")
	db, err := backend.CreateWithOptions(ctx, vaultPath, creds, CreateOptions{})
	if err != nil {
		t.Fatalf("create key-only vault: %v", err)
	}
	db.Clear()
	db, err = backend.Open(ctx, vaultPath, creds)
	if err != nil {
		t.Fatalf("open key-only vault: %v", err)
	}
	db.Clear()
	wrong := Credentials{Password: []byte("wrong")}
	if _, err := backend.Open(ctx, vaultPath, wrong); err == nil {
		t.Fatal("opened key-only vault without its key file")
	}
}
