package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/vault"
)

type immediateFieldBackend struct{}

func (immediateFieldBackend) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	return nil, ErrUnsupported
}

func (immediateFieldBackend) ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error) {
	return []byte("synthetic clipboard secret"), nil
}

type blockedClipboardDriver struct {
	mu      sync.Mutex
	value   []byte
	writes  int
	started chan struct{}
	release chan struct{}
}

func (d *blockedClipboardDriver) Write(ctx context.Context, value []byte) error {
	d.mu.Lock()
	d.writes++
	block := d.writes == 1
	d.mu.Unlock()
	if block {
		close(d.started)
		select {
		case <-d.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	d.mu.Lock()
	d.value = append(d.value[:0], value...)
	d.mu.Unlock()
	return nil
}

func (d *blockedClipboardDriver) Read(context.Context) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]byte(nil), d.value...), nil
}

func TestBlockedClipboardWriteDoesNotBlockReadsAndLockClears(t *testing.T) {
	ctx := context.Background()
	driver := &blockedClipboardDriver{started: make(chan struct{}), release: make(chan struct{})}
	core := New(immediateFieldBackend{}, clipboard.New(driver), time.Minute)
	core.Adopt(&vault.Database{Path: "synthetic.kdbx"})
	t.Cleanup(func() {
		select {
		case <-driver.release:
		default:
			close(driver.release)
		}
		_ = core.Lock(ctx)
	})

	copyDone := make(chan error, 1)
	go func() { copyDone <- core.CopyValue(ctx, "entry", "Password", time.Now()) }()
	select {
	case <-driver.started:
	case <-time.After(5 * time.Second):
		close(driver.release)
		t.Fatal("clipboard write did not reach the blocking driver")
	}

	snapshotDone := make(chan *Snapshot, 1)
	go func() { snapshotDone <- core.Snapshot() }()
	var snapshot *Snapshot
	select {
	case snapshot = <-snapshotDone:
	case <-time.After(time.Second):
		close(driver.release)
		<-copyDone
		t.Fatal("Snapshot blocked behind clipboard I/O")
	}
	defer snapshot.Clear()

	resolveDone := make(chan error, 1)
	go func() {
		value, err := core.ResolveField(ctx, snapshot, "entry", "Password")
		clear(value)
		resolveDone <- err
	}()
	select {
	case err := <-resolveDone:
		if err != nil {
			close(driver.release)
			<-copyDone
			t.Fatalf("ResolveField while clipboard write blocked: %v", err)
		}
	case <-time.After(time.Second):
		close(driver.release)
		<-copyDone
		t.Fatal("ResolveField blocked behind clipboard I/O")
	}

	lockStarted := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		close(lockStarted)
		lockDone <- core.Lock(ctx)
	}()
	<-lockStarted
	select {
	case err := <-lockDone:
		close(driver.release)
		<-copyDone
		t.Fatalf("Lock returned before the in-flight clipboard write completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(driver.release)
	if err := <-copyDone; err != nil {
		t.Fatalf("CopyValue: %v", err)
	}
	if err := <-lockDone; err != nil {
		t.Fatalf("Lock: %v", err)
	}
	value, err := driver.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 0 {
		t.Fatal("Lock left the copied value on the clipboard")
	}
}
