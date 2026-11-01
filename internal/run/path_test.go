package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlainPathRejectsHiddenSymlink(t *testing.T) {
	// Resolve the test harness's temporary directory first: macOS /var itself
	// commonly is a symlink unrelated to the supplied test paths.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PlainPath(link); err == nil {
		t.Fatal("accepted symlink")
	}
	if _, err := PlainPath(link + "/../target"); err == nil {
		t.Fatal("cleaning concealed a symlink")
	}
	if got, err := PlainPath(target); err != nil || got != target {
		t.Fatalf("plain path: %q, %v", got, err)
	}
}
