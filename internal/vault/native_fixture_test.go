package vault

import (
	"context"
	"path/filepath"
	"testing"
)

func nativeFixture(t *testing.T) (string, Credentials, *Database, *NativeBackend) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "native-fixture.kdbx")
	creds := Credentials{Password: []byte("synthetic native fixture master")}
	backend := NewBackend()
	db, err := backend.CreateWithOptions(context.Background(), path, creds, CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(creds.Clear)
	t.Cleanup(db.Clear)
	return path, creds, db, backend
}

func applyFixtureChange(t *testing.T, backend *NativeBackend, db **Database, creds Credentials, change Change, backup string) {
	t.Helper()
	updated, err := backend.Apply(context.Background(), *db, creds, change, backup)
	if err != nil {
		t.Fatal(err)
	}
	(*db).TransferBackend(updated)
	(*db).Clear()
	*db = updated
}
