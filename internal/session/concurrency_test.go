package session

import (
	"context"
	"testing"
	"time"

	"bypass/internal/vault"
)

type delayedOpen struct {
	started chan struct{}
	release chan struct{}
	db      *vault.Database
}

func (b delayedOpen) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	close(b.started)
	<-b.release
	return b.db, nil
}

func TestLockInvalidatesPendingOpen(t *testing.T) {
	db := &vault.Database{Entries: []vault.Entry{{UUID: "late"}}}
	backend := delayedOpen{started: make(chan struct{}), release: make(chan struct{}), db: db}
	s := New(backend, nil, 0)
	opened := make(chan error, 1)
	go func() { opened <- s.Open(context.Background(), "test.kdbx", vault.Credentials{}) }()
	<-backend.started
	locked := make(chan error, 1)
	go func() { locked <- s.Lock(context.Background()) }()
	select {
	case err := <-locked:
		if err != nil {
			close(backend.release)
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		close(backend.release)
		t.Fatal("lock waited for the pending backend open")
	}
	close(backend.release)
	if err := <-opened; err == nil {
		t.Fatal("pending open succeeded after lock")
	}
	if s.Database() != nil || len(db.Entries) != 0 {
		t.Fatal("late open retained decrypted data")
	}
}
