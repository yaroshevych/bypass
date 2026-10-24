// Command mutations checks that critical tests reject deliberately broken
// guards. Go source overlays keep the working tree untouched.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type mutation struct {
	name, file, before, after, pkg, test, failure string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("run from the Bypass Go module root")
	}
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		return fmt.Errorf("mutation checks require keepassxc-cli, including the native backup-failure check")
	}
	checks := []mutation{
		{name: "clipboard ownership", file: "internal/clipboard/manager.go", before: "if !bytes.Equal(current, m.owned) {", after: "if false && !bytes.Equal(current, m.owned) {", pkg: "./internal/clipboard", test: "^TestClearPreservesClipboardWhenAnotherAppReplacesValue$", failure: "TestClearPreservesClipboardWhenAnotherAppReplacesValue"},
		{name: "read-only writes", file: "internal/ui/operations.go", before: "if s.ReadOnly {", after: "if false && s.ReadOnly {", pkg: "./internal/ui", test: "^TestReadOnlyAndStaleWritesBlocked$", failure: "TestReadOnlyAndStaleWritesBlocked"},
		{name: "PIN during pending work", file: "internal/ui/model.go", before: "if key == \"ctrl+k\" {", after: "if key == \"ctrl+k\" && !m.writing {", pkg: "./internal/ui", test: "^TestPINProtectsPendingSaveWithoutCancelingIt$", failure: "TestPINProtectsPendingSaveWithoutCancelingIt"},
		{name: "late authentication after lock", file: "internal/ui/operations.go", before: "func (m *Model) cancelPendingOpen() {\n\tm.request++", after: "func (m *Model) cancelPendingOpen() {\n\t// intentionally omit request invalidation", pkg: "./internal/ui", test: "^TestPublicFullLockDropsPendingSecondaryOpen$", failure: "TestPublicFullLockDropsPendingSecondaryOpen"},
		{name: "backup required before credential commit", file: "internal/vault/lifecycle.go", before: "if err := createBackup(backupDir, path, initialRevision); err != nil {\n\t\treturn nil, ErrBackup\n\t}", after: "if err := createBackup(backupDir, path, initialRevision); err != nil {\n\t\t// intentionally ignore backup failure\n\t}", pkg: "./internal/vault", test: "^TestCredentialChangeFailurePathsPreserveOriginalVault$/^backup_failure$", failure: "TestCredentialChangeFailurePathsPreserveOriginalVault/backup_failure"},
	}
	dir, err := os.MkdirTemp("", "bypass-mutations-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for index, check := range checks {
		originalPath := filepath.Join(root, check.file)
		source, err := os.ReadFile(originalPath)
		if err != nil {
			return err
		}
		if !strings.Contains(string(source), check.before) {
			return fmt.Errorf("%s: mutation anchor changed; update the probe", check.name)
		}
		if output, err := runTest(root, check, ""); err != nil {
			return fmt.Errorf("%s: baseline must pass first:\n%s", check.name, output)
		}
		mutantPath := filepath.Join(dir, fmt.Sprintf("mutant-%d.go", index))
		mutated := strings.Replace(string(source), check.before, check.after, 1)
		if err := os.WriteFile(mutantPath, []byte(mutated), 0o600); err != nil {
			return err
		}
		overlayPath := filepath.Join(dir, fmt.Sprintf("overlay-%d.json", index))
		overlay, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{originalPath: mutantPath}})
		if err != nil {
			return err
		}
		if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
			return err
		}
		output, err := runTest(root, check, overlayPath)
		if err == nil {
			return fmt.Errorf("%s: broken guard survived its test", check.name)
		}
		if !strings.Contains(output, "--- FAIL: "+check.failure) {
			return fmt.Errorf("%s: expected behavioral failure, got a build/tool failure instead:\n%s", check.name, output)
		}
		fmt.Printf("caught: %s\n", check.name)
	}
	fmt.Printf("%d/%d critical guard mutations caught; source tree unchanged\n", len(checks), len(checks))
	return nil
}

func runTest(root string, check mutation, overlay string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"test", "-count=1", "-timeout=60s", "-run", check.test}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	args = append(args, check.pkg)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	return string(output), err
}
