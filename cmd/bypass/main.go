package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/platform"
	runpkg "bypass/internal/run"
	"bypass/internal/ui"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func main() {
	if err := disableCoreDumps(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not disable core dumps:", err)
	}
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "run" {
		return runCommand(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("bypass", flag.ContinueOnError)
	flags.SetOutput(stderr)
	backend := flags.String("backend", "keepassxc-cli", "KeePassXC CLI executable")
	showVersion := flags.Bool("version", false, "print version and exit")
	logPath := flags.String("log-file", "", "write private vault-open diagnostics to this file")
	readOnly := flags.Bool("read-only", false, "open vaults read only")
	idle := flags.Duration("idle-timeout", 0, "vault lock timeout without a PIN (default 15m or config)")
	clipTimeout := flags.Duration("clipboard-timeout", 0, "clipboard timeout (default 10s or config)")
	debugStartup := configureDebug(flags)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: bypass [options] [vault.kdbx]\n\nBypass Secrets Manager — KeePass-compatible vault workspace.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *showVersion {
		_, err := fmt.Fprintln(stdout, "Bypass Secrets Manager", version)
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("expected at most one vault path")
	}
	var store ui.RecentStore
	var saveSettings func(platform.Settings) error
	settings := platform.Settings{IdleTimeout: 15 * time.Minute, PINIdleTimeout: 5 * time.Minute, ClipboardTimeout: 10 * time.Second}
	warning := ""
	if paths, err := platform.Resolve(); err == nil {
		store = platform.NewStore(paths)
		var err error
		settings, err = platform.LoadSettings(paths)
		if err != nil {
			warning = "Invalid settings; using defaults."
		}
		persisted := settings
		saveSettings = func(value platform.Settings) error {
			preferences := persisted
			preferences.Theme, preferences.Appearance, preferences.ASCII = value.Theme, value.Appearance, value.ASCII
			return platform.SaveSettings(paths, preferences)
		}
	} else {
		warning = "Local state unavailable; writes disabled until a backup location is available."
		*readOnly = true
	}
	if *idle != 0 {
		if *idle < time.Second || *idle > 24*time.Hour {
			return fmt.Errorf("idle timeout must be between 1s and 24h")
		}
		settings.IdleTimeout = *idle
	}
	if *clipTimeout != 0 {
		if *clipTimeout < time.Second || *clipTimeout > 10*time.Minute {
			return fmt.Errorf("clipboard timeout must be between 1s and 10m")
		}
		settings.ClipboardTimeout = *clipTimeout
	}
	path := ""
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	vaultBackend := vault.NewInteractiveCLI(*backend)
	if *logPath != "" {
		file, err := openDiagnosticLog(*logPath)
		if err != nil {
			return fmt.Errorf("could not open diagnostic log: %w", err)
		}
		defer file.Close()
		if err := file.Chmod(0600); err != nil {
			return fmt.Errorf("could not make diagnostic log private: %w", err)
		}
		logger := slog.New(slog.NewJSONHandler(file, nil))
		logger.Info("app_started", "version", version)
		vaultBackend.SetLogger(logger)
	}
	m := ui.New(ui.Options{Backend: vaultBackend, Store: store, InitialPath: path, Context: ctx, Settings: settings, ReadOnly: *readOnly, Clipboard: clipboard.New(clipboard.NewSystem()), Warning: warning, SaveSettings: saveSettings})
	defer func() {
		m.Close()
		if m.CleanupError() != nil {
			fmt.Fprintln(stderr, "Clipboard could not be cleared; clear it manually.")
		}
	}()
	model, err := debugStartup(m)
	if err != nil {
		return err
	}
	p := tea.NewProgram(model, tea.WithContext(ctx), tea.WithOutput(stdout), tea.WithAltScreen(), tea.WithMouseAllMotion())
	socketPath, socketErr := runpkg.SocketPath()
	if socketErr == nil {
		server, err := runpkg.Listen(socketPath, func(pending *runpkg.Pending) {
			p.Send(ui.RunRequestMsg{Pending: pending})
			go func() { <-pending.Context.Done(); p.Send(ui.RunDisconnectedMsg{}) }()
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			m.ShowApprovalsUnavailable(errors.Is(err, runpkg.ErrServerInUse))
		} else {
			defer server.Close()
		}
	} else {
		m.ShowApprovalsUnavailable(false)
	}
	if _, err := p.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("terminal session failed: %w", err)
	}
	return nil
}
