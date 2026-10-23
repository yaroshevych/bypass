package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"keepassui/internal/platform"
	"keepassui/internal/ui"
	"keepassui/internal/vault"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("keepassui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	backend := flags.String("backend", "keepassxc-cli", "KeePassXC CLI executable")
	showVersion := flags.Bool("version", false, "print version and exit")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: keepassui [--backend executable] [vault.kdbx]\n\nRead-only KeePass vault workspace.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *showVersion {
		_, err := fmt.Fprintln(stdout, "KeePassUI", version)
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("expected at most one vault path")
	}
	var store ui.RecentStore
	if paths, err := platform.Resolve(); err == nil {
		store = platform.NewStore(paths)
	}
	path := ""
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	m := ui.New(ui.Options{Backend: vault.NewCLI(*backend), Store: store, InitialPath: path, Context: ctx})
	defer m.Close()
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithOutput(stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("terminal session failed: %w", err)
	}
	return nil
}
