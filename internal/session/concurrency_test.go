package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"bypass/internal/vault"
)

type delayedOpen struct {
	started chan struct{}
	release chan struct{}
	db      *vault.Database
}

type delayedField struct {
	vault.Backend
	started chan struct{}
	release chan struct{}
	value   []byte
}

func (b delayedField) ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error) {
	close(b.started)
	<-b.release
	return b.value, nil
}

func TestFieldReadRejectsReplacementAndLockSeparately(t *testing.T) {
	for _, tc := range []struct {
		name string
		lock bool
		want error
	}{
		{"replaced", false, ErrChanged},
		{"locked and reopened", true, ErrLocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := delayedField{started: make(chan struct{}), release: make(chan struct{}), value: []byte("fixture value")}
			s := New(backend, nil, 0)
			ctx := context.Background()
			s.Adopt(&vault.Database{Path: "test.kdbx"})
			t.Cleanup(func() { _ = s.Lock(ctx) })
			snapshot := s.Snapshot()
			defer snapshot.Clear()
			type readResult struct {
				value []byte
				err   error
			}
			result := make(chan readResult, 1)
			go func() {
				value, err := s.ResolveField(ctx, snapshot, "item", "Password")
				result <- readResult{value, err}
			}()
			select {
			case <-backend.started:
			case <-time.After(5 * time.Second):
				close(backend.release)
				<-result
				t.Fatal("field resolver did not start")
			}
			if tc.lock {
				_ = s.Lock(ctx)
				s.Adopt(&vault.Database{Path: "test.kdbx"})
			} else {
				s.ReplaceDatabase(&vault.Database{Path: "test.kdbx"})
			}
			close(backend.release)
			got := <-result
			if !errors.Is(got.err, tc.want) || len(got.value) != 0 {
				t.Fatalf("stale read error = %v, want %v with no value", got.err, tc.want)
			}
			for _, b := range backend.value {
				if b != 0 {
					t.Fatal("stale read retained secret bytes")
				}
			}
		})
	}
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
