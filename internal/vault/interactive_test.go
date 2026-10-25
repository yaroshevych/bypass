package vault

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func interactiveFixture(t *testing.T) (string, Credentials, *Database) {
	t.Helper()
	if _, err := exec.LookPath("keepassxc-cli"); err != nil {
		t.Skip("keepassxc-cli required")
	}
	if runtime.GOOS == "windows" {
		t.Skip("experimental backend targets Linux and macOS")
	}
	path := filepath.Join(t.TempDir(), "interactive.kdbx")
	creds := Credentials{Password: []byte("synthetic interactive master password")}
	cli := NewCLI("")
	db, err := cli.CreateWithOptions(context.Background(), path, creds, CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(creds.Clear)
	t.Cleanup(db.Clear)
	return path, creds, db
}

func TestInteractiveNativeBrowseResolveAndLifetime(t *testing.T) {
	path, creds, seed := interactiveFixture(t)
	cli := NewCLI("")
	notes := "line one\nPassword: false-field\ninteractive.kdbx> \nlast line\n"
	updated, err := cli.Apply(context.Background(), seed, creds, Change{Kind: AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": `entry "quoted" \\ path`, "UserName": "synthetic-user", "Notes": notes}, PasswordChanged: true, Password: []byte("synthetic-entry-password")}, filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	defer updated.Clear()
	before, _ := Revision(path)
	ctx, cancel := context.WithCancel(context.Background())
	backend := NewInteractiveCLI("")
	db, err := backend.Open(ctx, path, creds)
	cancel() // The unlock request does not own the authenticated process lifetime.
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if !db.Interactive() || len(db.Entries) != 1 {
		t.Fatal("interactive entry missing")
	}
	entry := db.Entries[0]
	if entry.Get("Notes") != "" || entry.DetailsLoaded {
		t.Fatal("details were loaded before focus")
	}
	if entry.Get("UserName") != "synthetic-user" || entry.IsProtected("UserName") || entry.IsProtected("URL") || entry.IsProtected("Notes") {
		t.Fatal("list fields hidden or missing")
	}
	details, err := backend.EntryDetails(context.Background(), db, entry.UUID)
	if err != nil || len(details) != 1 || details[0].Name != "Notes" || details[0].Value != notes {
		t.Fatal("on-demand multiline notes did not round-trip", err)
	}
	if entry.Get("Password") != "" {
		t.Fatal("password fetched during initial load")
	}
	snapshot := db.Clone()
	snapshot.Clear() // Discarding a UI snapshot must not kill the session.
	secret, err := backend.ResolveField(context.Background(), db, Credentials{}, entry.UUID, "Password")
	if err != nil {
		t.Fatal(err)
	}
	if string(secret) != "synthetic-entry-password" {
		t.Fatal("incorrect resolved password")
	}
	zero(secret)
	after, _ := Revision(path)
	if before != after {
		t.Fatal("read-only backend changed source vault")
	}
	session := db.interactive
	canonical, err := canonicalPath(path)
	if err != nil || session.cmd.Args[len(session.cmd.Args)-1] != canonical {
		t.Fatal("KeePassXC did not open the original vault")
	}
	for _, arg := range session.cmd.Args {
		if strings.Contains(arg, string(creds.Password)) || strings.Contains(arg, "synthetic-entry-password") {
			t.Fatal("password exposed in process arguments")
		}
	}
	db.Clear()
	select {
	case <-session.done:
	case <-time.After(3 * time.Second):
		t.Fatal("full lock did not reap CLI")
	}
	if _, err := session.command(context.Background(), "ls"); err == nil {
		t.Fatal("closed session accepted command")
	}
}

func TestInteractiveNativeWrongPasswordAndExternalChange(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	backend := NewInteractiveCLI("")
	if _, err := backend.Open(context.Background(), path, Credentials{Password: []byte("wrong")}); !errors.Is(err, ErrUnlock) {
		t.Fatalf("wrong password: %v", err)
	}
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if _, err := backend.Open(context.Background(), path, Credentials{}); !errors.Is(err, ErrUnlock) {
		t.Fatal("reload did not require authentication")
	}
	// Empty vaults still own an authenticated process and release it on lock.
	if len(db.Entries) != 0 {
		t.Fatal("unexpected entries")
	}
}

func TestInteractiveNativeNestedGroups(t *testing.T) {
	path, creds, seed := interactiveFixture(t)
	cli := NewCLI("")
	backup := filepath.Join(t.TempDir(), "backups")
	grouped, err := cli.Apply(context.Background(), seed, creds, Change{Kind: CreateGroup, GroupUUID: seed.RootUUID, NewName: "Nested group"}, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer grouped.Clear()
	var group string
	for _, g := range grouped.Groups {
		if g.Name == "Nested group" {
			group = g.UUID
		}
	}
	populated, err := cli.Apply(context.Background(), grouped, creds, Change{Kind: AddEntry, GroupUUID: group, Values: map[string]string{"Title": "Nested entry"}}, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer populated.Clear()
	backend := NewInteractiveCLI("")
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if len(db.Groups) != 2 || len(db.Entries) != 1 || db.Entries[0].GroupPath != "Passwords/Nested group" {
		t.Fatal("nested navigation missing")
	}

}

func TestInteractiveCommandQuotingRejectsInjection(t *testing.T) {
	for _, value := range []string{"x\nquit", "x\rquit", "x\x00", "x\tquit"} {
		if _, err := interactiveLine("show", "-a", value); !errors.Is(err, ErrUnsupported) {
			t.Fatal("accepted command control character")
		}
	}
	line, err := interactiveLine("show", `a "quote" and \ slash`)
	if err != nil || line != `"show" "a \"quote\" and \\ slash"` {
		t.Fatalf("incorrect quoting: %q, %v", line, err)
	}
}

func TestInteractiveCancellationClosesStalledProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "stalled-cli")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := startInteractive(ctx, binary, "unused", Credentials{Password: []byte("synthetic")})
	if !errors.Is(err, ErrCanceled) || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestInteractiveCapabilitiesAdvertiseWriter(t *testing.T) {
	backend := NewInteractiveCLI("")
	if _, ok := any(backend).(Writer); !ok {
		t.Fatal("interactive backend does not implement writes")
	}
}

func TestInteractiveEmptyMarkerEntryIsNotDropped(t *testing.T) {
	path, creds, seed := interactiveFixture(t)
	updated, err := NewCLI("").Apply(context.Background(), seed, creds, Change{Kind: AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "[empty]"}}, filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	defer updated.Clear()
	db, err := NewInteractiveCLI("").Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if len(db.Entries) != 1 || db.Entries[0].Get("Title") != "[empty]" {
		t.Fatal("legal entry mistaken for empty group")
	}
}

func TestInteractiveProtocolWithoutReadlineEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	binary := filepath.Join(t.TempDir(), "no-readline-cli")
	script := `#!/bin/sh
IFS= read -r password
printf 'fixture> '
while IFS= read -r line; do
 case "$line" in
 bypass_complete_*) printf 'Unknown command %s\n' "$line" ;;
 *) printf '[empty]\n' ;;
 esac
 printf 'fixture> '
done
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := startInteractive(ctx, binary, "unused", Credentials{Password: []byte("synthetic")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	result, err := s.command(ctx, "ls")
	defer zero(result)
	if err != nil || string(result) != "[empty]\n" {
		t.Fatal("non-readline framing failed", err)
	}
}
