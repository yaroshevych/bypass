//go:build integration

package evals

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunApprovalAndDenialTmux(t *testing.T) {
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Fatal(err)
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)
	e.startWith(backend, e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	// /bin/sh is commonly a symlink on Linux; use a physical executable path.
	shell, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	launch := func() (*exec.Cmd, *bytes.Buffer) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, appBinary, "run", "--timeout", "15s", "--", shell, "-c", `test "$RUN_SECRET" = "$EXPECTED" && printf approved`)
		cmd.Dir = e.pickerRoot
		cmd.Env = append(os.Environ(), "HOME="+filepath.Join(e.root, "home"), "XDG_CACHE_HOME="+filepath.Join(e.root, "xdg-cache"), "XDG_CONFIG_HOME="+filepath.Join(e.root, "xdg-config"), "RUN_SECRET=bypass://Seed Entry", "EXPECTED="+fixtureEntryPassword)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, &out
	}
	cmd, out := launch()
	e.mustShow("secret request pending")
	e.key("C-p")
	e.typeText("Pending Secret Requests")
	e.key("Enter")
	e.mustShow("Pending secret requests")
	e.key("Enter")
	e.mustShow("REVIEW REQUEST")
	// Enter must activate the default Back button, never grant access.
	e.key("Enter")
	e.waitFor("review returns to request list", func() bool { return !strings.Contains(e.capture(), "REVIEW REQUEST") })
	e.mustShow("Pending secret requests")
	e.key("Enter")
	e.mustShow("REVIEW REQUEST")
	e.key("a")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("approved run failed: %v: %s", err, e.redact(out.String()))
	}
	if out.String() != "approved" {
		t.Fatal("child did not confirm injected secret")
	}
	e.key("Escape")
	e.waitFor("return to vault", func() bool { return !strings.Contains(e.capture(), "Pending secret requests") })
	cmd, out = launch()
	e.mustShow("secret request pending")
	e.key("C-b")
	e.mustShow("Pending secret requests")
	e.key("d")
	if err := cmd.Wait(); err == nil {
		t.Fatal("denied child ran")
	}
	if !strings.Contains(out.String(), "denied") {
		t.Fatalf("unexpected denial: %s", e.redact(out.String()))
	}
	first, firstOut := launch()
	e.mustShow("Pending secret requests (1)")
	second, secondOut := launch()
	e.mustShow("Pending secret requests (2)")
	e.key("Down")
	e.key("d")
	if err := second.Wait(); err == nil || !strings.Contains(secondOut.String(), "denied") {
		t.Fatal("could not deny second request before first")
	}
	e.key("Enter")
	e.mustShow("REVIEW REQUEST")
	e.key("a")
	if err := first.Wait(); err != nil || firstOut.String() != "approved" {
		t.Fatalf("first queued request failed: %v: %s", err, e.redact(firstOut.String()))
	}

}

func TestRunIdentityGrantAndTimeoutTmux(t *testing.T) {
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Fatal(err)
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)
	e.startWith(backend, e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	executable, err := exec.LookPath("printenv")
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	directory := e.pickerRoot
	identity := "repeat-" + strings.Repeat("d4", 16)
	e.secrets = append(e.secrets, identity)
	launch := func(timeout string) (*exec.Cmd, *bytes.Buffer) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, appBinary, "run", "--timeout", timeout, "--", executable, "RUN_SECRET")
		cmd.Dir = directory
		cmd.Env = append(os.Environ(), "HOME="+filepath.Join(e.root, "home"), "XDG_CACHE_HOME="+filepath.Join(e.root, "xdg-cache"), "XDG_CONFIG_HOME="+filepath.Join(e.root, "xdg-config"), "RUN_SECRET=bypass://Seed Entry", "BYPASS_IDENTITY="+identity)
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, out
	}
	cmd, out := launch("15s")
	e.mustShow("secret request pending")
	e.key("C-b")
	e.mustShow("Pending secret requests")
	e.key("Enter")
	e.mustShow("Allow for this identity")
	e.key("i")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("persistent approval failed: %v: %s", err, e.redact(out.String()))
	}
	if strings.TrimSpace(out.String()) != fixtureEntryPassword {
		t.Fatal("persistent approval did not deliver field")
	}
	cmd, out = launch("5s")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("saved grant did not authorize repeat request: %v: %s screen=%s", err, e.redact(out.String()), e.capture())
	}
	if strings.TrimSpace(out.String()) != fixtureEntryPassword {
		t.Fatal("repeat request did not deliver field")
	}
	directory = e.root
	cmd, out = launch("5s")
	if err := cmd.Wait(); err != nil || strings.TrimSpace(out.String()) != fixtureEntryPassword {
		t.Fatal("identity grant did not apply in another directory")
	}
	identity = "other-" + strings.Repeat("e5", 16)
	e.secrets = append(e.secrets, identity)
	cmd, out = launch("300ms")
	if err := cmd.Wait(); err == nil {
		t.Fatal("different identity inherited the grant")
	}
	if !strings.Contains(out.String(), "timed out") {
		t.Fatalf("expected timeout: %s", e.redact(out.String()))
	}
	directory = e.pickerRoot
	identity = "repeat-" + strings.Repeat("d4", 16)
	e.mustShow("Pending secret requests (0)")
	e.key("r")
	e.mustShow("RESET SAVED PERMISSIONS", "Cancel")
	// Reset also has a safe default: Enter cancels without revoking access.
	e.key("Enter")
	e.waitFor("reset canceled", func() bool { return !strings.Contains(e.capture(), "RESET SAVED PERMISSIONS") })
	cmd, out = launch("5s")
	if err := cmd.Wait(); err != nil || strings.TrimSpace(out.String()) != fixtureEntryPassword {
		t.Fatal("canceling reset revoked a saved permission")
	}
	e.mustShow("Pending secret requests (0)")
	e.key("r")
	e.mustShow("RESET SAVED PERMISSIONS")
	e.key("C-s")
	e.waitFor("saved permissions reset", func() bool { return !strings.Contains(e.capture(), "RESET SAVED PERMISSIONS") })
	cmd, out = launch("500ms")
	if err := cmd.Wait(); err == nil {
		t.Fatal("revoked permission still authorized a request")
	}
	if !strings.Contains(out.String(), "timed out") {
		t.Fatalf("revoked request: %s", e.redact(out.String()))
	}

}

func TestRunMultiVaultSelectionTmux(t *testing.T) {
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Fatal(err)
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)
	encrypted, err := os.ReadFile(e.realDB)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.db2, encrypted, 0600); err != nil {
		t.Fatal(err)
	}
	e.startWith(backend, e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.openPath(e.db2)
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.mustActiveVault("second.kdbx")
	e.key("[")
	e.mustActiveVault("real-cli.kdbx")
	e.key("]")
	e.mustActiveVault("second.kdbx")
	shell, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	launch := func(refs []string) (*exec.Cmd, *bytes.Buffer) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, appBinary, "run", "--timeout", "15s", "--", shell, "-c", `test "$ONE" = "$EXPECTED" && test "$TWO" = "$EXPECTED" && printf approved`)
		cmd.Dir = e.pickerRoot
		cmd.Env = append(os.Environ(), "HOME="+filepath.Join(e.root, "home"), "XDG_CACHE_HOME="+filepath.Join(e.root, "xdg-cache"), "XDG_CONFIG_HOME="+filepath.Join(e.root, "xdg-config"), "EXPECTED="+fixtureEntryPassword)
		cmd.Env = append(cmd.Env, refs...)
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, out
	}
	cmd, out := launch([]string{"ONE=bypass://Seed Entry", "TWO=bypass://real-cli.kdbx/Seed Entry"})
	e.mustShow("secret request pending")
	e.key("C-b")
	e.mustShow("Pending secret requests")
	e.key("Enter")
	e.mustShow("Select vault for this reference")
	e.key("Tab")
	e.key("Enter")
	e.mustShow("Allow for this identity")
	e.key("a")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("vault selection failed: %v: %s", err, e.redact(out.String()))
	}
	if out.String() != "approved" {
		t.Fatal("selected batch did not resolve")
	}
	e.key("Escape")
	e.waitFor("return to vault", func() bool { return !strings.Contains(e.capture(), "Pending secret requests") })
	cmd, out = launch([]string{"ONE=bypass://second.kdbx/Seed Entry", "TWO=bypass://real-cli.kdbx/Seed Entry"})
	e.mustShow("secret request pending")
	e.key("C-b")
	e.mustShow("Pending secret requests")
	e.key("Enter")
	e.mustShow("REVIEW REQUEST")
	e.key("a")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("multi-vault batch failed: %v: %s", err, e.redact(out.String()))
	}
	if out.String() != "approved" {
		t.Fatal("multi-vault batch did not resolve")
	}
}

func TestRunChildSurvivesVaultLockAndTUIClose(t *testing.T) {
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Fatal(err)
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)
	e.startWith(backend, e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	shell, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(e.root, "child-started")
	release := filepath.Join(e.root, "child-release")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, appBinary, "run", "--timeout", "15s", "--", shell, "-c", `touch "$MARKER"; while ! test -f "$RELEASE"; do sleep 0.02; done; test "$RUN_SECRET" = "$EXPECTED" && printf survived`)
	cmd.Dir = e.pickerRoot
	cmd.Env = append(os.Environ(), "HOME="+filepath.Join(e.root, "home"), "XDG_CACHE_HOME="+filepath.Join(e.root, "xdg-cache"), "XDG_CONFIG_HOME="+filepath.Join(e.root, "xdg-config"), "RUN_SECRET=bypass://Seed Entry", "EXPECTED="+fixtureEntryPassword, "MARKER="+marker, "RELEASE="+release)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e.mustShow("secret request pending")
	e.key("C-b")
	e.mustShow("Pending secret requests")
	e.key("Enter")
	e.mustShow("REVIEW REQUEST")
	e.key("a")
	e.waitFor("approved child starts", func() bool { _, err := os.Stat(marker); return err == nil })
	e.key("Escape")
	e.waitFor("return to vault", func() bool { return !strings.Contains(e.capture(), "Pending secret requests") })
	e.key("C-l")
	e.mustShow("Vault locked")
	e.key("C-c")
	e.waitForCleanQuit()
	if err := os.WriteFile(release, []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child interrupted by vault or TUI: %v: %s", err, e.redact(out.String()))
	}
	if out.String() != "survived" {
		t.Fatal("child lost its environment")
	}
}
