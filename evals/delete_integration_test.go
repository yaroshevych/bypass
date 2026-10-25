//go:build integration

package evals

import (
	"bypass/internal/vaulttest"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestNativeDeleteCancelRecyclePermanentAndGroupTmux(t *testing.T) {
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
	runCLI(e, backend, fixturePassword+"\n"+fixtureEntryPassword+"\n", "add", "-q", "--password-prompt", "--username", "survivor-user", e.realDB, "Survivor")
	e.startWith(backend, e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry", "Survivor")
	e.key("Right")
	initial := vaultFileHash(t, e.realDB)
	e.key("C-d")
	e.mustShow("DELETE", "Cancel")
	e.key("Enter")
	e.waitFor("entry-delete cancel closes dialog", func() bool { return !strings.Contains(e.capture(), "DELETE") })
	e.mustShow("Seed Entry", "Survivor")
	if vaultFileHash(t, e.realDB) != initial {
		t.Fatal("default delete confirmation mutated the vault")
	}
	e.key("C-d")
	e.mustShow("DELETE")
	e.key("Right")
	e.key("Enter")
	e.mustShowWrite("saved · backup created")
	e.key("Left")
	for range 3 {
		e.key("Down")
	}
	e.key("Right")
	e.mustShow("Bin", "Seed Entry")
	recycledHash := vaultFileHash(t, e.realDB)
	e.key("C-d")
	e.mustShow("DELETE", "Cancel", "Items already in", "permanently.")
	e.key("Enter")
	e.waitFor("permanent-delete cancel closes dialog", func() bool { return !strings.Contains(e.capture(), "DELETE") })
	e.mustShow("Bin", "Seed Entry")
	if vaultFileHash(t, e.realDB) != recycledHash {
		t.Fatal("default permanent-delete confirmation mutated the vault")
	}
	e.key("C-d")
	e.mustShow("DELETE")
	e.key("Right")
	e.key("Enter")
	e.waitForWithin(20*time.Second, "permanent entry deletion saved", func() bool {
		s := e.capture()
		return strings.Contains(s, "saved · backup created") && !strings.Contains(s, "DELETE") && !strings.Contains(s, "Working…")
	})
	e.key("g")
	e.mustShow("NEW GROUP")
	e.typeText("Temporary Group")
	e.key("Enter")
	e.waitForWithin(20*time.Second, "new group dialog completed", func() bool {
		s := e.capture()
		return !strings.Contains(s, "NEW GROUP") && !strings.Contains(s, "Working…") && strings.Contains(s, "saved · backup created")
	})
	e.mustShowWrite("Temporary Group", "saved · backup created")
	e.key("Left")
	groupHash := vaultFileHash(t, e.realDB)
	e.key("C-d")
	e.mustShow("DELETE", "Temporary Group", "Cancel")
	e.key("Enter")
	e.waitFor("group-delete cancel closes dialog", func() bool { return !strings.Contains(e.capture(), "DELETE") })
	if vaultFileHash(t, e.realDB) != groupHash {
		t.Fatal("default group-delete confirmation mutated the vault")
	}
	e.key("C-d")
	e.mustShow("DELETE", "Temporary Group")
	e.key("Right")
	e.key("Enter")
	e.waitForWithin(20*time.Second, "group deletion saved", func() bool {
		s := e.capture()
		return strings.Contains(s, "saved · backup created") && !strings.Contains(s, "DELETE") && !strings.Contains(s, "Working…")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	creds := vault.Credentials{Password: []byte(fixturePassword)}
	defer creds.Clear()
	db, err := vaulttest.NewCLI(backend).Open(ctx, e.realDB, creds)
	if err != nil {
		t.Fatal("reopen after deletions:", err)
	}
	defer db.Clear()
	survived, recycledGroup := false, false
	for _, entry := range db.Entries {
		if entry.Get("Title") == "Seed Entry" {
			t.Fatal("permanently deleted entry remains in native database")
		}
		if entry.Get("Title") == "Survivor" {
			survived = entry.Get("UserName") == "survivor-user" && entry.Get("Password") == fixtureEntryPassword && !entry.Recycled
		}
	}
	for _, group := range db.Groups {
		if group.Name == "Temporary Group" {
			recycledGroup = group.Recycled
		}
	}
	if !survived || !recycledGroup {
		t.Fatal("deletion altered an unrelated entry or failed to recycle the selected group")
	}
	e.key("q")
	e.waitForCleanQuit()
}
