package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	if len(args) > 0 && args[0] == "help" {
		switch {
		case len(args) == 1:
			args = []string{"--help"}
		case len(args) == 2 && args[1] == "run":
			args = []string{"run", "--help"}
		default:
			return fmt.Errorf("unknown help topic; try 'bypass --help'")
		}
	}
	if len(args) > 0 && args[0] == "run" {
		return runCommand(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "widget-service" {
		return widgetCommand(os.Stdin, stdout)
	}
	flags := flag.NewFlagSet("bypass", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version and exit")
	readOnly := flags.Bool("read-only", false, "open vaults read only")
	idle := flags.Duration("idle-timeout", 0, "vault lock timeout without a PIN (default 15m or config)")
	clipTimeout := flags.Duration("clipboard-timeout", 0, "clipboard timeout (default 10s or config)")
	debugStartup := configureDebug(flags)
	var help bytes.Buffer
	flags.Usage = func() {
		fmt.Fprintln(&help, `Usage:
bypass [OPTIONS] [VAULT]    Run in interactive TUI mode. VAULT is an optional
                            path to a vault file; omit it to open the welcome screen.

bypass run [OPTIONS] -- COMMAND [ARGS...]
                            Run in non-interactive mode, passing secrets to COMMAND
                            via environment variables.

Options:
  --read-only               Open vaults read only
  --idle-timeout TIME       Vault lock timeout (default: 15m or config)
  --clipboard-timeout TIME  Clipboard timeout (default: 10s or config)
  --version                 Print version and exit
  --help                    Show this help

Examples:
  bypass
  bypass vault.kdbx
  API_TOKEN=bypass://GitHub bypass run -- ./scripts/publish.sh

Run 'bypass help run' for command options.`)
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			_, _ = io.Copy(stdout, &help)
			return nil
		}
		return fmt.Errorf("invalid option or value; try 'bypass --help'")
	}
	if *showVersion {
		_, err := fmt.Fprintln(stdout, "bypass", version)
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("expected at most one vault path; try 'bypass --help'")
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
		if settings.Theme == "omarchy" && settings.OmarchyPalette.Background == "" {
			warning = "Omarchy theme unavailable; using the default palette."
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
			return fmt.Errorf("--idle-timeout must be between 1s and 24h")
		}
		settings.IdleTimeout = *idle
	}
	if *clipTimeout != 0 {
		if *clipTimeout < time.Second || *clipTimeout > 10*time.Minute {
			return fmt.Errorf("--clipboard-timeout must be between 1s and 10m")
		}
		settings.ClipboardTimeout = *clipTimeout
	}
	path := ""
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	vaultBackend := vault.NewBackend()
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
