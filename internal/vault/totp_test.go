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

func TestCustomFieldEditPreservesTOTPAndProtection(t *testing.T) {
	path, creds, db, backend := nativeFixture(t)
	ctx := context.Background()
	backup := filepath.Join(t.TempDir(), "backups")
	applyFixtureChange(t, backend, &db, creds, Change{
		Kind: AddEntry, GroupUUID: db.RootUUID,
		Values:       map[string]string{"Title": "Authenticator", "Email": "old@example.test", "TOTP Seed": "JBSWY3DPEHPK3PXP", "Period": "17", "Digits": "6"},
		CustomFields: []Field{{Name: "Private", Value: "original", Protected: true}, {Name: "Public", Value: "visible"}},
	}, backup)
	entryID := db.Entries[0].UUID
	applyFixtureChange(t, backend, &db, creds, Change{
		Kind: EditEntry, EntryUUID: entryID, Values: map[string]string{"Title": "Authenticator", "Email": "new@example.test"},
		CustomFields: []Field{{Name: "Private", Value: "first line\nsecond line", Protected: true}, {Name: "Public", Value: "updated"}},
	}, backup)
	db.Clear()
	opened, err := backend.Open(ctx, path, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Clear()
	fields, err := backend.EntryDetails(ctx, opened, entryID)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]Field, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
	}
	for _, want := range []Field{
		{Name: "Email", Value: "new@example.test"},
		{Name: "TOTP Seed", Value: "JBSWY3DPEHPK3PXP", Protected: true},
		{Name: "Period", Value: "17"},
		{Name: "Digits", Value: "6"},
		{Name: "Private", Value: "first line\nsecond line", Protected: true},
		{Name: "Public", Value: "updated"},
	} {
		if got, ok := byName[want.Name]; !ok || got != want {
			t.Errorf("field %q was not preserved with its expected value and protection", want.Name)
		}
	}
	result, err := backend.TOTP(ctx, opened, Credentials{}, entryID)
	if err != nil {
		t.Fatal("TOTP unavailable after custom field edit:", err)
	}
	defer zero(result.Code)
	if result.PeriodSeconds != 17 || len(result.Code) != 6 {
		t.Fatalf("TOTP settings changed: period=%d digits=%d", result.PeriodSeconds, len(result.Code))
	}
}
