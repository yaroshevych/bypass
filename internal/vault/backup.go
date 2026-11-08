package vault

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

var backupMu sync.Mutex
var backupName = regexp.MustCompile(`^.+-[0-9]+\.kdbx$`)

// backupVault copies the encrypted vault before a mutation and keeps the five
// newest completed backups across all vaults in the backup directory.
func backupVault(path, directory string) error {
	backupMu.Lock()
	defer backupMu.Unlock()
	if directory == "" {
		return ErrBackup
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	backup, err := os.CreateTemp(directory, filepath.Base(path)+"-*.kdbx")
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = backup.Close()
		if !complete {
			_ = os.Remove(backup.Name())
		}
	}()
	if _, err := io.Copy(backup, source); err != nil {
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	complete = true
	return pruneBackups(directory, backup.Name())
}

// Unrelated files, symlinks, and directories in the backup directory are left alone.
func pruneBackups(directory, newest string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var backups []os.FileInfo
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !backupName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		backups = append(backups, info)
	}
	newest = filepath.Base(newest)
	sort.Slice(backups, func(i, j int) bool {
		left, right := backups[i], backups[j]
		if left.Name() == newest || right.Name() == newest {
			return left.Name() == newest
		}
		if left.ModTime().Equal(right.ModTime()) {
			return left.Name() > right.Name()
		}
		return left.ModTime().After(right.ModTime())
	})
	for _, backup := range backups[min(5, len(backups)):] {
		if err := os.Remove(filepath.Join(directory, backup.Name())); err != nil {
			return err
		}
	}
	return nil
}
