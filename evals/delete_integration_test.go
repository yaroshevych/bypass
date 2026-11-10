//go:build integration

package evals

import (
	"bypass/internal/vaulttest"
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestNativeDeleteCancelRecyclePermanentAndGroupTmux(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e)
	creds := vault.Credentials{Password: []byte(fixturePassword)}
	db, err := vaulttest.NewBackend().Open(context.Background(), e.realDB, creds)
	if err != nil {
		t.Fatal("open fixture vault:", err)
	}
	updated, err := vaulttest.NewBackend().Apply(context.Background(), db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Survivor", "UserName": "survivor-user"}, PasswordChanged: true, Password: []byte(fixtureEntryPassword)}, filepath.Join(e.root, "fixture-backups"))
	db.Clear()
	creds.Clear()
	if err != nil {
		t.Fatal("seed survivor entry:", err)
	}
	updated.Clear()
	e.startWith(e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry", "Survivor")
	e.key("Right")
	initial := vaultFileHash(t, e.realDB)
	e.key("DC")
	e.mustShow("DELETE", "Cancel")
	e.key("Enter")
	e.waitFor("entry-delete cancel closes dialog", func() bool { return !strings.Contains(e.capture(), "DELETE") })
	e.mustShow("Seed Entry", "Survivor")
	if vaultFileHash(t, e.realDB) != initial {
		t.Fatal("default delete confirmation mutated the vault")
	}
	e.key("DC")
	e.mustShow("DELETE")
	e.key("Down")
	e.key("Enter")
	e.mustShowWrite("saved · backup created")
	e.key("Left")
	for range 3 {
		e.key("Down")
	}
	e.key("Right")
	e.mustShow("Bin", "Seed Entry")
	recycledHash := vaultFileHash(t, e.realDB)
	e.key("DC")
	e.mustShow("DELETE", "Cancel")
	e.key("Enter")
	e.waitFor("permanent-delete cancel closes dialog", func() bool { return !strings.Contains(e.capture(), "DELETE") })
	e.mustShow("Bin", "Seed Entry")
	if vaultFileHash(t, e.realDB) != recycledHash {
		t.Fatal("default permanent-delete confirmation mutated the vault")
	}
	e.key("DC")
	e.mustShow("DELETE")
	e.key("Down")
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
	e.key("DC")
	e.mustShow("MOVE GROUP TO RECYCLE BIN", "Temporary Group", "Cancel")
	e.key("Enter")
	e.waitFor("group-delete cancel closes dialog", func() bool { return !strings.Contains(e.capture(), "MOVE GROUP TO RECYCLE BIN") })
	if vaultFileHash(t, e.realDB) != groupHash {
		t.Fatal("default group-delete confirmation mutated the vault")
	}
	e.key("DC")
	e.mustShow("MOVE GROUP TO RECYCLE BIN", "Temporary Group")
	e.key("Down")
	e.key("Enter")
	e.waitForWithin(20*time.Second, "group deletion saved", func() bool {
		s := e.capture()
		return strings.Contains(s, "saved · backup created") && !strings.Contains(s, "MOVE GROUP TO RECYCLE BIN") && !strings.Contains(s, "Working…")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	readCreds := vault.Credentials{Password: []byte(fixturePassword)}
	defer readCreds.Clear()
	db, err = vaulttest.NewBackend().Open(ctx, e.realDB, readCreds)
	if err != nil {
		t.Fatal("reopen after deletions:", err)
	}
	defer db.Clear()
	survived, groupInBin := false, false
	for _, entry := range db.Entries {
		if entry.Get("Title") == "Seed Entry" {
			t.Fatal("permanently deleted entry remains in native database")
		}
		if entry.Get("Title") == "Survivor" {
			survived = entry.Get("UserName") == "survivor-user" && entry.Get("Password") == fixtureEntryPassword && !entry.Recycled
		}
	}
	for _, group := range db.Groups {
		if group.Name == "Temporary Group" && group.Recycled && group.ParentUUID == db.RecycleBinUUID {
			groupInBin = true
		}
	}
	if !survived || !groupInBin {
		e.failWithScreen(fmt.Sprintf("deletion outcome: survivor intact=%v, group recycled=%v", survived, groupInBin), e.capture())
	}
	e.key("q")
	e.waitForCleanQuit()
}
