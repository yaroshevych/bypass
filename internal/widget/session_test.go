package widget

import (
	"context"
	"path/filepath"
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
	updated, err := backend.Apply(ctx, db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Example", "Tags": "Favorite"}, PasswordChanged: true, Password: []byte("synthetic secret")}, filepath.Join(t.TempDir(), "backups"))
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
	fields, err := s.Fields(ctx, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Protected && field.Value != "" {
			t.Fatal("protected value exposed")
		}
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
	if err := s.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if len(driver.value) != 0 {
		t.Fatal("clipboard retained after lock")
	}
	if _, err := s.Items(ctx, ""); err == nil {
		t.Fatal("locked vault remained accessible")
	}
}
