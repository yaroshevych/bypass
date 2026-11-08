package vaulttest

import (
	"context"

	"bypass/internal/vault"
)

// Backend provides fixture operations through the same in-process backend used
// by the application. Saved files are reopened after writes so callers inspect
// serialized state rather than a write result's cache.
type Backend struct{ backend *vault.NativeBackend }

func NewBackend() *Backend { return &Backend{backend: vault.NewBackend()} }

func (b *Backend) Open(ctx context.Context, path string, creds vault.Credentials) (*vault.Database, error) {
	db, err := b.backend.Open(ctx, path, creds)
	if err != nil {
		return nil, err
	}
	// Backend list models intentionally omit protected values. Test readback
	// needs the saved detail view, so fetch details from the reopened file.
	for i := range db.Entries {
		fields, err := b.backend.EntryDetails(ctx, db, db.Entries[i].UUID)
		if err != nil {
			db.Clear()
			return nil, err
		}
		db.Entries[i].Fields = fields
		db.Entries[i].DetailsLoaded = true
	}
	return db, nil
}

func (b *Backend) Create(ctx context.Context, path string, creds vault.Credentials) (*vault.Database, error) {
	return b.CreateWithOptions(ctx, path, creds, vault.CreateOptions{})
}

func (b *Backend) CreateWithOptions(ctx context.Context, path string, creds vault.Credentials, options vault.CreateOptions) (*vault.Database, error) {
	db, err := b.backend.CreateWithOptions(ctx, path, creds, options)
	if err != nil {
		return nil, err
	}
	db.Clear()
	return b.Open(ctx, path, creds)
}

func (b *Backend) Apply(ctx context.Context, snapshot *vault.Database, creds vault.Credentials, change vault.Change, backup string) (*vault.Database, error) {
	db, err := b.backend.Open(ctx, snapshot.Path, creds)
	if err != nil {
		return nil, err
	}
	defer db.Clear()
	updated, err := b.backend.Apply(ctx, db, creds, change, backup)
	if err != nil {
		return nil, err
	}
	updated.Clear()
	return b.Open(ctx, snapshot.Path, creds)
}

func (b *Backend) Info(ctx context.Context, path string, creds vault.Credentials) (vault.DatabaseInfo, error) {
	db, err := b.Open(ctx, path, creds)
	if err != nil {
		return vault.DatabaseInfo{}, err
	}
	defer db.Clear()
	return b.backend.InfoDatabase(ctx, db)
}

func (b *Backend) ExportAttachment(ctx context.Context, db *vault.Database, creds vault.Credentials, id, name string) ([]byte, error) {
	return b.backend.ExportAttachment(ctx, db, creds, id, name)
}

// NewCLI remains as a source compatibility shim for older eval helpers. The
// binary argument is ignored; normal test setup must use the direct backend.
func NewCLI(_ string) *Backend { return NewBackend() }
