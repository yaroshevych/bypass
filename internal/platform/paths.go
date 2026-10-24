package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths contains per-user directories used by bypass. Resolve does not
// create these directories; callers create them only when writing state.
type Paths struct {
	Config string
	State  string
	Cache  string
}

// Resolve selects platform-standard directories for the current user.
func Resolve() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	paths, err := resolveFor(runtime.GOOS, home, os.Getenv, os.UserConfigDir, os.UserCacheDir)
	if err != nil {
		return paths, err
	}
	// Existing development installs retain their preferences and backups.
	// Fresh installs use the Bypass namespace; no files are moved implicitly.
	paths.Config = existingLegacyDir(paths.Config)
	paths.State = existingLegacyDir(paths.State)
	paths.Cache = existingLegacyDir(paths.Cache)
	return paths, nil
}

func existingLegacyDir(path string) string {
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return path
	}
	separator := string(filepath.Separator)
	parts := strings.Split(path, separator)
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "bypass" {
			parts[i] = "keepassui"
			legacy := strings.Join(parts, separator)
			if info, err := os.Stat(legacy); err == nil && info.IsDir() {
				return legacy
			}
			break
		}
	}
	return path
}

// ResolveFor is the deterministic form of Resolve, useful to applications
// that need to evaluate paths against a supplied environment.
func ResolveFor(goos, home string, getenv func(string) string) (Paths, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return resolveFor(goos, home, getenv, func() (string, error) {
		return windowsConfigDir(home, getenv), nil
	}, func() (string, error) {
		return windowsCacheDir(home, getenv), nil
	})
}

func resolveFor(goos, home string, getenv func(string) string, userConfigDir, userCacheDir func() (string, error)) (Paths, error) {
	if strings.TrimSpace(home) == "" {
		return Paths{}, errors.New("home directory is empty")
	}

	if goos == "windows" {
		config, err := userConfigDir()
		if err != nil {
			return Paths{}, err
		}
		cache, err := userCacheDir()
		if err != nil {
			return Paths{}, err
		}
		return Paths{
			Config: filepath.Join(config, "bypass"),
			State:  filepath.Join(config, "bypass", "state"),
			Cache:  filepath.Join(cache, "bypass"),
		}, nil
	}

	if goos == "darwin" {
		appSupport := filepath.Join(home, "Library", "Application Support", "bypass")
		cache := filepath.Join(home, "Library", "Caches", "bypass")
		return Paths{
			Config: xdgAppDir(getenv("XDG_CONFIG_HOME"), filepath.Join(appSupport, "config")),
			State:  xdgAppDir(getenv("XDG_STATE_HOME"), filepath.Join(appSupport, "state")),
			Cache:  xdgAppDir(getenv("XDG_CACHE_HOME"), cache),
		}, nil
	}

	return Paths{
		Config: xdgAppDir(getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config", "bypass")),
		State:  xdgAppDir(getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state", "bypass")),
		Cache:  xdgAppDir(getenv("XDG_CACHE_HOME"), filepath.Join(home, ".cache", "bypass")),
	}, nil
}

func xdgAppDir(value, fallback string) string {
	if filepath.IsAbs(value) {
		return filepath.Join(filepath.Clean(value), "bypass")
	}
	return fallback
}

func windowsConfigDir(home string, getenv func(string) string) string {
	if dir := getenv("APPDATA"); filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(home, "AppData", "Roaming")
}

func windowsCacheDir(home string, getenv func(string) string) string {
	if dir := getenv("LOCALAPPDATA"); filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(home, "AppData", "Local")
}
