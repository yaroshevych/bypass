//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticLogRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openDiagnosticLog(link); err == nil {
		f.Close()
		t.Fatal("opened a symlink")
	}
	f, err := openDiagnosticLog(filepath.Join(dir, "log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected log mode: %v, %v", info, err)
	}
}
