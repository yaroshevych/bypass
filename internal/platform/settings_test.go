package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultSettingsAndMissingFileDoNotWrite(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
	want := Settings{
		IdleTimeout:      15 * time.Minute,
		ClipboardTimeout: 10 * time.Second,
		BackupDir:        filepath.Join(paths.State, "backups"),
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
	data := []byte(`{"idle_timeout_seconds":900,"clipboard_timeout_seconds":20,"backup_dir":"` + backup + `","future_setting":"ignored"}`)
	if err := os.WriteFile(filepath.Join(config, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(Paths{Config: config, State: filepath.Join(root, "state")})
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{IdleTimeout: 15 * time.Minute, ClipboardTimeout: 20 * time.Second, BackupDir: backup}
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
		`{"idle_timeout_seconds":9223372036854775807}`,
		`{"backup_dir":"relative"}`,
		`{"backup_dir":""}`,
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
