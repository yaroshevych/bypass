package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeTOTPResolveAndReadDoesNotWrite(t *testing.T) {
	backend := NewBackend()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "totp.kdbx")
	creds := Credentials{Password: []byte("synthetic TOTP master")}
	db, err := backend.CreateWithOptions(ctx, path, creds, CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Clear()
	backup := filepath.Join(t.TempDir(), "backups")
	updated, err := backend.Apply(ctx, db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{
		"Title": "Authenticator", "TOTP Seed": "JBSWY3DPEHPK3PXP", "Period": "17",
	}}, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	db = updated
	updated, err = backend.Apply(ctx, db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Ordinary"}}, backup)
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	db = updated
	defer db.Clear()

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var configured, absent string
	for _, entry := range db.Entries {
		if entry.Get("Title") == "Authenticator" {
			configured = entry.UUID
			if entry.Get("TOTP Seed") != "" || entry.Get("Password") != "" {
				t.Fatal("protected TOTP seed appeared in the list model")
			}
		} else {
			absent = entry.UUID
		}
	}
	if configured == "" || absent == "" {
		t.Fatal("test entries were not created")
	}
	result, err := backend.TOTP(ctx, db, Credentials{}, configured)
	if err != nil {
		t.Fatal("configured TOTP was unavailable")
	}
	defer zero(result.Code)
	if len(result.Code) != 6 || result.PeriodSeconds != 17 || result.RemainingSeconds < 1 || result.RemainingSeconds > 17 {
		t.Fatalf("unexpected TOTP metadata: digits=%d period=%d remaining=%d", len(result.Code), result.PeriodSeconds, result.RemainingSeconds)
	}
	if _, err := backend.TOTP(ctx, db, Credentials{}, absent); !errors.Is(err, ErrUnsupported) {
		t.Fatal("entry without a TOTP seed was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reading or generating a TOTP changed the vault")
	}
}
