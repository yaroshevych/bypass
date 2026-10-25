//go:build integration

package evals

import (
	"bypass/internal/vaulttest"
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestFailedWritesKeepVaultAndDraftAndAllowRetryTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	for _, failure := range []string{"native-command", "backup", "file-permissions"} {
		t.Run(failure, func(t *testing.T) {
			if failure != "backup" {
				t.Skip("legacy standalone-command/staged-save failure injection; native session failures have dedicated coverage")
			}
			e := newEval(t)
			defer e.close()
			createRealVault(e, backend)
			initial := vaultFileHash(t, e.realDB)
			appBackend := backend
			var repair func()
			switch failure {
			case "native-command":
				marker := filepath.Join(e.root, "fail-edit")
				if err := os.WriteFile(marker, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				appBackend = filepath.Join(e.root, "failing-backend")
				script := "#!/bin/sh\nif [ \"$1\" = edit ] && [ -f " + shellQuote(marker) + " ]; then\n  printf '%s\\n' " + shellQuote(secretSentinel) + " >&2\n  exit 1\nfi\nexec " + shellQuote(backend) + " \"$@\"\n"
				if err := os.WriteFile(appBackend, []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
				repair = func() {
					if err := os.Remove(marker); err != nil {
						t.Fatal(err)
					}
				}
			case "backup":
				backup := filepath.Join(e.root, "nonprivate-backups")
				if err := os.WriteFile(backup, []byte("blocked backup directory"), 0600); err != nil {
					t.Fatal(err)
				}
				e.writeConfig(map[string]any{"backup_dir": backup})
				repair = func() {
					if err := os.Remove(backup); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(backup, 0700); err != nil {
						t.Fatal(err)
					}
				}

			case "file-permissions":
				if err := os.Chmod(e.realDB, 0o400); err != nil {
					t.Fatal(err)
				}
				repair = func() {
					if err := os.Chmod(e.realDB, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			e.startWith(appBackend, e.realDB)
			e.mustShow("UNLOCK VAULT")
			e.typeText(fixturePassword)
			e.key("Enter")
			e.mustShow("Seed Entry")
			if failure == "file-permissions" {
				e.mustShow("read only")
				e.key("e")
				e.mustShow("read-only · press shift+r to enable")
				e.key("R")
				e.mustShow("writes enabled")
			}
			e.key("e")
			e.mustShow("Generate Password")
			e.typeText(" Draft")
			e.mustShow("Seed Entry *", "Seed Entry Draft")
			e.key("C-s")
			wantError := "Save failed"
			if failure == "backup" {
				wantError = "backup failed"
			}
			if failure == "file-permissions" {
				wantError = "read only"
			}
			e.mustShowWrite(wantError, "Seed Entry *", "Seed Entry Draft")
			e.mustNotShow(secretSentinel)
			if vaultFileHash(t, e.realDB) != initial {
				t.Fatal("failed write changed the original ciphertext")
			}
			assertNativeEntry(t, backend, e.realDB, "Seed Entry", fixtureEntryPassword)
			repair()
			e.key("C-s")
			e.waitForWithin(20*time.Second, "repaired save completed", func() bool {
				screen := e.capture()
				return strings.Contains(screen, "saved · backup created") && !strings.Contains(screen, "Generate Password")
			})
			assertNativeEntry(t, backend, e.realDB, "Seed Entry Draft", fixtureEntryPassword)
			if vaultFileHash(t, e.realDB) == initial {
				t.Fatal("repaired save did not change ciphertext")
			}
			e.key("q")
			e.waitForCleanQuit()
		})
	}
}

func TestExternalChangeDraftCancelAndDiscardReloadTmux(t *testing.T) {
	t.Skip("persistent sessions require lock/unlock to reload; cached-credential reload is unsupported")
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)
	e.startWith(backend, e.realDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.key("e")
	e.mustShow("Edit Entry")
	e.typeText(" Unsaved")
	e.mustShow("Edit Entry *", "Seed Entry Unsaved")
	addExternalEntry(e, backend)
	externalHash := vaultFileHash(t, e.realDB)
	e.waitForWithin(8*time.Second, "external change prompt", func() bool { return strings.Contains(e.capture(), "Vault changed externally") })
	e.key("Enter") // Keep the current draft.
	e.mustShow("Changed externally", "Edit Entry *")
	e.key("C-s")
	e.mustShow("Vault changed externally")
	e.key("Enter")
	e.key("C-r")
	e.mustShow("Discard draft and reload?")
	e.key("Enter") // Cancel is the default.
	e.mustShow("Edit Entry *", "Seed Entry Unsaved")
	if vaultFileHash(t, e.realDB) != externalHash {
		t.Fatal("blocked stale save overwrote the external change")
	}
	e.key("C-r")
	e.mustShow("Discard draft and reload?")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("Vault reloaded.", "External Entry")
	if vaultFileHash(t, e.realDB) != externalHash {
		t.Fatal("reload changed ciphertext")
	}
	assertNativeEntry(t, backend, e.realDB, "Seed Entry", fixtureEntryPassword)
	e.key("q")
	e.waitForCleanQuit()
}

func vaultFileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func assertNativeEntry(t *testing.T, backend, path, title, password string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	creds := vault.Credentials{Password: []byte(fixturePassword)}
	defer creds.Clear()
	db, err := vaulttest.NewCLI(backend).Open(ctx, path, creds)
	if err != nil {
		t.Fatal("independent native reopen failed:", err)
	}
	defer db.Clear()
	for _, entry := range db.Entries {
		if entry.Get("Title") == title && entry.Get("Password") == password {
			return
		}
	}
	t.Fatal("independent native reopen did not find expected title and password")
}
