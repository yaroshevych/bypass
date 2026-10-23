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
	IdleTimeout      time.Duration
	ClipboardTimeout time.Duration
	BackupDir        string
}

type settingsFile struct {
	IdleTimeoutSeconds      *int64  `json:"idle_timeout_seconds"`
	ClipboardTimeoutSeconds *int64  `json:"clipboard_timeout_seconds"`
	BackupDir               *string `json:"backup_dir"`
}

func DefaultSettings(paths Paths) Settings {
	backupDir := filepath.Join(paths.State, "backups")
	if !filepath.IsAbs(backupDir) {
		if absolute, err := filepath.Abs(backupDir); err == nil {
			backupDir = absolute
		}
	}
	return Settings{
		IdleTimeout:      defaultIdleTimeout,
		ClipboardTimeout: defaultClipboardTimeout,
		BackupDir:        filepath.Clean(backupDir),
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
	if file.BackupDir != nil {
		settings.BackupDir = filepath.Clean(*file.BackupDir)
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
	if settings.IdleTimeout < time.Second || settings.IdleTimeout > maxIdleTimeout {
		return fmt.Errorf("idle timeout must be between 1 second and %s", maxIdleTimeout)
	}
	if settings.ClipboardTimeout < time.Second || settings.ClipboardTimeout > maxClipboardTimeout {
		return fmt.Errorf("clipboard timeout must be between 1 second and %s", maxClipboardTimeout)
	}
	if !filepath.IsAbs(settings.BackupDir) {
		return errors.New("backup directory must be an absolute path")
	}
	return nil
}
