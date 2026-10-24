package platform

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultSettingsAndMissingFileDoNotWrite(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
	want := Settings{
		IdleTimeout:        15 * time.Minute,
		ClipboardTimeout:   10 * time.Second,
		BackupDir:          filepath.Join(paths.State, "backups"),
		Theme:              "macchiato",
		Appearance:         "classic",
		GrandViewGroupSize: 2,
	}
	if got := DefaultSettings(paths); got != want {
		t.Fatalf("DefaultSettings() = %#v, want %#v", got, want)
	}
	got, err := LoadSettings(paths)
	if err != nil || got != want {
		t.Fatalf("LoadSettings(missing) = %#v, %v", got, err)
	}
	if _, err := os.Stat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("LoadSettings created config directory: %v", err)
	}
}

func TestLoadSettingsValidConfigAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup")
	data := []byte(`{"idle_timeout_seconds":900,"clipboard_timeout_seconds":20,"backup_dir":"` + backup + `","theme":"nord","appearance":"compact","ascii":true,"full_lock_timeout_seconds":3600,"grand_view_group_size":3,"future_setting":"ignored"}`)
	if err := os.WriteFile(filepath.Join(config, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(Paths{Config: config, State: filepath.Join(root, "state")})
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{IdleTimeout: 15 * time.Minute, ClipboardTimeout: 20 * time.Second, BackupDir: backup, Theme: "nord", Appearance: "compact", ASCII: true, FullLockTimeout: time.Hour, GrandViewGroupSize: 3}
	if got != want {
		t.Fatalf("LoadSettings() = %#v, want %#v", got, want)
	}
}

func TestInvalidSettingsReturnDefaultsWithError(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := Paths{Config: config, State: filepath.Join(root, "state")}
	defaults := DefaultSettings(paths)
	for _, contents := range []string{
		`{"idle_timeout_seconds":0}`,
		`{"idle_timeout_seconds":-2}`,
		`{"idle_timeout_seconds":86401}`,
		`{"clipboard_timeout_seconds":601}`,
		`{"full_lock_timeout_seconds":-1}`,
		`{"full_lock_timeout_seconds":86401}`,
		`{"idle_timeout_seconds":9223372036854775807}`,
		`{"backup_dir":"relative"}`,
		`{"backup_dir":""}`,
		`{"theme":"unknown"}`,
		`{"appearance":"wide"}`,
		`{"grand_view_group_size":0}`,
		`{"grand_view_group_size":-1}`,
		`{"grand_view_group_size":2.5}`,
		`{"grand_view_group_size":"2"}`,
		`{`,
	} {
		if err := os.WriteFile(filepath.Join(config, "config.json"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadSettings(paths)
		if err == nil || got != defaults {
			t.Fatalf("LoadSettings(%s) = %#v, %v; want defaults and error", contents, got, err)
		}
	}
}

func TestSaveSettingsPreservesUnknownKeysAndWritesPrivately(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
	if err := os.MkdirAll(paths.Config, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"future_setting":{"keep":[1,true]},"idle_timeout_seconds":77}`)
	if err := os.WriteFile(filepath.Join(paths.Config, "config.json"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings(paths)
	settings.IdleTimeout = 20 * time.Minute
	settings.ClipboardTimeout = 12 * time.Second
	settings.FullLockTimeout = 2 * time.Hour
	settings.Theme = "dracula"
	settings.Appearance = "compact"
	settings.ASCII = true
	settings.GrandViewGroupSize = 4
	if err := SaveSettings(paths, settings); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(paths)
	if err != nil || got != settings {
		t.Fatalf("settings after save = %#v, %v", got, err)
	}
	data, err := os.ReadFile(filepath.Join(paths.Config, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, object["future_setting"]); err != nil {
		t.Fatal(err)
	}
	if compact.String() != `{"keep":[1,true]}` {
		t.Fatalf("unknown config key was not preserved: %s", object["future_setting"])
	}
	for path, want := range map[string]os.FileMode{paths.Config: 0o700, filepath.Join(paths.Config, "config.json"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

func TestLoadSettingsRejectsSymlinkedConfigFile(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "other.json")
	if err := os.WriteFile(target, []byte(`{"clipboard_timeout_seconds":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(config, "config.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	paths := Paths{Config: config, State: filepath.Join(root, "state")}
	got, err := LoadSettings(paths)
	if err == nil || got != DefaultSettings(paths) {
		t.Fatalf("symlink settings = %#v, %v; want defaults and error", got, err)
	}
}
