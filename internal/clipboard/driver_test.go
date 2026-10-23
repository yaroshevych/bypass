package clipboard

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFakeCommand(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-clipboard-command")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+contents+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLimitedBufferDoesNotExposeReaderFromAndCapsOutput(t *testing.T) {
	if _, ok := any(&limitedBuffer{}).(io.ReaderFrom); ok {
		t.Fatal("limited buffer exposes ReaderFrom and could bypass its output limit")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake commands are used")
	}
	driver := systemDriver{readCommand: writeFakeCommand(t, "head -c 1048577 /dev/zero")}
	value, err := driver.Read(context.Background())
	clear(value)
	if !errors.Is(err, ErrOperation) {
		t.Fatalf("oversized output error = %v, want ErrOperation", err)
	}
}

func TestSystemWriteSendsSecretOnlyOnStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake commands are used")
	}
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv")
	stdinPath := filepath.Join(dir, "stdin")
	command := writeFakeCommand(t, "printf '%s' \"$*\" > '"+argvPath+"'\ncat > '"+stdinPath+"'")
	secret := []byte("synthetic-secret-value")
	driver := systemDriver{writeCommand: command}
	if err := driver.Write(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Fatalf("fake command received argv data: %q", args)
	}
	if string(input) != string(secret) {
		t.Fatalf("stdin = %q, want secret bytes", input)
	}
}

func TestSystemReadBoundsCanceledChildAndHidesDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake commands are used")
	}
	driver := systemDriver{readCommand: writeFakeCommand(t, "(sleep 5) &\nwait")}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	started := time.Now()
	_, err := driver.Read(ctx)
	cancel()
	if !errors.Is(err, ErrOperation) {
		t.Fatalf("canceled read error = %v, want ErrOperation", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("canceled child kept read open too long: %s", time.Since(started))
	}

	secret := "diagnostic-secret"
	driver.readCommand = writeFakeCommand(t, "echo '"+secret+"' >&2\nexit 7")
	_, err = driver.Read(context.Background())
	if !errors.Is(err, ErrOperation) || strings.Contains(err.Error(), secret) {
		t.Fatalf("read exposed command diagnostics: %v", err)
	}
}

func TestSystemWriteHidesCommandDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake commands are used")
	}
	secret := "diagnostic-secret"
	driver := systemDriver{writeCommand: writeFakeCommand(t, "cat >/dev/null\necho '"+secret+"' >&2\nexit 7")}
	err := driver.Write(context.Background(), []byte("synthetic"))
	if !errors.Is(err, ErrOperation) || strings.Contains(err.Error(), secret) {
		t.Fatalf("write exposed command diagnostics: %v", err)
	}
}
