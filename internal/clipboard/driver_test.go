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

func TestLimitedBufferWipeOverwritesItsStorage(t *testing.T) {
	var buffer limitedBuffer
	secret := []byte("synthetic clipboard secret")
	buffer.limit = len(secret)
	if _, err := buffer.Write(secret); err != nil {
		t.Fatal(err)
	}
	storage := buffer.Bytes()
	buffer.wipe()
	if string(storage) != string(make([]byte, len(storage))) || buffer.buf.Len() != 0 {
		t.Fatal("limited buffer retained plaintext after wipe")
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

func TestLinuxWaylandClipboardProbesSensitiveSupport(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "old wl-copy", true: "new wl-copy"}[supported], func(t *testing.T) {
			dir := t.TempDir()
			argsPath := filepath.Join(dir, "args")
			inputPath := filepath.Join(dir, "input")
			help := "echo 'Usage: wl-copy'\n"
			if supported {
				help = "echo 'Usage: wl-copy [--sensitive]'\n"
			}
			copyScript := "#!/bin/sh\nif [ \"$1\" = --help ]; then\n" + help + "exit 0\nfi\nprintf '%s' \"$*\" > '" + argsPath + "'\nexec /bin/cat > '" + inputPath + "'\n"
			for name, script := range map[string]string{
				"wl-copy":  copyScript,
				"wl-paste": "#!/bin/sh\nexit 0\n",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir)
			driver, ok := newSystemForOS("linux").(systemDriver)
			if !ok || SupportsSensitive(driver) != supported || !RequiresSensitive(driver) {
				t.Fatalf("Wayland capability mismatch: %#v", driver)
			}
			if SupportsSensitive(unavailableDriver{}) || SupportsSensitive(systemDriver{writeCommand: "xclip"}) {
				t.Fatal("non-sensitive clipboard driver advertised sensitive support")
			}
			if CanCopySensitive(systemDriver{writeCommand: "xclip"}) != true {
				t.Fatal("xclip should allow a field copy when sensitivity is optional")
			}
			if CanCopySensitive(driver) != supported {
				t.Fatalf("Wayland field-copy policy = %t, want %t", CanCopySensitive(driver), supported)
			}

			manager := New(driver)
			if err := manager.Copy(context.Background(), []byte("public"), time.Now(), time.Minute); err != nil {
				t.Fatalf("ordinary write: %v", err)
			}
			args, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			if supported && string(args) != "--sensitive" {
				t.Fatalf("supported wl-copy args = %q, want --sensitive", args)
			}
			if !supported && len(args) != 0 {
				t.Fatalf("old wl-copy args = %q, want no options", args)
			}

			err = manager.CopySensitive(context.Background(), []byte("synthetic secret"), time.Now(), time.Minute)
			if supported {
				if err != nil {
					t.Fatalf("sensitive write on supported wl-copy: %v", err)
				}
				args, err = os.ReadFile(argsPath)
				if err != nil || string(args) != "--sensitive" {
					t.Fatalf("sensitive wl-copy args = %q, err=%v", args, err)
				}
			} else if !errors.Is(err, ErrSensitiveUnavailable) {
				t.Fatalf("unsupported sensitive write error = %v", err)
			} else {
				input, readErr := os.ReadFile(inputPath)
				if readErr != nil || string(input) != "public" || !manager.hasOwned {
					t.Fatalf("refused secret write disturbed prior clipboard: input=%q owned=%t err=%v", input, manager.hasOwned, readErr)
				}
			}
		})
	}
}
