package vault

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenDiagnosticsIdentifyMetadataFailureWithoutVaultData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "private-vault-name.kdbx")
	if err := os.WriteFile(path, []byte("disposable encrypted-file stand-in"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "fixture-cli")
	script := `#!/bin/sh
IFS= read -r password
printf 'private-vault-name.kdbx> '
while IFS= read -r line; do
 case "$line" in
 bypass_complete_*) printf 'Unknown command %s\n' "$line" ;;
 '"db-info"'*) printf 'UUID: {01010101-0101-0101-0101-010101010101}\nName: private-database-name\n' ;;
 '"ls"'*) printf 'private-entry-title\nprivate-entry-title\n' ;;
 *'"Uuid"'*) printf '{02020202-0202-0202-0202-020202020202}\n' ;;
 *) printf 'private-field-value\n'; printf 'private-native-stderr\n' >&2 ;;
 esac
 printf 'private-vault-name.kdbx> '
done
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	backend := NewInteractiveCLI(binary)
	backend.SetLogger(slog.New(slog.NewJSONHandler(&log, nil)))
	_, err := backend.Open(context.Background(), path, Credentials{Password: []byte("private-master-password")})
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected duplicate-name failure, got %v", err)
	}
	var pending *DuplicateEntries
	if errors.As(err, &pending) {
		defer pending.Close()
	}
	for _, expected := range []string{"session_authenticated", "command_started", "duplicate_entries_found", "open_failed", "find_duplicates"} {
		if !strings.Contains(log.String(), expected) {
			t.Fatalf("missing diagnostic %q", expected)
		}
	}
	for _, secret := range []string{directory, "private-vault-name", "private-database-name", "private-master-password", "private-entry-title", "private-field-value", "private-native-stderr"} {
		if strings.Contains(log.String(), secret) {
			t.Fatal("diagnostic included private fixture data")
		}
	}
}

func TestDuplicateScanLogsRejectionWithoutNames(t *testing.T) {
	for _, fixture := range []struct{ listing, reason string }{
		{"private/group/", "slash_in_listing_name"},
		{"private-group/\nprivate-group/", "duplicate_group_name"},
		{"private\tname", "control_character_in_listing_name"},
		{"\nprivate-name", "empty_listing_name"},
	} {
		t.Run(fixture.reason, func(t *testing.T) {
			dir := t.TempDir()
			path, binary := filepath.Join(dir, "fixture.kdbx"), filepath.Join(dir, "fixture-cli")
			if err := os.WriteFile(path, []byte("stand-in"), 0600); err != nil {
				t.Fatal(err)
			}
			script := `#!/bin/sh
IFS= read -r password
printf 'fixture.kdbx> '
while IFS= read -r line; do
 case "$line" in
 bypass_complete_*) printf 'Unknown command %s\n' "$line" ;;
 *) printf '%s\n' '` + fixture.listing + `' ;;
 esac
 printf 'fixture.kdbx> '
done
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			backend := NewInteractiveCLI(binary)
			backend.SetLogger(slog.New(slog.NewJSONHandler(&log, nil)))
			_, err := backend.Open(context.Background(), path, Credentials{Password: []byte("synthetic")})
			if !errors.Is(err, ErrAmbiguous) {
				t.Fatal("expected rejected listing", err)
			}
			if !strings.Contains(log.String(), fixture.reason) || !strings.Contains(log.String(), "duplicate_scan_rejected") {
				t.Fatal("missing rejection reason")
			}
			if strings.Contains(log.String(), "private") {
				t.Fatal("logged entry data")
			}
		})
	}
}

func TestOpenDiagnosticsRecordSuccessfulNativeLoad(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	var log bytes.Buffer
	backend := NewInteractiveCLI("")
	backend.SetLogger(slog.New(slog.NewJSONHandler(&log, nil)))
	db, err := backend.Open(context.Background(), path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	if !strings.Contains(log.String(), "metadata_loaded") || !strings.Contains(log.String(), "open_completed") {
		t.Fatal("successful open was not logged")
	}
	if strings.Contains(log.String(), path) || strings.Contains(log.String(), string(creds.Password)) {
		t.Fatal("successful-open log included private fixture data")
	}
}

func TestOpenDiagnosticsClassifyNativeAuthenticationFailure(t *testing.T) {
	path, creds, _ := interactiveFixture(t)
	var log bytes.Buffer
	backend := NewInteractiveCLI("")
	backend.SetLogger(slog.New(slog.NewJSONHandler(&log, nil)))
	_, err := backend.Open(context.Background(), path, Credentials{Password: []byte("deliberately wrong fixture password")})
	if !errors.Is(err, ErrUnlock) {
		t.Fatalf("expected unlock failure, got %v", err)
	}
	for _, expected := range []string{"unlock_diagnostic", "credentials_rejected", "exit_code", "authentication_options"} {
		if !strings.Contains(log.String(), expected) {
			t.Fatalf("missing diagnostic %q", expected)
		}
	}
	for _, private := range []string{path, string(creds.Password), "deliberately wrong fixture password", "Invalid credentials were provided"} {
		if strings.Contains(log.String(), private) {
			t.Fatal("authentication log included private data or raw CLI output")
		}
	}
}
