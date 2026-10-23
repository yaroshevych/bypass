package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths contains per-user directories used by keepassui. Resolve does not
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
	return resolveFor(runtime.GOOS, home, os.Getenv, os.UserConfigDir, os.UserCacheDir)
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
			Config: filepath.Join(config, "keepassui"),
			State:  filepath.Join(config, "keepassui", "state"),
			Cache:  filepath.Join(cache, "keepassui"),
		}, nil
	}

	if goos == "darwin" {
		appSupport := filepath.Join(home, "Library", "Application Support", "keepassui")
		cache := filepath.Join(home, "Library", "Caches", "keepassui")
		return Paths{
			Config: xdgAppDir(getenv("XDG_CONFIG_HOME"), filepath.Join(appSupport, "config")),
			State:  xdgAppDir(getenv("XDG_STATE_HOME"), filepath.Join(appSupport, "state")),
			Cache:  xdgAppDir(getenv("XDG_CACHE_HOME"), cache),
		}, nil
	}

	return Paths{
		Config: xdgAppDir(getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config", "keepassui")),
		State:  xdgAppDir(getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state", "keepassui")),
		Cache:  xdgAppDir(getenv("XDG_CACHE_HOME"), filepath.Join(home, ".cache", "keepassui")),
	}, nil
}

func xdgAppDir(value, fallback string) string {
	if filepath.IsAbs(value) {
		return filepath.Join(filepath.Clean(value), "keepassui")
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
