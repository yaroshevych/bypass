package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultIdleTimeout      = 15 * time.Minute
	defaultClipboardTimeout = 10 * time.Second
	maxIdleTimeout          = 24 * time.Hour
	maxClipboardTimeout     = 10 * time.Minute
	maxSettingsBytes        = 1 << 20
)

type Settings struct {
	IdleTimeout        time.Duration
	ClipboardTimeout   time.Duration
	BackupDir          string
	Theme              string
	Appearance         string
	ASCII              bool
	FullLockTimeout    time.Duration
	GrandViewGroupSize int
	FavoriteOverrides  string
}

type settingsFile struct {
	GrandViewGroupSize      *int    `json:"grand_view_group_size"`
	IdleTimeoutSeconds      *int64  `json:"idle_timeout_seconds"`
	ClipboardTimeoutSeconds *int64  `json:"clipboard_timeout_seconds"`
	FullLockTimeoutSeconds  *int64  `json:"full_lock_timeout_seconds"`
	BackupDir               *string `json:"backup_dir"`
	Theme                   *string `json:"theme"`
	Appearance              *string `json:"appearance"`
	ASCII                   *bool   `json:"ascii"`
	FavoriteOverrides       *string `json:"favorite_overrides"`
}

func DefaultSettings(paths Paths) Settings {
	backupDir := filepath.Join(paths.State, "backups")
	if !filepath.IsAbs(backupDir) {
		if absolute, err := filepath.Abs(backupDir); err == nil {
			backupDir = absolute
		}
	}
	return Settings{
		IdleTimeout:        defaultIdleTimeout,
		ClipboardTimeout:   defaultClipboardTimeout,
		BackupDir:          filepath.Clean(backupDir),
		Theme:              "macchiato",
		Appearance:         "classic",
		GrandViewGroupSize: 2,
	}
}

// LoadSettings reads config.json without creating directories or files. Any
// malformed or invalid settings return safe defaults together with an error.
func LoadSettings(paths Paths) (Settings, error) {
	defaults := DefaultSettings(paths)
	configPath := filepath.Join(paths.Config, "config.json")
	if info, err := os.Lstat(paths.Config); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return defaults, errors.New("config path must not be a symlink")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return defaults, err
	}
	if info, err := os.Lstat(configPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return defaults, errors.New("settings file must not be a symlink")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return defaults, err
	}
	data, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	if len(data) > maxSettingsBytes {
		return defaults, errors.New("settings file is too large")
	}
	var file settingsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return defaults, fmt.Errorf("invalid settings file: %w", err)
	}
	settings := defaults
	if file.IdleTimeoutSeconds != nil {
		idle, err := durationSeconds(*file.IdleTimeoutSeconds, maxIdleTimeout)
		if err != nil {
			return defaults, errors.New("invalid idle timeout")
		}
		settings.IdleTimeout = idle
	}
	if file.ClipboardTimeoutSeconds != nil {
		clipboard, err := durationSeconds(*file.ClipboardTimeoutSeconds, maxClipboardTimeout)
		if err != nil {
			return defaults, errors.New("invalid clipboard timeout")
		}
		settings.ClipboardTimeout = clipboard
	}
	if file.FullLockTimeoutSeconds != nil {
		if *file.FullLockTimeoutSeconds == 0 {
			settings.FullLockTimeout = 0
		} else {
			fullLock, err := durationSeconds(*file.FullLockTimeoutSeconds, maxIdleTimeout)
			if err != nil {
				return defaults, errors.New("invalid full lock timeout")
			}
			settings.FullLockTimeout = fullLock
		}
	}
	if file.BackupDir != nil {
		settings.BackupDir = filepath.Clean(*file.BackupDir)
	}
	if file.Theme != nil {
		settings.Theme = *file.Theme
	}
	if file.Appearance != nil {
		settings.Appearance = *file.Appearance
	}
	if file.GrandViewGroupSize != nil {
		settings.GrandViewGroupSize = *file.GrandViewGroupSize
	}
	if file.ASCII != nil {
		settings.ASCII = *file.ASCII
	}
	if file.FavoriteOverrides != nil {
		settings.FavoriteOverrides = *file.FavoriteOverrides
	}
	if err := validateSettings(settings); err != nil {
		return defaults, err
	}
	return settings, nil
}

func durationSeconds(seconds int64, maximum time.Duration) (time.Duration, error) {
	if seconds < 1 || seconds > int64(maximum/time.Second) {
		return 0, errors.New("timeout out of range")
	}
	return time.Duration(seconds) * time.Second, nil
}

func validateSettings(settings Settings) error {
	if settings.GrandViewGroupSize < 1 {
		return errors.New("grand view group size must be a positive integer")
	}
	if settings.IdleTimeout < time.Second || settings.IdleTimeout > maxIdleTimeout {
		return fmt.Errorf("idle timeout must be between 1 second and %s", maxIdleTimeout)
	}
	if settings.ClipboardTimeout < time.Second || settings.ClipboardTimeout > maxClipboardTimeout {
		return fmt.Errorf("clipboard timeout must be between 1 second and %s", maxClipboardTimeout)
	}
	if !filepath.IsAbs(settings.BackupDir) {
		return errors.New("backup directory must be an absolute path")
	}
	if settings.FullLockTimeout < 0 || settings.FullLockTimeout > maxIdleTimeout || (settings.FullLockTimeout > 0 && settings.FullLockTimeout < time.Second) {
		return errors.New("full lock timeout must be disabled or between 1 second and 24 hours")
	}
	if err := ValidateTheme(settings.Theme); err != nil {
		return err
	}
	if settings.Appearance != "compact" && settings.Appearance != "classic" {
		return errors.New("appearance must be compact or classic")
	}
	return nil
}

func ValidateTheme(theme string) error {
	switch theme {
	case "macchiato", "mocha", "frappe", "latte", "tokyo-night", "gruvbox", "dracula", "nord", "vesper":
		return nil
	default:
		return errors.New("unsupported theme")
	}
}

// SaveSettings atomically stores settings while preserving unrecognized keys
// in an existing config file.
func SaveSettings(paths Paths, settings Settings) error {
	if err := validateSettings(settings); err != nil {
		return err
	}
	if err := ensurePrivateConfigDir(paths.Config); err != nil {
		return err
	}
	configPath := filepath.Join(paths.Config, "config.json")
	fields := make(map[string]json.RawMessage)
	if info, err := os.Lstat(configPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("settings file must be a regular file, not a symlink")
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		if len(data) > maxSettingsBytes {
			return errors.New("settings file is too large")
		}
		if err := json.Unmarshal(data, &fields); err != nil {
			return fmt.Errorf("invalid settings file: %w", err)
		}
		if fields == nil {
			fields = make(map[string]json.RawMessage)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	values := map[string]any{
		"grand_view_group_size":     settings.GrandViewGroupSize,
		"idle_timeout_seconds":      int64(settings.IdleTimeout / time.Second),
		"clipboard_timeout_seconds": int64(settings.ClipboardTimeout / time.Second),
		"full_lock_timeout_seconds": int64(settings.FullLockTimeout / time.Second),
		"backup_dir":                settings.BackupDir,
		"theme":                     settings.Theme,
		"appearance":                settings.Appearance,
		"ascii":                     settings.ASCII,
		"favorite_overrides":        settings.FavoriteOverrides,
	}
	for key, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields[key] = encoded
	}
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(paths.Config, ".config-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
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
	return os.Rename(tempPath, configPath)
}

func ensurePrivateConfigDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("config path must be a directory, not a symlink")
	}
	return os.Chmod(dir, 0o700)
}
