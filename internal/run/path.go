package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PlainPath returns an absolute path after rejecting every observable symlink.
// Check before cleaning: cleaning away a component could hide a symlink.
func PlainPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = cwd + string(os.PathSeparator) + path
	}
	current := string(os.PathSeparator)
	for _, component := range strings.Split(path, string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			current = filepath.Dir(current)
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("cannot inspect path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink paths are unsupported: %s", current)
		}
	}
	return current, nil
}
