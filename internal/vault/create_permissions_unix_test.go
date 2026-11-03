//go:build darwin || linux

package vault

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCreatePrivateWithPermissiveUmask(t *testing.T) {
	if path := os.Getenv("BYPASS_TEST_UMASK_VAULT"); path != "" {
		unix.Umask(0)
		db, err := NewInteractiveCLI("").CreateWithOptions(context.Background(), path, Credentials{Password: []byte("fixture-password")}, CreateOptions{DecryptionTimeMS: 100})
		if err != nil {
			t.Fatal(err)
		}
		db.Clear()
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("vault mode: %v, %v", info, err)
		}
		return
	}
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("keepassxc-cli unavailable")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCreatePrivateWithPermissiveUmask$")
	cmd.Env = append(os.Environ(), "BYPASS_TEST_UMASK_VAULT="+filepath.Join(t.TempDir(), "vault.kdbx"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
}
