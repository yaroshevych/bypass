package widget

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

type memoryClipboard struct{ value []byte }

func (m *memoryClipboard) Write(_ context.Context, value []byte) error {
	m.value = append(m.value[:0], value...)
	return nil
}
func (m *memoryClipboard) Read(context.Context) ([]byte, error) {
	return append([]byte(nil), m.value...), nil
}

func TestSessionVaultLifecycle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	backend := vault.NewBackend()
	creds := vault.Credentials{Password: []byte("synthetic master")}
	db, err := backend.CreateWithOptions(ctx, path, creds, vault.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := backend.Apply(ctx, db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Example", "Tags": "Favorite", "URL": "https://example.test", "Notes": "first\nsecond", "TOTP Seed": "JBSWY3DPEHPK3PXP"}, PasswordChanged: true, Password: []byte("synthetic secret")}, filepath.Join(t.TempDir(), "backups"))
	db.Clear()
	if err != nil {
		t.Fatal(err)
	}
	updated.Clear()
	driver := &memoryClipboard{}
	s := NewSession(driver, time.Minute)
	password := []byte("synthetic master")
	if err := s.Open(ctx, path, password); err != nil {
		t.Fatal(err)
	}
	for _, b := range password {
		if b != 0 {
			t.Fatal("password not cleared")
		}
	}
	items, err := s.Items(ctx, "synthetic secret")
	if err != nil || len(items) != 1 {
		t.Fatalf("search failed: %v, %v", items, err)
	}
	if _, err := s.Items(ctx, strings.Repeat("x", 4097)); err == nil {
		t.Fatal("accepted an oversized search")
	}
	fields, err := s.Fields(ctx, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Protected && field.Value != "" {
			t.Fatal("protected value exposed")
		}
	}
	last := -1
	foundTOTP := false
	for _, field := range fields {
		if rank := fieldOrder(field.Name); rank < last {
			t.Fatal("fields are not in TUI order")
		} else {
			last = rank
		}
		if field.Name == "TOTP" {
			foundTOTP = field.Protected && field.Value == ""
		}
	}
	if !foundTOTP {
		t.Fatal("generated TOTP field is missing or unmasked")
	}
	passwordValue, _, err := s.Reveal(ctx, items[0].ID, "Password")
	if err != nil || string(passwordValue) != "synthetic secret" {
		t.Fatal("password reveal failed")
	}
	clear(passwordValue)
	code, remaining, err := s.Reveal(ctx, items[0].ID, "TOTP")
	if err != nil || len(code) != 6 || remaining < 1 {
		t.Fatal("TOTP reveal failed")
	}
	clear(code)
	if _, _, err := s.Reveal(ctx, items[0].ID, "TOTP Seed"); err == nil {
		t.Fatal("seed was revealable")
	}
	if err := s.CopyReference(ctx, items[0].ID, "TOTP"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Targets(ctx, []string{string(driver.value)}); err != nil {
		t.Fatal("generated TOTP reference cannot be resolved")
	}
	if err := s.CopyValue(ctx, items[0].ID, "Password"); err != nil {
		t.Fatal(err)
	}
	if string(driver.value) != "synthetic secret" {
		t.Fatal("copy failed")
	}
	if err := s.CopyReference(ctx, items[0].ID, "Password"); err != nil {
		t.Fatal(err)
	}
	if len(driver.value) == 0 || string(driver.value[:9]) != "bypass://" {
		t.Fatal("reference copy failed")
	}
	targets, err := s.Targets(ctx, []string{string(driver.value)})
	if err != nil || len(targets) != 1 {
		t.Fatalf("target resolution failed: %v", err)
	}
	resolved, err := s.ResolveApproved(ctx, targets)
	if err != nil {
		t.Fatal(err)
	}
	if string(resolved[targets[0].Reference]) != "synthetic secret" {
		t.Fatal("approval resolved the wrong value")
	}
	for _, value := range resolved {
		clear(value)
	}
	if err := s.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if len(driver.value) != 0 {
		t.Fatal("clipboard retained after lock")
	}
	if _, err := s.Items(ctx, ""); err == nil {
		t.Fatal("locked vault remained accessible")
	}
	if _, err := s.ResolveApproved(ctx, targets); err == nil {
		t.Fatal("approved a locked vault")
	}
}
