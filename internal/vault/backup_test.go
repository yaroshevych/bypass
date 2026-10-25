package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestBackupsKeepNewestFiveAcrossVaults(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "backups")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(directory, "personal.kdbx")
	if err := os.WriteFile(unrelated, []byte("leave alone"), 0600); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 8; index++ {
		path := filepath.Join(root, fmt.Sprintf("vault-%d.kdbx", index%2))
		if err := os.WriteFile(path, []byte(fmt.Sprint(index)), 0600); err != nil {
			t.Fatal(err)
		}
		if err := backupInteractiveVault(path, directory); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !backupName.MatchString(entry.Name()) {
				continue
			}
			name := filepath.Join(directory, entry.Name())
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) == fmt.Sprint(index) {
				stamp := time.Unix(int64(index+1), 0)
				if err := os.Chtimes(name, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !backupName.MatchString(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		seen[string(data)] = true
	}
	if len(seen) != 5 {
		t.Fatalf("kept %d backup versions; want 5", len(seen))
	}
	for index := 3; index < 8; index++ {
		if !seen[fmt.Sprint(index)] {
			t.Fatalf("missing recent backup %d", index)
		}
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "leave alone" {
		t.Fatal("unrelated vault changed")
	}
	if err := backupInteractiveVault(filepath.Join(root, "missing"), directory); err == nil {
		t.Fatal("missing source accepted")
	}
	entries, err = os.ReadDir(directory)
	if err != nil || len(entries) != 6 {
		t.Fatal("failed backup removed existing files")
	}
}

func TestConcurrentBackupsRespectTotalLimit(t *testing.T) {
	root := t.TempDir()
	path, directory := filepath.Join(root, "vault.kdbx"), filepath.Join(root, "backups")
	if err := os.WriteFile(path, []byte("disposable ciphertext stand-in"), 0600); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errors := make(chan error, 12)
	for index := 0; index < cap(errors); index++ {
		workers.Add(1)
		go func() { defer workers.Done(); errors <- backupInteractiveVault(path, directory) }()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 5 {
		t.Fatalf("concurrent backup count = %d, err %v", len(entries), err)
	}
}
