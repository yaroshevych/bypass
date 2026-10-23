//go:build integration

package evals

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	fixturePassword      = "fixture-passphrase"
	fixtureWrongPassword = "incorrect-eval-password"
	secretSentinel       = "EVAL-PROTECTED-SECRET-DO-NOT-DISPLAY"
)

var appBinary string

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		os.Exit(m.Run())
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Fprintln(os.Stderr, "integration evals require tmux:", err)
		os.Exit(2)
	}
	root, err := os.MkdirTemp("", "keepassui-evals-build-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create eval build directory:", err)
		os.Exit(2)
	}
	appBinary = filepath.Join(root, "keepassui")
	cmd := exec.Command("go", "build", "-o", appBinary, "./cmd/keepassui")
	cmd.Dir = ".."
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build keepassui: %v\n%s", err, output)
		os.Exit(2)
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func TestMilestone1TmuxLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals use Unix process and shell behavior")
	}
	e := newEval(t)
	defer e.close()

	e.start()
	e.mustShow("KeePassUI", "Open Vault")

	// Exercise an authentication failure followed by a retry. Passwords are
	// sent as literal tmux input and are never included in diagnostics.
	e.openPath(e.db1)
	e.typeText(fixtureWrongPassword)
	e.key("Enter")
	e.mustShow("Could not unlock")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry", "Read only", "entries")
	e.mustNotShow(secretSentinel)
	e.key("Tab")
	e.key("Tab")
	e.mustShow("Password", "••••••••")
	e.scrollDownUntil("Secret Note", 16)
	e.mustShow("Secret Note")
	e.mustNotShow(secretSentinel)
	e.key("S-Tab")
	e.key("S-Tab")

	// Search can be cleared after an empty result without closing the vault.
	e.key("/")
	e.typeText("query-with-no-eval-match")
	e.key("Enter")
	e.mustShow("No matches")
	e.key("Escape")
	e.mustShow("Eval Entry")

	// A duplicate view and a second vault remain independently addressable.
	e.key("C-t")
	e.mustShow("Eval Entry", "2 first.kdbx")
	e.key("M-1")
	e.mustShow("1 first.kdbx")
	e.key("M-2")
	e.mustShow("Eval Entry", "2 first.kdbx")
	e.key("M-1")
	e.mustShow("1 first.kdbx")
	e.key("C-o")
	e.key("C-u")
	e.typeText(e.db2)
	e.key("Enter")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry", "Read only", "3 second.kdbx")
	e.mustNotShow(secretSentinel)
	e.key("C-l")
	e.mustShow("Vault locked", "second.kdbx")
	e.key("M-1")
	e.mustShow("Eval Entry")
	e.mustNotShow(secretSentinel)

	// Close all views and confirm the welcome screen is restored.
	e.key("C-w")
	e.key("C-w")
	e.key("C-w")
	e.mustShow("KeePassUI", "Open Vault")

	// Tiny terminals show a useful message and recover after resize.
	e.resize(20, 5)
	e.mustDimensions(20, 5)
	e.mustShow("Terminal too small")
	e.resize(120, 35)
	e.mustDimensions(120, 35)
	e.mustShow("KeePassUI", "Open Vault")

	e.key("q")
	e.waitFor("application quit", func() bool { return e.applicationStopped() })
}

type evalSession struct {
	t      *testing.T
	root   string
	label  string
	db1    string
	db2    string
	closed bool
}

func newEval(t *testing.T) *evalSession {
	t.Helper()
	root, err := os.MkdirTemp("", "keepassui-tmux-eval-")
	if err != nil {
		t.Fatal("create private eval directory:", err)
	}
	label := fmt.Sprintf("kui%d", time.Now().UnixNano()%1_000_000_000)
	e := &evalSession{t: t, root: root, label: label}
	e.db1 = filepath.Join(root, "first.kdbx")
	e.db2 = filepath.Join(root, "second.kdbx")
	for _, path := range []string{e.db1, e.db2} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal("create synthetic vault path:", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "home"), 0700); err != nil {
		t.Fatal("create private HOME:", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "xdg-config"), 0700); err != nil {
		t.Fatal("create private XDG_CONFIG_HOME:", err)
	}
	for _, dir := range []string{"xdg-data", "xdg-state", "xdg-cache", "xdg-runtime"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatalf("create private %s: %v", dir, err)
		}
	}
	backend := filepath.Join(root, "fake-keepassxc-cli")
	if err := os.WriteFile(backend, []byte(fakeBackend), 0700); err != nil {
		t.Fatal("create fake backend:", err)
	}
	return e
}

func (e *evalSession) start() {
	e.t.Helper()
	launcher := filepath.Join(e.root, "launch.sh")
	command := shellQuote(appBinary) + " --backend " + shellQuote(filepath.Join(e.root, "fake-keepassxc-cli"))
	script := "#!/bin/sh\nexec env HOME=" + shellQuote(filepath.Join(e.root, "home")) +
		" XDG_CONFIG_HOME=" + shellQuote(filepath.Join(e.root, "xdg-config")) +
		" XDG_DATA_HOME=" + shellQuote(filepath.Join(e.root, "xdg-data")) +
		" XDG_STATE_HOME=" + shellQuote(filepath.Join(e.root, "xdg-state")) +
		" XDG_CACHE_HOME=" + shellQuote(filepath.Join(e.root, "xdg-cache")) +
		" XDG_RUNTIME_DIR=" + shellQuote(filepath.Join(e.root, "xdg-runtime")) +
		" TERM=xterm-256color LC_ALL=C.UTF-8 " + command + "\n"
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		e.t.Fatal("write application launcher:", err)
	}
	e.tmux("-f", "/dev/null", "new-session", "-d", "-x", "120", "-y", "35", "-s", "eval", launcher)
	e.mustShow("KeePassUI", "Open Vault")
}

func (e *evalSession) openPath(path string) {
	e.t.Helper()
	e.key("o")
	e.key("C-u")
	e.typeText(path)
	e.key("Enter")
	e.waitFor("password field", func() bool {
		s := e.capture()
		return strings.Contains(s, "Password") || strings.Contains(s, "Unlock")
	})
}

func (e *evalSession) key(key string) {
	e.t.Helper()
	e.tmux("send-keys", "-t", "eval", key)
}

func (e *evalSession) typeText(value string) {
	e.t.Helper()
	e.tmux("send-keys", "-t", "eval", "-l", value)
}

func (e *evalSession) resize(width, height int) {
	e.t.Helper()
	e.tmux("resize-window", "-t", "eval", "-x", fmt.Sprint(width), "-y", fmt.Sprint(height))
}

func (e *evalSession) mustDimensions(width, height int) {
	e.t.Helper()
	out := strings.TrimSpace(e.tmux("display-message", "-p", "-t", "eval", "#{window_width} #{window_height}"))
	want := fmt.Sprintf("%d %d", width, height)
	if out != want {
		e.failWithScreen(fmt.Sprintf("tmux dimensions = %q, want %q", out, want), e.capture())
	}
}

func (e *evalSession) scrollDownUntil(text string, maxSteps int) {
	e.t.Helper()
	for step := 0; step <= maxSteps; step++ {
		if strings.Contains(e.capture(), text) {
			return
		}
		e.key("Down")
		deadline := time.Now().Add(250 * time.Millisecond)
		for time.Now().Before(deadline) {
			if strings.Contains(e.capture(), text) {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	e.failWithScreen(fmt.Sprintf("detail text %q did not appear after scrolling", text), e.capture())
}

func (e *evalSession) mustShow(parts ...string) {
	e.t.Helper()
	e.waitFor("screen to contain "+strings.Join(parts, ", "), func() bool {
		s := e.capture()
		for _, part := range parts {
			if !strings.Contains(s, part) {
				return false
			}
		}
		return true
	})
}

func (e *evalSession) mustNotShow(value string) {
	e.t.Helper()
	if screen := e.capture(); strings.Contains(screen, value) {
		e.failWithScreen("protected sentinel appeared in screen capture", screen)
	}
}

func (e *evalSession) waitFor(description string, condition func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	e.failWithScreen("timed out waiting for "+description, e.capture())
}

func (e *evalSession) capture() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", "-L", e.label, "capture-pane", "-p", "-t", "eval")
	cmd.Env = tmuxEnv(e.label)
	out, _ := cmd.Output()
	return string(out)
}

func (e *evalSession) paneDead() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", "-L", e.label, "display-message", "-p", "-t", "eval", "#{pane_dead}")
	cmd.Env = tmuxEnv(e.label)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

func (e *evalSession) applicationStopped() bool {
	if e.paneDead() {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", "-L", e.label, "has-session", "-t", "eval")
	cmd.Env = tmuxEnv(e.label)
	return cmd.Run() != nil
}

func (e *evalSession) tmux(args ...string) string {
	e.t.Helper()
	full := append([]string{"-L", e.label}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", full...)
	cmd.Env = tmuxEnv(e.label)
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.failWithScreen(fmt.Sprintf("tmux %s: %v: %s", strings.Join(args, " "), err, string(out)), e.capture())
	}
	return string(out)
}

func (e *evalSession) close() {
	if e.closed {
		return
	}
	e.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "tmux", "-L", e.label, "kill-server").Run()
	_ = os.RemoveAll(e.root)
}

func (e *evalSession) redact(s string) string {
	return strings.NewReplacer(
		secretSentinel, "[REDACTED]",
		fixturePassword, "[REDACTED]",
		fixtureWrongPassword, "[REDACTED]",
	).Replace(s)
}

func (e *evalSession) failWithScreen(description, screen string) {
	e.t.Helper()
	redacted := e.redact(screen)
	artifact := ""
	if dir := os.Getenv("KEEPASSUI_EVAL_ARTIFACTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err == nil {
			path := filepath.Join(dir, "keepassui-"+e.label+"-failure.txt")
			if err := os.WriteFile(path, []byte(redacted), 0600); err == nil {
				artifact = "; redacted screen saved to " + path
			}
		}
	}
	e.t.Fatalf("%s%s; screen:\n%s", e.redact(description), artifact, redacted)
}

func tmuxEnv(_ string) []string {
	return append(os.Environ(), "TMUX=", "TERM=xterm-256color", "LC_ALL=C.UTF-8", "TMUX_TMPDIR="+os.TempDir())
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

const fakeBackend = `#!/bin/sh
if [ "$1" = "--version" ]; then
	printf '%s\n' 'keepassxc-cli 2.8.0'
	exit 0
fi
password=
no_password=0
for arg in "$@"; do
	if [ "$arg" = "--no-password" ]; then
		no_password=1
	fi
done
if [ "$no_password" -eq 0 ]; then
	IFS= read -r password || true
fi
if [ "$no_password" -eq 0 ] && [ "$password" != "fixture-passphrase" ]; then
	printf '%s\n' 'synthetic authentication failure' >&2
	exit 1
fi
cat <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<KeePassFile><Meta><DatabaseName>Eval Vault</DatabaseName><RecycleBinEnabled>false</RecycleBinEnabled></Meta><Root><Group><UUID>00000000-0000-0000-0000-000000000001</UUID><Name>Root</Name><Entry><UUID>00000000-0000-0000-0000-000000000002</UUID><String><Key>Title</Key><Value>Eval Entry</Value></String><String><Key>UserName</Key><Value>eval-user</Value></String><String><Key>Password</Key><Value Protected="True">` + secretSentinel + `</Value></String><String><Key>Secret Note</Key><Value Protected="True">` + secretSentinel + `</Value></String></Entry></Group></Root></KeePassFile>
XML
`
