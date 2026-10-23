package platform

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const recentLimit = 10

// Store persists non-sensitive application state below Paths.State.
type Store struct {
	paths Paths
	mu    sync.Mutex
}

type recentFile struct {
	Recent []string `json:"recent"`
}

func NewStore(paths Paths) *Store { return &Store{paths: paths} }

func (s *Store) recentPath() string { return filepath.Join(s.paths.State, "recent.json") }

// LoadRecent returns recent vault paths. A missing file is an empty list.
func (s *Store) LoadRecent() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRecent()
}

func (s *Store) loadRecent() ([]string, error) {
	if info, err := os.Lstat(s.paths.State); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("state path must not be a symlink")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	recentPath := s.recentPath()
	if info, err := os.Lstat(recentPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("recent state file must not be a symlink")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	data, err := os.ReadFile(recentPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored recentFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	paths := make([]string, 0, min(len(stored.Recent), recentLimit))
	seen := make(map[string]struct{}, recentLimit)
	for _, path := range stored.Recent {
		if path == "" {
			continue
		}
		canonical, err := canonicalPath(path)
		if err != nil {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		paths = append(paths, canonical)
		if len(paths) == recentLimit {
			break
		}
	}
	return paths, nil
}

// Remember puts path at the front of the recent list and retains at most ten.
func (s *Store) Remember(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	paths, err := s.loadRecent()
	if err != nil {
		return err
	}
	updated := make([]string, 0, recentLimit)
	updated = append(updated, canonical)
	for _, existing := range paths {
		if existing != canonical && len(updated) < recentLimit {
			updated = append(updated, existing)
		}
	}
	return s.writeRecent(updated)
}

// Remove removes path from the recent list.
func (s *Store) Remove(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	paths, err := s.loadRecent()
	if err != nil {
		return err
	}
	updated := make([]string, 0, len(paths))
	for _, existing := range paths {
		if existing != canonical {
			updated = append(updated, existing)
		}
	}
	return s.writeRecent(updated)
}

func (s *Store) writeRecent(paths []string) error {
	if err := ensurePrivateStateDir(s.paths.State); err != nil {
		return err
	}
	data, err := json.Marshal(recentFile{Recent: paths})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.paths.State, ".recent-*.tmp")
	if err != nil {
		return err
	}
	tempName := file.Name()
	defer os.Remove(tempName)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, s.recentPath()); err != nil {
		return err
	}
	return nil
}

func ensurePrivateStateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("state path must be a directory, not a symlink")
	}
	return os.Chmod(dir, 0o700)
}

func canonicalPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(abs), nil
}
