//go:build integration

package evals

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunDialogsBackDefaultAndResponsiveTmux(t *testing.T) {
	t.Parallel()
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Fatal(err)
	}
	shell, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			e := newEval(t)
			defer e.close()
			createRealVault(e, backend)
			e.writeConfig(map[string]any{"appearance": appearance})
			e.startWith(backend, e.realDB)
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShow("Seed Entry")
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			identity := "dev-rw-" + strings.Repeat("a1", 16)
			e.secrets = append(e.secrets, identity)
			cmd := exec.CommandContext(ctx, appBinary, "run", "--timeout", "40s", "--", shell, "-c", `test "$RUN_SECRET" = "$EXPECTED" && printf approved`)
			cmd.Dir = e.pickerRoot
			cmd.Env = append(os.Environ(), "HOME="+filepath.Join(e.root, "home"), "XDG_CACHE_HOME="+filepath.Join(e.root, "xdg-cache"), "XDG_CONFIG_HOME="+filepath.Join(e.root, "xdg-config"), "BYPASS_IDENTITY="+identity, "RUN_SECRET=bypass://Seed Entry", "EXPECTED="+fixtureEntryPassword)
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			if err := cmd.Start(); err != nil {
				t.Fatal("could not start request")
			}
			e.mustShow("secret request pending")
			e.key("C-b")
			snapshot := func(page string, width, height int) {
				if dir := os.Getenv("BYPASS_REQUEST_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(dir, fmt.Sprintf("%s-%s-%dx%d.ansi", page, appearance, width, height))
					if err := os.WriteFile(path, []byte(e.redact(e.captureEscaped())), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, size := range [][2]int{{48, 16}, {80, 24}, {120, 35}} {
				e.tmux("resize-window", "-t", "eval", "-x", fmt.Sprint(size[0]), "-y", fmt.Sprint(size[1]))
				e.waitFor("request dialog resized", func() bool {
					lines := strings.Split(strings.TrimSuffix(e.capture(), "\n"), "\n")
					return len(lines) == size[1] && strings.Contains(e.capture(), "Pending secret requests")
				})
				e.mustShow("Pending secret requests", "Deny all", "Approve all", "Close")
				snapshot("requests", size[0], size[1])
				e.key("Enter")
				e.mustShow("REVIEW REQUEST", "Deny", "Allow for this identity", "Back")
				e.mustNotShow(fixtureEntryPassword)
				snapshot("review", size[0], size[1])
				e.key("Enter")
				e.waitFor("default Back restores request list", func() bool { return !strings.Contains(e.capture(), "REVIEW REQUEST") })
				e.mustShow("Pending secret requests (1)")
			}
			e.key("Enter")
			e.mustShow("REVIEW REQUEST")
			e.key("a")
			if err := cmd.Wait(); err != nil || out.String() != "approved" {
				t.Fatal("request was lost or resolved before explicit Allow")
			}
		})
	}
}
