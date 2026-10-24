package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingLegacyDirectoryPreservesSettingsWithoutMovingFiles(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "bypass", "config")
	legacy := filepath.Join(root, "keepassui", "config")
	if got := existingLegacyDir(current); got != current {
		t.Fatal("fresh install did not use Bypass namespace")
	}
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if got := existingLegacyDir(current); got != legacy {
		t.Fatal("existing install lost its settings directory")
	}
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Fatal("path resolution unexpectedly created a directory")
	}
	if err := os.MkdirAll(current, 0700); err != nil {
		t.Fatal(err)
	}
	if got := existingLegacyDir(current); got != current {
		t.Fatal("legacy directory overrode existing Bypass settings")
	}
}

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
		Config: filepath.Join(home, "xdg-config", "bypass"),
		State:  filepath.Join(home, "xdg-state", "bypass"),
		Cache:  filepath.Join(home, ".cache", "bypass"),
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
	if darwin.Config != filepath.Join(home, "Library", "Application Support", "bypass", "config") ||
		darwin.State != filepath.Join(home, "Library", "Application Support", "bypass", "state") ||
		darwin.Cache != filepath.Join(home, "Library", "Caches", "bypass") {
		t.Fatalf("unexpected macOS paths: %#v", darwin)
	}

	windows, err := ResolveFor("windows", home, env(map[string]string{
		"APPDATA":      filepath.Join(home, "roaming"),
		"LOCALAPPDATA": filepath.Join(home, "local"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if windows.Config != filepath.Join(home, "roaming", "bypass") ||
		windows.State != filepath.Join(home, "roaming", "bypass", "state") ||
		windows.Cache != filepath.Join(home, "local", "bypass") {
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
		Config: filepath.Join(home, "xdg-config", "bypass"),
		State:  filepath.Join(home, "xdg-state", "bypass"),
		Cache:  filepath.Join(home, "Library", "Caches", "bypass"),
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
