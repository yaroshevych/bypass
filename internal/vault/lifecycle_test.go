package vault

import (
	"context"
	"errors"
	"os"
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

func TestLifecycleCreateRejectsUnsupportedFactorsAndSettings(t *testing.T) {
	cli := NewCLI("missing-cli")
	if _, err := cli.CreateWithOptions(context.Background(), filepath.Join(t.TempDir(), "db.kdbx"), Credentials{Password: []byte("pw")}, CreateOptions{DecryptionTimeMS: 99}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("invalid decryption time should be rejected before invocation, got %v", err)
	}
	if _, err := cli.Create(context.Background(), filepath.Join(t.TempDir(), "db.kdbx"), Credentials{Password: []byte("pw"), YubiKey: "slot"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("hardware factor creation should be unsupported, got %v", err)
	}
}
