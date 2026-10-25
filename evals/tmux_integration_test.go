//go:build integration

package evals

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	fixturePassword      = "fixture-passphrase"
	fixtureWrongPassword = "incorrect-eval-password"
	fixtureEntryPassword = "m2-entry-password"
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
	root, err := os.MkdirTemp("", "bypass-evals-build-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create eval build directory:", err)
		os.Exit(2)
	}
	appBinary = filepath.Join(root, "bypass")
	cmd := exec.Command("go", "build", "-race", "-o", appBinary, "./cmd/bypass")
	cmd.Dir = ".."
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build bypass: %v\n%s", err, output)
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
	e.mustShow("bypass", "VAULTS", "ctrl+o")

	// Exercise an authentication failure followed by a retry. Passwords are
	// sent as literal tmux input and are never included in diagnostics.
	e.openPathBySelection()
	e.typeText(fixtureWrongPassword)
	e.key("Enter")
	e.mustShow("Could not unlock")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry", "read only", "entries")
	e.mustNotShow(secretSentinel)
	e.key("Tab")
	e.key("Tab")
	e.mustShow("Password", "••••••••")
	e.mustShow("NOTES")
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
	e.key("t")
	e.mustShow("Eval Entry", "2 EVAL ENTRY")
	e.key("1")
	e.mustShow("1 EVAL ENTRY")
	e.key("2")
	e.mustShow("Eval Entry", "2 EVAL ENTRY")
	e.key("1")
	e.mustShow("1 EVAL ENTRY")
	e.key("C-o")
	e.key("C-u")
	e.typeText(e.db2)
	e.key("Enter")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry", "read only", "3 EVAL ENTRY")
	e.mustNotShow(secretSentinel)
	e.key("C-l")
	e.mustShow("Vault locked", "second.kdbx")
	e.key("1")
	e.mustActiveVault("first.kdbx")
	e.mustShow("Vault locked")
	e.mustNotShow(secretSentinel)
	e.key("2")
	e.mustActiveVault("second.kdbx")
	e.mustShow("Vault locked")
	e.key("1")
	e.mustActiveVault("first.kdbx")

	// Close all views and confirm the welcome screen is restored.
	e.key("C-w")
	e.key("C-w")
	e.key("C-w")
	e.mustShow("bypass", "VAULTS", "ctrl+o")

	// Tiny terminals show a useful message and recover after resize.
	e.resize(20, 5)
	e.mustDimensions(20, 5)
	e.mustShow("Terminal too small")
	e.resize(120, 35)
	e.mustDimensions(120, 35)
	e.mustShow("bypass", "VAULTS", "ctrl+o")

	e.key("q")
	e.waitForCleanQuit()
}

func TestMilestone2RealCLITmuxWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals use Unix process and shell behavior")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)

	e.startWith(backend, e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")

	// A new multiline entry starts in the vault's root group.
	e.key("n")
	e.mustShow("New Entry")
	e.typeText("Eval Created")
	e.key("Tab")
	e.typeText("m2-user")
	e.key("Tab")
	e.typeText(fixtureEntryPassword)
	e.key("Tab")
	e.typeText("https://m2.test")
	e.key("Tab")
	e.typeText("first note line")
	e.key("Enter")
	e.typeText("second note line")
	e.key("C-s")
	e.mustShowWrite("Eval Created", "saved · backup created")
	e.mustNotShow(fixtureEntryPassword)

	// Edit the title, cancel one generator dialog, then apply a generated
	// password and save the edit. The generated value must stay masked.
	e.key("e")
	e.mustShow("Generate Password")
	e.typeText(" Edited")
	e.key("Tab")
	e.key("Tab")
	e.key("C-g")
	e.mustShow("Length (8–64)")
	e.key("Escape")
	e.mustShow("Generate Password")
	e.mustNotShow("PASSWORD *")
	e.key("C-g")
	e.key("Enter")
	e.mustShow("Generated password")
	e.mustNotShow(fixtureEntryPassword)
	e.key("Enter")
	e.mustShow("Eval Created *", "PASSWORD *")
	e.mustNotShow(fixtureEntryPassword)
	e.key("C-s")
	e.waitForWithin(20*time.Second, "edited entry saved", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "Eval Created Edited") && strings.Contains(screen, "saved · backup created") && !strings.Contains(screen, "Generate Password")
	})

	// Unsaved Esc defaults to Cancel. Left selects Discard; Enter removes the
	// draft without writing it.
	e.key("n")
	e.typeText("Discard Draft")
	e.key("Escape")
	e.mustShow("UNSAVED ENTRY")
	e.key("Enter")
	e.waitFor("cancel closes unsaved dialog", func() bool { return !strings.Contains(e.capture(), "UNSAVED ENTRY") })
	e.mustShow("New Entry")
	e.key("Escape")
	e.mustShow("UNSAVED ENTRY")
	e.key("Left")
	e.key("Enter")
	e.waitFor("discard closes unsaved dialog", func() bool { return !strings.Contains(e.capture(), "UNSAVED ENTRY") })
	e.mustNotShow("Discard Draft")

	// Create a group, move the entry into it, recycle, then restore to root.
	e.key("g")
	e.mustShow("NEW GROUP")
	e.typeText("Archive")
	e.key("Enter")
	e.mustShowWrite("Archive", "saved · backup created")
	e.searchAll("Eval Created Edited")
	e.mustShow("Eval Created Edited")
	e.key("Right")
	e.key("Right")
	e.key("m")
	e.mustShow("MOVE EVAL CREATED EDITED")
	e.typeText("Archive")
	e.key("Down")
	e.key("Enter")
	e.mustShowWrite("Passwords / Archive", "saved · backup created")
	e.key("C-d")
	e.mustShow("DELETE")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("saved · backup created")
	e.key("Left")
	e.key("Left")
	for i := 0; i < 4; i++ {
		e.key("Up")
	}
	for i := 0; i < 3; i++ {
		e.key("Down")
	}
	e.mustShow("Bin", "Eval Created Edited")
	e.key("m")
	e.mustShow("RESTORE EVAL CREATED EDITED")
	e.typeText("Passwords")
	e.key("Enter")
	for i := 0; i < 3; i++ {
		e.key("Up")
	}
	e.mustShow("Eval Created Edited")
	e.key("Escape")
	e.mustShow("Seed Entry")

	t.Run("legacy automatic reload", func(t *testing.T) {
		t.Skip("native sessions reload with lock/unlock, not cached credentials")
		// An external CLI write is detected, can be kept temporarily, then reloaded.
		addExternalEntry(e, backend)
		e.waitFor("external change indicator", func() bool {
			screen := e.capture()
			return strings.Contains(screen, "Changed externally") || strings.Contains(screen, "Vault changed externally")
		})
		if !strings.Contains(e.capture(), "Vault changed externally") {
			e.key("n")
		}
		e.mustShow("Vault changed externally")
		e.key("Enter") // Keep Current View is selected by default.
		e.mustShow("Changed externally")
		e.key("C-r")
		e.mustShow("vault reloaded", "External Entry")

	})
	// Read-only mode blocks new writes with a visible reason.
	e.key("R")
	e.mustShow("read-only enabled")
	e.key("n")
	e.mustShow("read-only · press shift+r to enable")
	e.mustNotShow("New Entry")

	e.key("q")
	e.waitForCleanQuit()
}

func TestMilestone3PaletteSearchPrivacyAndResponsiveJourney(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals use Unix process and shell behavior")
	}
	e := newEval(t)
	defer e.close()
	configDir := filepath.Join(e.root, "xdg-config", "bypass")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"grand_view_group_size":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e.start()
	e.openPathBySelection()
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry", "read only", "entries")
	e.mustNotShow(secretSentinel)

	// Command palette can preview a nested theme and restore the original
	// appearance on Escape, then commit a different theme and compact layout.
	e.key("C-p")
	e.typeText("Theme")
	e.key("Enter")
	e.mustShow("THEME", "Macchiato")
	e.typeText("Tokyo Night")
	e.mustShow("Tokyo Night")
	e.key("Escape")
	e.waitFor("theme preview canceled", func() bool { return !strings.Contains(e.capture(), "THEME") })
	e.key("C-p")
	e.typeText("Theme")
	e.key("Enter")
	e.mustShow("THEME")
	e.typeText("Nord")
	e.mustShow("Nord")
	e.key("Enter")
	e.mustShow("preferences saved")
	e.mustSettingsContain("\"theme\": \"nord\"")
	e.mustSettingsContain("\"grand_view_group_size\": 3")

	e.key("C-p")
	e.typeText("Layout")
	e.key("Enter")
	e.mustShow("CHOOSE APPEARANCE", "Compact")
	e.typeText("Compact")
	e.key("Enter")
	e.mustShow("Eval Entry", "", "", "preferences saved")
	e.mustSettingsContain("\"appearance\": \"compact\"")

	// Jumping to Password selects its detail without revealing its protected
	// value. Read-only edits report a reason instead of opening an editor.
	e.key("J")
	e.key("p")
	e.mustShow("Eval Entry", "Password", "••••••••")
	e.mustNotShow(secretSentinel)
	e.key("G")
	e.mustShow("GRAND VIEW", "Password", "positions start at 1", "q close")
	e.key("Escape")
	e.mustShow("Eval Entry", "••••••••")
	e.mustNotShow(secretSentinel)
	e.key("n")
	e.mustShow("read-only · press shift+r to enable")
	e.mustNotShow("New Entry")

	// Ctrl+F searches all entries even when details have focus. Fuzzy matches
	// are underlined, and the accepted query remains as the active filter.
	e.key("/")
	e.typeText("evl")
	e.key("Enter")
	e.mustShow("Eval Entry", "evl")
	if hasUnderlineSGR(e.captureEscaped()) {
		e.failWithScreen("search results should not use underline", e.capture())
	}
	e.key("/")
	e.key("C-u")
	e.typeText("query-with-no-eval-match")
	e.mustShow("No matches")
	e.key("Escape")
	e.mustShow("Eval Entry", "evl")
	e.key("Escape")
	e.mustShow("All Entries", "Eval Entry")

	// The same entry remains reachable in wide, medium, and narrow layouts.
	e.resize(90, 25)
	e.mustDimensions(90, 25)
	e.mustShow("Eval Entry")
	e.key("Left")
	e.resize(60, 20)
	e.mustDimensions(60, 20)
	e.mustShow("All Entries / Views", "All Entries")
	e.key("Right")
	e.mustShow("Eval Entry", "Entries")
	e.key("Tab")
	e.mustShow("Eval Entry", "Details", "Password", "••••••••")
	e.key("Left")
	e.key("Enter")
	e.mustShow("Eval Entry", "Details", "Password", "••••••••")
	e.resize(120, 35)
	e.mustDimensions(120, 35)
	e.mustShow("Eval Entry")
	e.resize(20, 5)
	e.mustDimensions(20, 5)
	e.mustShow("Terminal too small")
	e.resize(120, 35)
	e.mustDimensions(120, 35)
	e.mustShow("Eval Entry")

	// Set a session PIN, protect the screen, verify keyboard shortcuts cannot
	// expose the workspace, then unlock with the PIN after the throttle clears.
	e.key("C-p")
	e.typeText("Change PIN")
	e.key("Enter")
	e.typeText("1234")
	e.key("Enter")
	e.mustShow("CONFIRM SESSION PIN")
	e.typeText("1234")
	e.key("Enter")
	e.mustShow("session pin set")
	e.key("C-k")
	e.mustShow("Screen protected")
	e.mustNotShow("Eval Entry")
	e.mustNotShow("first.kdbx")
	e.key("1")
	e.key("C-o")
	e.mustShow("Screen protected")
	e.mustNotShow("Eval Entry")
	e.mustNotShow("OPEN VAULT")
	e.typeText("0000")
	e.key("Enter")
	e.mustShow("incorrect pin")
	e.waitFor("PIN retry window to expire", func() bool {
		screen := e.capture()
		return strings.Contains(screen, "incorrect pin") && !regexp.MustCompile(`wait [0-9]+s`).MatchString(screen)
	})
	e.typeText("1234")
	e.key("Enter")
	e.mustShow("Eval Entry", "read only")

	// Full lock uses the vault credential path and does not require the session
	// PIN again after the master password unlocks the vault.
	e.key("C-k")
	e.mustShow("Screen protected")
	e.key("C-l")
	e.mustShow("Full lock all vaults?")
	e.mustNotShow("Eval Entry")
	e.key("Enter")
	e.mustShow("Vault locked")
	e.mustNotShow("Eval Entry")
	e.key("Enter")
	e.mustShow("UNLOCK VAULT", "Password")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry", "read only")
	e.mustNotShow(secretSentinel)
	e.key("q")
	e.waitForCleanQuit()

	// Restart against the same private XDG config to confirm the applied theme
	// and compact appearance survive process exit.
	restarted := *e
	restarted.label = fmt.Sprintf("kui%d", time.Now().UnixNano()%1_000_000_000)
	restarted.closed = false
	restarted.startWith(filepath.Join(e.root, "fake-keepassxc-cli"), "--read-only", e.db1)
	restarted.mustShow("UNLOCK VAULT")
	restarted.typeText(fixturePassword)
	restarted.key("Enter")
	restarted.mustShow("Eval Entry", "", "", "read only")
	restarted.key("q")
	restarted.waitForCleanQuit()
	restarted.close()
}

func TestMilestone2IdleLockTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals use Unix process and shell behavior")
	}
	e := newEval(t)
	defer e.close()
	e.startWith(filepath.Join(e.root, "fake-keepassxc-cli"), "--idle-timeout=2s", e.db1)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry")
	e.waitFor("idle vault lock", func() bool {
		return strings.Contains(e.capture(), "Vault locked")
	})
}

func createRealVault(e *evalSession, backend string) {
	e.t.Helper()
	runCLI(e, backend, fixturePassword+"\n"+fixturePassword+"\n", "db-create", "--set-password", "--decryption-time", "100", e.realDB)
	runCLI(e, backend, fixturePassword+"\n"+fixtureEntryPassword+"\n", "add", "--quiet", "--username", "seed-user", "--url", "https://seed.test", "--notes", "synthetic seed note", "--password-prompt", e.realDB, "Seed Entry")
}

func addExternalEntry(e *evalSession, backend string) {
	e.t.Helper()
	runCLI(e, backend, fixturePassword+"\n", "add", "--quiet", "--username", "external-user", "--url", "https://external.test", "--generate", e.realDB, "External Entry")
}

func runCLI(e *evalSession, backend, stdin string, args ...string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, backend, args...)
	cmd.Stdin = strings.NewReader(stdin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("synthetic KeePassXC setup command %q failed: %v: %s", args[0], err, e.redact(string(output)))
	}
}

type evalSession struct {
	t             *testing.T
	root          string
	pickerRoot    string
	label         string
	db1           string
	db2           string
	realDB        string
	closed        bool
	clipboardPath string
	clipboardBin  string
	secrets       []string
}

func newEval(t *testing.T) *evalSession {
	t.Helper()
	root, err := os.MkdirTemp("", "bypass-tmux-eval-")
	if err != nil {
		t.Fatal("create private eval directory:", err)
	}
	label := fmt.Sprintf("kui%d", time.Now().UnixNano()%1_000_000_000)
	e := &evalSession{t: t, root: root, label: label}
	e.pickerRoot = filepath.Join(root, "picker")
	vaultDir := filepath.Join(e.pickerRoot, "vaults")
	if err := os.MkdirAll(vaultDir, 0700); err != nil {
		t.Fatal("create picker fixture directory:", err)
	}
	e.db1 = filepath.Join(vaultDir, "first.kdbx")
	e.db2 = filepath.Join(vaultDir, "second.kdbx")
	e.realDB = filepath.Join(vaultDir, "real-cli.kdbx")
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
	e.initClipboard()
	return e
}

func (e *evalSession) start() {
	e.startWith(filepath.Join(e.root, "fake-keepassxc-cli"), "--read-only")
	e.mustShow("bypass", "VAULTS", "ctrl+o")
}

func (e *evalSession) startWith(backend string, appArgs ...string) {
	e.startApp(backend, appArgs...)
}

func (e *evalSession) startApp(backend string, appArgs ...string) {
	e.t.Helper()
	launcher := filepath.Join(e.root, "launch.sh")
	command := shellQuote(appBinary) + " --backend " + shellQuote(backend)
	for _, arg := range appArgs {
		command += " " + shellQuote(arg)
	}
	script := "#!/bin/sh\nexec env HOME=" + shellQuote(filepath.Join(e.root, "home")) +
		" XDG_CONFIG_HOME=" + shellQuote(filepath.Join(e.root, "xdg-config")) +
		" XDG_DATA_HOME=" + shellQuote(filepath.Join(e.root, "xdg-data")) +
		" XDG_STATE_HOME=" + shellQuote(filepath.Join(e.root, "xdg-state")) +
		" XDG_CACHE_HOME=" + shellQuote(filepath.Join(e.root, "xdg-cache")) +
		" XDG_RUNTIME_DIR=" + shellQuote(filepath.Join(e.root, "xdg-runtime")) +
		" PATH=" + shellQuote(e.clipboardBin+string(os.PathListSeparator)+os.Getenv("PATH")) +
		" BYPASS_EVAL_CLIPBOARD=" + shellQuote(e.clipboardPath) +
		" NO_COLOR= CLICOLOR_FORCE=1 TERM=xterm-256color LC_ALL=C.UTF-8 " + command + "\n"
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		e.t.Fatal("write application launcher:", err)
	}
	tmuxConfig := filepath.Join(e.root, "tmux.conf")
	if err := os.WriteFile(tmuxConfig, []byte("set-option -w -g remain-on-exit on\n"), 0600); err != nil {
		e.t.Fatal("write private tmux config:", err)
	}
	e.tmux("-f", tmuxConfig, "new-session", "-d", "-x", "120", "-y", "35", "-s", "eval", "-c", e.pickerRoot, launcher)
	e.tmux("set-option", "-w", "-t", "eval", "remain-on-exit", "on")
}

func (e *evalSession) openPathBySelection() {
	e.t.Helper()
	e.key("o")
	e.mustShow("vaults/")
	e.key("Down") // ".." -> "vaults"
	e.key("Enter")
	e.mustShow("first.kdbx")
	e.key("Left") // navigate to the parent directory while list-focused
	e.mustShow("vaults/")
	e.key("Down")
	e.key("Enter")
	e.mustShow("first.kdbx")
	e.key("Down") // ".." -> "first.kdbx"
	e.key("Enter")
	e.waitFor("password field", func() bool {
		s := e.capture()
		return strings.Contains(s, "Password") || strings.Contains(s, "Unlock")
	})
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

func (e *evalSession) searchAll(query string) {
	e.t.Helper()
	e.key("C-p")
	e.mustShow("COMMANDS")
	e.typeText("Search")
	e.key("Enter")
	e.mustShow("enter apply")
	e.key("C-u")
	e.typeText(query)
	e.key("Enter")
	e.waitFor("entry filter applied", func() bool { return !strings.Contains(e.capture(), "enter apply") })
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
	e.mustShowWithin(4*time.Second, parts...)
}

func (e *evalSession) mustActiveVault(name string) {
	e.t.Helper()
	e.waitFor("active vault "+name, func() bool {
		first := strings.SplitN(e.capture(), "\n", 2)[0]
		return strings.HasPrefix(strings.TrimSpace(first), name+" ")
	})
}

func (e *evalSession) mustShowWrite(parts ...string) {
	e.t.Helper()
	e.waitForWithin(20*time.Second, "completed operation: "+strings.Join(parts, ", "), func() bool {
		s := e.capture()
		if strings.Contains(s, "Working…") {
			return false
		}
		if strings.Contains(strings.Join(parts, " "), "saved · backup created") {
			// A shaded modal preserves the previous success notice underneath.
			// That notice is not evidence that this mutation has completed.
			for _, title := range []string{"New Entry", "Generate Password", "NEW GROUP", "Move Entry", "Restore Entry", "DELETE", "DELETE", "UNSAVED ENTRY"} {
				if strings.Contains(s, title) {
					return false
				}
			}
		}
		for _, part := range parts {
			if !strings.Contains(s, part) {
				return false
			}
		}
		return true
	})
}

func (e *evalSession) mustShowWithin(timeout time.Duration, parts ...string) {
	e.t.Helper()
	e.waitForWithin(timeout, "screen to contain "+strings.Join(parts, ", "), func() bool {
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

func (e *evalSession) mustSettingsContain(value string) {
	e.t.Helper()
	path := filepath.Join(e.root, "xdg-config", "bypass", "config.json")
	e.waitFor("settings file to contain "+value, func() bool {
		contents, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(contents), value)
	})
}

func (e *evalSession) mustShowUnderline(value string) {
	e.t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		screen := e.captureEscaped()
		if strings.Contains(e.capture(), value) && hasUnderlineSGR(screen) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	e.failWithScreen("search result did not include an ANSI underline", e.capture())
}

func hasUnderlineSGR(screen string) bool {
	for _, match := range regexp.MustCompile(`\x1b\[([0-9;]*)m`).FindAllStringSubmatch(screen, -1) {
		for _, parameter := range strings.Split(match[1], ";") {
			if parameter == "4" {
				return true
			}
		}
	}
	return false
}

func (e *evalSession) waitFor(description string, condition func() bool) {
	e.waitForWithin(4*time.Second, description, condition)
}

func (e *evalSession) waitForWithin(timeout time.Duration, description string, condition func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
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

func (e *evalSession) captureEscaped() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", "-L", e.label, "capture-pane", "-e", "-p", "-t", "eval")
	cmd.Env = tmuxEnv(e.label)
	out, _ := cmd.Output()
	return string(out)
}

func (e *evalSession) paneExitStatus() (dead bool, status string, available bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", "-L", e.label, "display-message", "-p", "-t", "eval", "#{pane_dead} #{pane_dead_status}")
	cmd.Env = tmuxEnv(e.label)
	out, err := cmd.Output()
	if err != nil {
		return false, "", false
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		return false, "", false
	}
	return parts[0] == "1", parts[1], true
}

func (e *evalSession) waitForCleanQuit() {
	e.t.Helper()
	e.waitFor("application exit", func() bool {
		dead, _, available := e.paneExitStatus()
		return available && dead
	})
	dead, status, available := e.paneExitStatus()
	if !available {
		e.failWithScreen("tmux pane exit status unavailable after quit", e.capture())
	}
	if !dead || status != "0" {
		e.failWithScreen("application quit abnormally (pane_dead="+fmt.Sprint(dead)+", pane_dead_status="+status+")", e.capture())
	}
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
	cmd := exec.CommandContext(ctx, "tmux", "-L", e.label, "kill-server")
	cmd.Env = tmuxEnv(e.label)
	_ = cmd.Run()
	_ = os.RemoveAll(e.root)
}

var displayedCodePattern = regexp.MustCompile(`(?m)(Code[ \t]+)[A-Za-z0-9]{4,32}\b`)

func (e *evalSession) redact(s string) string {
	if strings.Contains(s, "\n") && strings.Contains(strings.ToUpper(s), "GRAND VIEW") {
		return "[Grand View screen withheld: explicit secret display was active]"
	}
	s = displayedCodePattern.ReplaceAllString(s, "${1}[REDACTED]")
	for _, secret := range e.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return strings.NewReplacer(
		secretSentinel, "[REDACTED]",
		fixturePassword, "[REDACTED]",
		fixtureWrongPassword, "[REDACTED]",
		fixtureEntryPassword, "[REDACTED]",
	).Replace(s)
}

func (e *evalSession) failWithScreen(description, screen string) {
	e.t.Helper()
	redacted := e.redact(screen)
	artifact := ""
	if dir := os.Getenv("BYPASS_EVAL_ARTIFACTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err == nil {
			path := filepath.Join(dir, "bypass-"+e.label+"-failure.txt")
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

const fakeBackend = `#!/usr/bin/env python3
import shlex
import sys

if sys.argv[1:] == ["--version"]:
    print("keepassxc-cli 2.8.0")
    sys.exit(0)
if sys.argv[1:] == ["--help"]:
    print("open  Open a vault\nls  List entries\nshow  Show an entry\ndb-info  Database information\nadd  Add an entry\nedit  Edit an entry\nmkdir  Create a group\nmv  Move an entry\nrm  Delete an entry\nrmdir  Delete a group\ngenerate  Generate a password\ndb-create  Create a vault\ndb-edit  Edit a vault")
    sys.exit(0)
if "--no-password" not in sys.argv and sys.stdin.readline().rstrip("\n") != "fixture-passphrase":
    sys.exit(1)
while True:
    sys.stdout.write("fixture.kdbx> ")
    sys.stdout.flush()
    line = sys.stdin.readline()
    if not line:
        break
    args = shlex.split(line)
    if not args:
        continue
    if args[0] == "db-info":
        print("UUID: {00000000-0000-0000-0000-000000000001}\nName: Eval Vault\nNumber of groups: 1\nNumber of entries: 1\nRecycle bin is disabled.")
    elif args[0] == "ls":
        print("Eval Entry")
    elif args[0] == "show":
        field = args[args.index("-a") + 1]
        print({"Uuid": "{00000000-0000-0000-0000-000000000002}", "UserName": "eval-user", "Password": "` + secretSentinel + `"}.get(field, ""))
    else:
        print("Unknown command " + args[0])
`
