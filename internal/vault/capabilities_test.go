package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func capabilityFixture(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	path := filepath.Join(t.TempDir(), "fake-cli")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCommandCapabilitiesParsesExactKnownFirstTokens(t *testing.T) {
	bin := capabilityFixture(t, `
[ "$1" = "--help" ] || exit 3
cat <<'HELP'
Usage: fake-cli [options]
Available commands:
db-create            Create a database
db-edit              Edit a database
db-info              Show database information
db-create-helper     Not a registered native command
prefix db-info        Not a command line
HELP
`)
	capabilities, err := NewCLI(bin).CommandCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"db-create", "db-edit", "db-info"} {
		if !capabilities[command] {
			t.Fatalf("recognized command %q missing from capabilities: %#v", command, capabilities)
		}
	}
	if capabilities["add"] || len(capabilities) != len(knownCLICommands) {
		t.Fatalf("expected a complete known-command map with unavailable commands false: %#v", capabilities)
	}
}

func TestCommandCapabilitiesRejectsUnknownOutputAndBoundsHelp(t *testing.T) {
	unknown := capabilityFixture(t, `printf '%s\n' 'db-create-helper appears in this unrelated text'`)
	if _, err := NewCLI(unknown).CommandCapabilities(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unrecognized help should be unsupported, got %v", err)
	}
	large := capabilityFixture(t, "dd if=/dev/zero bs=65536 count=2 2>/dev/null\n")
	if _, err := NewCLI(large).CommandCapabilities(context.Background()); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized help should be rejected, got %v", err)
	}
}

func TestCommandCapabilitiesCancellationAndUnavailable(t *testing.T) {
	if _, err := NewCLI(filepath.Join(t.TempDir(), "missing")).CommandCapabilities(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing CLI should be unavailable, got %v", err)
	}
	bin := capabilityFixture(t, "exec sleep 10\n")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(25*time.Millisecond, cancel)
	_, err := NewCLI(bin).CommandCapabilities(ctx)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled help probe should return cancellation sentinel, got %v", err)
	}
}

func TestCommandCapabilitiesNativeCLI(t *testing.T) {
	cli := NewCLI("")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Probe(ctx); err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	capabilities, err := cli.CommandCapabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"db-create", "db-edit", "db-info", "add", "export"} {
		if !capabilities[command] {
			t.Fatalf("native CLI help omitted expected command %q: %#v", command, capabilities)
		}
	}
}
