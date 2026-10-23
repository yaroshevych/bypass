package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentRecentUpdatesRetainPaths(t *testing.T) {
	root := t.TempDir()
	store := NewStore(Paths{State: filepath.Join(root, "state")})
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := store.Remember(filepath.Join(root, string(rune('a'+i))+".kdbx")); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	paths, err := store.LoadRecent()
	if err != nil || len(paths) != 5 {
		t.Fatalf("concurrent updates lost: count=%d err=%v", len(paths), err)
	}
}

func TestStoreRecentOrderDedupLimitAndRemove(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "private", "state")
	store := NewStore(Paths{State: state})
	if got, err := store.LoadRecent(); err != nil || len(got) != 0 {
		t.Fatalf("missing recent list = %v, %v", got, err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("read created state directory: %v", err)
	}

	for i := 0; i < recentLimit+2; i++ {
		path := filepath.Join(root, "vault-"+string(rune('a'+i)))
		if err := store.Remember(path); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(root, "vault-a")
	if err := store.Remember(first); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadRecent()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != recentLimit || got[0] != first {
		t.Fatalf("recent list has wrong size/order: %v", got)
	}
	count := 0
	for _, path := range got {
		if path == first {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("path is not deduplicated: %v", got)
	}
	if err := store.Remove(first); err != nil {
		t.Fatal(err)
	}
	got, err = store.LoadRecent()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range got {
		if path == first {
			t.Fatalf("removed path remains in list: %v", got)
		}
	}
}

func TestStoreCanonicalizesSymlinkAndUsesPrivateAtomicFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "vault.kdbx")
	if err := os.WriteFile(target, []byte("vault"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.kdbx")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	state := filepath.Join(root, "state")
	store := NewStore(Paths{State: state})
	if err := store.Remember(alias); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(target); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadRecent()
	canonicalTarget, canonicalErr := canonicalPath(target)
	if err != nil || canonicalErr != nil || len(got) != 1 || got[0] != canonicalTarget {
		t.Fatalf("canonical recent paths = %v, %v", got, err)
	}
	for path, want := range map[string]os.FileMode{state: 0o700, filepath.Join(state, "recent.json"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

func TestStoreIgnoresUnknownSchemaFieldsAndRejectsMalformedJSON(t *testing.T) {
	state := t.TempDir()
	store := NewStore(Paths{State: state})
	data, _ := json.Marshal(map[string]any{"recent": []string{"one"}, "future": true})
	if err := os.WriteFile(filepath.Join(state, "recent.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadRecent(); err != nil || len(got) != 1 {
		t.Fatalf("unknown field handling = %v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(state, "recent.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadRecent(); err == nil {
		t.Fatal("malformed JSON did not return an error")
	}
}

func TestStoreRejectsSymlinkStateDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state-link")
	if err := os.Symlink(target, state); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := NewStore(Paths{State: state}).Remember(filepath.Join(root, "vault")); err == nil {
		t.Fatal("store accepted a symlink as its state directory")
	}
	if _, err := os.Stat(filepath.Join(target, "recent.json")); !os.IsNotExist(err) {
		t.Fatalf("store wrote through the state directory symlink: %v", err)
	}
}
