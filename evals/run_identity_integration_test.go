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

func TestRunIdentityNotForwardedWithoutReferences(t *testing.T) {
	t.Parallel()
	shell, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "environment")
	if err := os.WriteFile(file, []byte("BYPASS_IDENTITY=bypass://unavailable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, appBinary, "run", "--env-file", file, "--", shell, "-c", `test -z "${BYPASS_IDENTITY+x}"`)
	cmd.Dir = root
	cmd.Env = []string{"HOME=" + root, "BYPASS_IDENTITY=dev-rw-" + strings.Repeat("a1", 16)}
	if err := cmd.Run(); err != nil {
		t.Fatal("identity config was resolved or forwarded when no secrets were requested")
	}
}

func TestRunIdentityGrantIsolationTmux(t *testing.T) {
	t.Parallel()
	e := newEval(t)
	defer e.close()
	createRealVault(e)
	e.startWith(e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")

	// A directly launched script supports session approvals, and asserts that the
	// recipient receives its requested value but never the identity credential.
	recipient := filepath.Join(e.pickerRoot, "identity-recipient")
	script := "#!/bin/sh\ntest \"$RUN_SECRET\" = \"$EXPECTED\" && test -z \"${BYPASS_IDENTITY+x}\" || exit 1\nprintf approved\n"
	if err := os.WriteFile(recipient, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	recipient, err := filepath.EvalSymlinks(recipient)
	if err != nil {
		t.Fatal(err)
	}
	dev := "dev-rw-" + strings.Repeat("a1", 16)
	prod := "prod-ro-" + strings.Repeat("b2", 16)
	otherDev := "dev-rw-" + strings.Repeat("c3", 16)
	for _, identity := range []string{dev, prod, otherDev} {
		e.secrets = append(e.secrets, identity, identity[strings.LastIndexByte(identity, '-')+1:])
	}
	launch := func(identity string, flag bool) (*exec.Cmd, *bytes.Buffer) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(cancel)
		args := []string{"run", "--timeout", "15s"}
		if flag {
			args = append(args, "--identity", identity)
		}
		args = append(args, "--", recipient)
		cmd := exec.CommandContext(ctx, appBinary, args...)
		cmd.Dir = e.pickerRoot
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "BYPASS_IDENTITY=") {
				cmd.Env = append(cmd.Env, item)
			}
		}
		cmd.Env = append(cmd.Env, "HOME="+filepath.Join(e.root, "home"), "XDG_CACHE_HOME="+filepath.Join(e.root, "xdg-cache"), "XDG_CONFIG_HOME="+filepath.Join(e.root, "xdg-config"), "RUN_SECRET=bypass://Seed Entry", "EXPECTED="+fixtureEntryPassword)
		if !flag && identity != "" {
			cmd.Env = append(cmd.Env, "BYPASS_IDENTITY="+identity)
		}
		out := new(bytes.Buffer)
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal("could not start identity request")
		}
		return cmd, out
	}
	assertHidden := func(identity string) {
		t.Helper()
		if strings.Contains(e.capture(), identity) || strings.Contains(e.capture(), identity[strings.LastIndexByte(identity, '-')+1:]) {
			t.Fatal("identity credential appeared in the TUI")
		}
	}
	cmd, out := launch(dev, false)
	e.mustShow("secret request pending")
	e.mustShow("dev-rw")
	assertHidden(dev)
	e.mustShow("Approve for this session")
	assertHidden(dev)
	e.key("s")
	if err := cmd.Wait(); err != nil || out.String() != "approved" {
		t.Fatal("identity approval failed or bearer reached the recipient")
	}
	e.key("S")
	e.mustShow("No pending requests")
	cmd, out = launch(dev, false)
	if err := cmd.Wait(); err != nil || out.String() != "approved" {
		t.Fatal("saved identity grant did not authorize a repeat request")
	}
	e.mustShow("No pending requests")
	otherRecipient := filepath.Join(filepath.Dir(recipient), "identity-recipient-second")
	if err := os.WriteFile(otherRecipient, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	recipient = otherRecipient
	cmd, out = launch(dev, false)
	if err := cmd.Wait(); err != nil || out.String() != "approved" {
		t.Fatal("identity grant did not apply to another command")
	}
	e.mustShow("No pending requests")
	for _, tc := range []struct {
		identity string
		name     string
		flag     bool
		allowed  bool
	}{{prod, "prod-ro", true, false}, {otherDev, "dev-rw", false, true}, {"", "", false, false}} {
		cmd, out := launch(tc.identity, tc.flag)
		if tc.allowed {
			if err := cmd.Wait(); err != nil || out.String() != "approved" {
				t.Fatal("same agent name did not reuse its approved item")
			}
			e.mustShow("No pending requests")
			continue
		}
		e.mustShow("REVIEW REQUEST")
		if tc.name != "" {
			e.mustShow(tc.name)
			assertHidden(tc.identity)
		}
		e.key("d")
		if err := cmd.Wait(); err == nil {
			t.Fatal("another identity or anonymous request inherited an identity grant")
		}
		if !strings.Contains(out.String(), "denied") {
			e.failWithScreen("request did not receive the denial: "+out.String(), e.capture())
		}
		e.key("S")
		e.mustShow("No pending requests")
	}
	e.key("Escape")
	e.key("L")
	e.mustShow("Vault locked")
	e.key("Enter")
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	cmd, out = launch(dev, false)
	e.mustShow("secret request pending")
	e.mustShow("dev-rw")
	e.mustShow("REVIEW REQUEST")
	e.key("d")
	if err := cmd.Wait(); err == nil || !strings.Contains(out.String(), "denied") {
		t.Fatal("vault lock did not revoke session permission")
	}

}
