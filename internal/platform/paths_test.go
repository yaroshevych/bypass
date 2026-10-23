package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestResolveForUnixDefaultsAndOverrides(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "alice")
	got, err := ResolveFor("linux", home, env(map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, "xdg-config"),
		"XDG_STATE_HOME":  filepath.Join(home, "xdg-state"),
		"XDG_CACHE_HOME":  "relative-cache",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		Config: filepath.Join(home, "xdg-config", "keepassui"),
		State:  filepath.Join(home, "xdg-state", "keepassui"),
		Cache:  filepath.Join(home, ".cache", "keepassui"),
	}
	if got != want {
		t.Fatalf("ResolveFor() = %#v, want %#v", got, want)
	}
}

func TestResolveForDarwinAndWindows(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "alice")
	darwin, err := ResolveFor("darwin", home, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if darwin.Config != filepath.Join(home, "Library", "Application Support", "keepassui", "config") ||
		darwin.State != filepath.Join(home, "Library", "Application Support", "keepassui", "state") ||
		darwin.Cache != filepath.Join(home, "Library", "Caches", "keepassui") {
		t.Fatalf("unexpected macOS paths: %#v", darwin)
	}

	windows, err := ResolveFor("windows", home, env(map[string]string{
		"APPDATA":      filepath.Join(home, "roaming"),
		"LOCALAPPDATA": filepath.Join(home, "local"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if windows.Config != filepath.Join(home, "roaming", "keepassui") ||
		windows.State != filepath.Join(home, "roaming", "keepassui", "state") ||
		windows.Cache != filepath.Join(home, "local", "keepassui") {
		t.Fatalf("unexpected Windows paths: %#v", windows)
	}
}

func TestResolveForDarwinNamespacesXDGOverridesAndIgnoresRelativeValues(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "alice")
	got, err := ResolveFor("darwin", home, env(map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, "xdg-config"),
		"XDG_STATE_HOME":  filepath.Join(home, "xdg-state"),
		"XDG_CACHE_HOME":  "relative-cache",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		Config: filepath.Join(home, "xdg-config", "keepassui"),
		State:  filepath.Join(home, "xdg-state", "keepassui"),
		Cache:  filepath.Join(home, "Library", "Caches", "keepassui"),
	}
	if got != want {
		t.Fatalf("ResolveFor(darwin) = %#v, want %#v", got, want)
	}
}

func TestResolveDoesNotCreatePaths(t *testing.T) {
	home := t.TempDir()
	paths, err := ResolveFor("linux", home, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatalf("ResolveFor created directories or stat failed unexpectedly: %v", err)
	}
	_ = paths
}
