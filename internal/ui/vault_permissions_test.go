package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWarnsAboutSharedVaultPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.kdbx")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{})
	m.Update(openedMsg{db: fixture(path)})
	if !strings.Contains(m.notice, "other users") {
		t.Fatal("missing vault permission warning")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatal("opening changed existing permissions")
	}
}
