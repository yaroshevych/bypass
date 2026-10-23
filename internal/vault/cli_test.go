package vault

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLIProbeAndOpenErrorsAreOpaque(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'KeePassXC 9.9'; exit 0; fi\necho 'secret diagnostic: password=leaked' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cli := NewCLI(bin)
	if v, err := cli.Probe(context.Background()); err != nil || v != "KeePassXC 9.9" {
		t.Fatalf("Probe() = %q, %v", v, err)
	}
	dbfile := filepath.Join(dir, "vault.kdbx")
	if err := os.WriteFile(dbfile, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := cli.Open(context.Background(), dbfile, Credentials{Password: []byte("private")})
	if !errors.Is(err, ErrUnlock) || strings.Contains(err.Error(), "leaked") {
		t.Fatalf("expected opaque unlock error, got %v", err)
	}
}

func TestCLIMissingBinaryAndInvalidPath(t *testing.T) {
	if _, err := NewCLI(filepath.Join(t.TempDir(), "missing")).Probe(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable, got %v", err)
	}
	if _, err := NewCLI("fake").Open(context.Background(), "/missing/file", Credentials{}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("expected invalid path, got %v", err)
	}
}

func TestCLIOpenDetectsExternalFileChange(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	vaultPath := filepath.Join(dir, "vault.kdbx")
	if err := os.WriteFile(vaultPath, []byte("encrypted-before"), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf x >> "$6"
cat <<'XML'
<?xml version="1.0"?><KeePassFile><Meta/><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Root</Name></Group></Root></KeePassFile>
XML
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCLI(bin).Open(context.Background(), vaultPath, Credentials{NoPassword: true}); !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("expected external change sentinel, got %v", err)
	}
}

func TestCLIOpenSuccessUsesArgumentsAndPasswordStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	vaultPath := filepath.Join(dir, "vault with spaces.kdbx")
	if err := os.WriteFile(vaultPath, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	expectedPath, err := canonicalPath(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
[ "$#" -eq 9 ] || exit 10
[ "$1" = export ] && [ "$2" = -q ] && [ "$3" = -f ] && [ "$4" = xml ] || exit 11
[ "$5" = -k ] && [ "$6" = "key file" ] && [ "$7" = -y ] && [ "$8" = "2:serial" ] || exit 12
[ "$9" = "` + expectedPath + `" ] || exit 13
IFS= read -r provided || exit 14
[ "$provided" = "test password" ] || exit 15
cat <<'XML'
<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Boundary</DatabaseName></Meta><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Root</Name></Group></Root></KeePassFile>
XML
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := NewCLI(bin).Open(context.Background(), vaultPath, Credentials{
		Password: []byte("test password"), KeyFile: "key file", YubiKey: "2:serial",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if db.Name != "Boundary" || len(db.Groups) != 1 {
		t.Fatalf("unexpected parsed database: name=%q groups=%d", db.Name, len(db.Groups))
	}
}

func TestCLIOpenCancellationMapsToSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	vaultPath := filepath.Join(dir, "vault.kdbx")
	if err := os.WriteFile(vaultPath, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(25*time.Millisecond, cancel)
	_, err := NewCLI(bin).Open(ctx, vaultPath, Credentials{NoPassword: true})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("expected canceled sentinel, got %v", err)
	}
}

func TestCLIOpenEnforcesStdoutLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	vaultPath := filepath.Join(dir, "vault.kdbx")
	if err := os.WriteFile(vaultPath, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec dd if=/dev/zero bs=1048576 count=33 2>/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := NewCLI(bin).Open(context.Background(), vaultPath, Credentials{NoPassword: true})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected output size sentinel, got %v", err)
	}
}

func TestOpenRealVaultOptIn(t *testing.T) {
	path, password := os.Getenv("KEEPASSUI_TEST_VAULT"), os.Getenv("KEEPASSUI_TEST_PASSWORD")
	if path == "" || password == "" {
		t.Skip("set KEEPASSUI_TEST_VAULT and KEEPASSUI_TEST_PASSWORD to run integration check")
	}
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("could not read integration vault")
	}
	beforeHash := sha256.Sum256(beforeBytes)
	pass := []byte(password)
	defer zero(pass)
	db, err := NewCLI("").Open(context.Background(), path, Credentials{Password: pass})
	if err != nil {
		t.Fatalf("vault integration open failed: %v", err)
	}
	defer db.Clear()
	if len(db.Groups) == 0 || len(db.Entries) == 0 {
		t.Fatal("opened database has no groups or entries")
	}
	seen := make(map[string]struct{}, len(db.Entries))
	protectedCount := 0
	for _, entry := range db.Entries {
		if !validEntityUUID(entry.UUID) {
			t.Fatal("opened database contains an invalid entry UUID")
		}
		if _, exists := seen[entry.UUID]; exists {
			t.Fatal("opened database contains duplicate entry UUIDs")
		}
		seen[entry.UUID] = struct{}{}
		for _, field := range entry.Fields {
			if entry.Get(field.Name) != field.Value || entry.IsProtected(field.Name) != field.Protected {
				t.Fatal("entry field accessors disagree with parsed field metadata")
			}
			if field.Protected {
				protectedCount++
			}
			if field.Name == "Password" && !entry.IsProtected("Password") {
				t.Fatal("password field is not marked protected")
			}
		}
	}
	if protectedCount == 0 {
		t.Fatal("opened database contains no protected fields")
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("could not reread integration vault")
	}
	if sha256.Sum256(afterBytes) != beforeHash {
		t.Fatal("vault file changed during read-only open")
	}
}

func TestOpenPinsCanonicalTargetBeforeBackendRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	dir := t.TempDir()
	first := filepath.Join(dir, "first.kdbx")
	second := filepath.Join(dir, "second.kdbx")
	alias := filepath.Join(dir, "alias.kdbx")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("encrypted fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(first, alias); err != nil {
		t.Skip("symlinks unavailable")
	}
	expected, err := canonicalPath(first)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-cli")
	script := `#!/bin/sh
[ "$6" = "` + expected + `" ] || exit 10
ln -sf "` + second + `" "` + alias + `" || exit 11
cat <<'XML'
<KeePassFile><Meta/><Root><Group><UUID>AQEBAQEBAQEBAQEBAQEBAQ==</UUID><Name>Root</Name></Group></Root></KeePassFile>
XML
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := NewCLI(bin).Open(context.Background(), alias, Credentials{NoPassword: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if db.Path != expected {
		t.Fatal("opened session followed a retargeted alias")
	}
	actual, err := canonicalPath(alias)
	if err != nil || actual == db.Path {
		t.Fatal("test did not retarget symlink")
	}
}
