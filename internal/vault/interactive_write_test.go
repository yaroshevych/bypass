package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestInteractiveNativeCreateEditAndIndependentReopen(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	session := db.interactive
	backup := filepath.Join(t.TempDir(), "backups")
	original, _ := os.ReadFile(path)
	apply := func(change Change) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		updated, err := backend.Apply(ctx, db, Credentials{}, change, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(updated)
		db.Clear()
		db = updated
	}
	apply(Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": `Entry "quoted"`, "UserName": "test-user", "URL": "https://example.test", "Notes": "first\nPassword: not-a-field\nlast\n"}, PasswordChanged: true, Password: []byte("initial synthetic entry password")})
	if len(db.Entries) != 1 {
		t.Fatal("entry not created")
	}
	id := db.Entries[0].UUID
	apply(Change{Kind: EditEntry, EntryUUID: id, Values: map[string]string{"Title": "Renamed entry", "UserName": "updated-user", "Notes": "updated\nmultiline notes"}, PasswordChanged: true, Password: []byte("updated synthetic entry password")})
	apply(Change{Kind: EditEntry, EntryUUID: id, Values: map[string]string{"URL": "https://updated.test"}})
	if db.interactive != session {
		t.Fatal("write replaced authenticated process")
	}
	opened, err := NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal("independent ciphertext reopen failed", err)
	}
	defer opened.Clear()
	if len(opened.Entries) != 1 {
		t.Fatal("wrong native entry count")
	}
	entry := opened.Entries[0]
	if entry.UUID != id || entry.Get("Title") != "Renamed entry" || entry.Get("UserName") != "updated-user" || entry.Get("Notes") != "updated\nmultiline notes" || entry.Get("Password") != "updated synthetic entry password" || entry.Get("URL") != "https://updated.test" {
		t.Fatal("independent reopen did not match edits")
	}
	backups, err := filepath.Glob(filepath.Join(backup, "*.kdbx"))
	if err != nil || len(backups) != 3 {
		t.Fatal("encrypted backups missing")
	}
	foundOriginal := false
	for _, name := range backups {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(data, original) {
			foundOriginal = true
		}
		old, err := NewCLI("").Open(context.Background(), name, creds)
		if err != nil {
			t.Fatal("backup cannot be independently decrypted", err)
		}
		old.Clear()
	}
	if !foundOriginal {
		t.Fatal("original ciphertext backup missing")
	}
	db.Clear()
	select {
	case <-session.done:
	default:
		t.Fatal("native process not reaped")
	}
}

func TestInteractiveNativeGroupMoveAndPasswordPreservation(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	backup := filepath.Join(t.TempDir(), "backups")
	apply := func(change Change) {
		t.Helper()
		updated, err := backend.Apply(context.Background(), db, Credentials{}, change, backup)
		if err != nil {
			t.Fatal(err)
		}
		db.TransferInteractive(updated)
		db.Clear()
		db = updated
	}
	apply(Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Move me"}, PasswordChanged: true, Password: []byte("unchanged synthetic password")})
	id := db.Entries[0].UUID
	apply(Change{Kind: CreateGroup, GroupUUID: db.RootUUID, NewName: "Destination"})
	var group string
	for _, g := range db.Groups {
		if g.Name == "Destination" {
			group = g.UUID
		}
	}
	apply(Change{Kind: MoveEntry, EntryUUID: id, GroupUUID: group})
	opened, err := NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	destination := ""
	for _, group := range opened.Groups {
		if group.Name == "Destination" {
			destination = group.UUID
		}
	}
	if len(opened.Entries) != 1 || opened.Entries[0].GroupUUID != destination || opened.Entries[0].Get("Password") != "unchanged synthetic password" {
		t.Fatal("native move lost entry or password")
	}
}

func TestInteractiveNativeBackupFailureAndKeePassXCExternalChange(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	before, _ := Revision(path)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("blocker"), 0600); err != nil {
		t.Fatal(err)
	}
	change := Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Retry"}}
	if _, err := backend.Apply(context.Background(), db, Credentials{}, change, blocker); !errors.Is(err, ErrBackup) {
		t.Fatal("expected backup failure", err)
	}
	after, _ := Revision(path)
	if before != after {
		t.Fatal("backup failure changed source")
	}
	updated, err := backend.Apply(context.Background(), db, Credentials{}, change, filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal("backup failure poisoned clean session", err)
	}
	db.TransferInteractive(updated)
	db.Clear()
	db = updated
	// KeePassXC itself detects changes made by another native session.
	external, err := NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := NewCLI("").Apply(context.Background(), external, creds, Change{Kind: AddEntry, GroupUUID: external.RootUUID, Values: map[string]string{"Title": "External"}}, filepath.Join(t.TempDir(), "external-backups"))
	external.Clear()
	if err != nil {
		t.Fatal(err)
	}
	defer changed.Clear()
	externalRevision, _ := Revision(path)
	_, err = backend.Apply(context.Background(), db, Credentials{}, Change{Kind: EditEntry, EntryUUID: db.Entries[0].UUID, Values: map[string]string{"Notes": "must not overwrite external changes"}}, filepath.Join(t.TempDir(), "backups"))
	if !errors.Is(err, ErrInteractiveSessionLost) {
		t.Fatal("native conflict was not reported", err)
	}
	finalRevision, _ := Revision(path)
	if finalRevision != externalRevision {
		t.Fatal("KeePassXC external-change refusal was bypassed")
	}
}

func TestInteractiveFullLockDoesNotUndoCompletedKeePassXCSave(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process/FIFO fixture")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required for controlled native process")
	}
	native, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli required")
	}
	path, creds, seed := interactiveFixture(t)
	seeded, err := NewCLI("").Apply(context.Background(), seed, creds, Change{Kind: AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "Seed"}}, filepath.Join(t.TempDir(), "seed-backups"))
	if err != nil {
		t.Fatal(err)
	}
	defer seeded.Clear()
	dir := t.TempDir()
	started, release := filepath.Join(dir, "started.fifo"), filepath.Join(dir, "release.fifo")
	if err := exec.Command("mkfifo", started, release).Run(); err != nil {
		t.Fatal(err)
	}
	signal, err := os.OpenFile(started, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer signal.Close()
	wrapper := filepath.Join(dir, "controlled-native")
	script := fmt.Sprintf(`#!%s
import os, subprocess, sys, threading
saved = threading.Event()
child = subprocess.Popen([%q] + sys.argv[1:], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
def forward():
    pending = b''
    while True:
        chunk = os.read(child.stdout.fileno(), 65536)
        if not chunk: return
        os.write(1, chunk)
        pending = (pending + chunk)[-65536:]
        if b'Successfully edited entry ' in pending:
            saved.set()
threading.Thread(target=forward, daemon=True).start()
for line in sys.stdin.buffer:
    child.stdin.write(line)
    child.stdin.flush()
    if line.startswith(b'"edit" '):
        if not saved.wait(10): sys.exit(7)
        with open(%q, 'wb', buffering=0) as marker: marker.write(b'x')
        with open(%q, 'rb', buffering=0) as gate: gate.read(1)
child.stdin.close()
sys.exit(child.wait())
`, python, native, started, release)
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	backend := NewInteractiveCLI(wrapper)
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	session := db.interactive
	before, _ := Revision(path)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		updated, err := backend.Apply(ctx, db, Credentials{}, Change{Kind: EditEntry, EntryUUID: db.Entries[0].UUID, Values: map[string]string{"Notes": "native save already completed"}}, filepath.Join(dir, "backups"))
		if updated != nil {
			updated.Clear()
		}
		result <- err
	}()
	reached := make(chan error, 1)
	go func() { var one [1]byte; _, err := signal.Read(one[:]); reached <- err }()
	select {
	case err := <-reached:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("native save did not reach acknowledgement barrier")
	}
	working, _ := Revision(path)
	if working == before {
		t.Fatal("barrier did not follow the native save")
	}
	// Full lock terminates the native process. It does not undo a save that
	// KeePassXC has already completed on the original file.
	session.close()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, ErrInteractiveSessionLost) {
			t.Fatal("pending write did not fail closed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native process did not cancel promptly")
	}
	after, _ := Revision(path)
	if before == after {
		t.Fatal("native save did not persist")
	}
	reopened, err := NewCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Clear()
	if reopened.Entries[0].Get("Notes") != "native save already completed" {
		t.Fatal("completed native edit was lost")
	}
}

func TestInteractiveCanceledFieldReadDrainsAndKeepsSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix FIFO fixture")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required for controlled protocol")
	}
	dir := t.TempDir()
	started, release := filepath.Join(dir, "started"), filepath.Join(dir, "release")
	if err := exec.Command("mkfifo", started, release).Run(); err != nil {
		t.Fatal(err)
	}
	signal, err := os.OpenFile(started, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer signal.Close()
	gate, err := os.OpenFile(release, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	binary := filepath.Join(dir, "controlled-read")
	script := fmt.Sprintf(`#!%s
import sys
sys.stdin.buffer.readline()
print('fixture> ', end='', flush=True)
paused = False
for raw in sys.stdin.buffer:
    line = raw.decode().rstrip('\n')
    if line.startswith('bypass_complete_'):
        print('Unknown command ' + line, flush=True)
    else:
        if not paused:
            paused = True
            with open(%q, 'wb', buffering=0) as marker: marker.write(b'x')
            with open(%q, 'rb', buffering=0) as gate: gate.read(1)
        print('synthetic-drained-secret', flush=True)
    print('fixture> ', end='', flush=True)
`, python, started, release)
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := startInteractive(context.Background(), binary, "unused", Credentials{Password: []byte("synthetic")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	path := filepath.Join(dir, "vault.kdbx")
	if err := os.WriteFile(path, []byte("encrypted fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	revision, _ := Revision(path)
	db := &Database{Path: path, Revision: revision, RootUUID: "root", Groups: []Group{{UUID: "root", Name: "Root"}}, Entries: []Entry{{UUID: "entry", GroupUUID: "root", Fields: []Field{{Name: "Title", Value: "Entry"}, {Name: "Password", Protected: true}}}}, interactive: s}
	backend := NewInteractiveCLI(binary)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		value, err := backend.ResolveField(ctx, db, Credentials{}, "entry", "Password")
		zero(value)
		result <- err
	}()
	reached := make(chan error, 1)
	go func() { var one [1]byte; _, err := signal.Read(one[:]); reached <- err }()
	select {
	case err := <-reached:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read did not reach cancellation boundary")
	}
	cancel()
	if _, err := gate.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrCanceled) {
			t.Fatal("canceled read was accepted", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read response did not drain")
	}
	value, err := backend.ResolveField(context.Background(), db, Credentials{}, "entry", "Password")
	defer zero(value)
	if err != nil || string(value) != "synthetic-drained-secret" {
		t.Fatal("canceled read destroyed authenticated session", err)
	}
}
