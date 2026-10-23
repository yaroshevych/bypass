package clipboard

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryDriver struct {
	mu             sync.Mutex
	value          []byte
	readErr        error
	writeErr       error
	writeThenError bool
	onWriteError   []byte
	writes         int
}

func (d *memoryDriver) Write(_ context.Context, value []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes++
	if d.writeErr != nil && !d.writeThenError {
		if d.onWriteError != nil {
			clear(d.value)
			d.value = bytes.Clone(d.onWriteError)
		}
		return d.writeErr
	}
	clear(d.value)
	d.value = bytes.Clone(value)
	if d.writeErr != nil {
		return d.writeErr
	}
	return nil
}

func (d *memoryDriver) Read(context.Context) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.readErr != nil {
		return nil, d.readErr
	}
	return bytes.Clone(d.value), nil
}

func (d *memoryDriver) external(value []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.value)
	d.value = bytes.Clone(value)
}

func (d *memoryDriver) snapshot() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return bytes.Clone(d.value)
}

func TestClearPreservesClipboardWhenAnotherAppReplacesValue(t *testing.T) {
	driver := &memoryDriver{}
	manager := New(driver)
	secret := []byte("copied secret")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := manager.Copy(context.Background(), secret, now, time.Second); err != nil {
		t.Fatal(err)
	}
	clear(secret)
	driver.external([]byte("external value"))
	if err := manager.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := string(driver.snapshot()); got != "external value" {
		t.Fatalf("Clear replaced unrelated clipboard value: %q", got)
	}
	if manager.hasOwned || len(manager.owned) != 0 || !manager.expires.IsZero() {
		t.Fatal("Clear retained ownership state")
	}
}

func TestExpireUsesInjectedTimeAndNewCopyResetsTimeout(t *testing.T) {
	driver := &memoryDriver{}
	manager := New(driver)
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := manager.Copy(context.Background(), []byte("first"), now, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := manager.Copy(context.Background(), []byte("second"), now.Add(4*time.Second), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if expired, err := manager.Expire(context.Background(), now.Add(6*time.Second)); err != nil || expired {
		t.Fatalf("Expire before reset timeout = %v, %v", expired, err)
	}
	if got := string(driver.snapshot()); got != "second" {
		t.Fatalf("new copy was not retained: %q", got)
	}
	if expired, err := manager.Expire(context.Background(), now.Add(13*time.Second)); err != nil || expired {
		t.Fatalf("Expire before deadline = %v, %v", expired, err)
	}
	if expired, err := manager.Expire(context.Background(), now.Add(14*time.Second)); err != nil || !expired {
		t.Fatalf("Expire at deadline = %v, %v", expired, err)
	}
	if got := driver.snapshot(); len(got) != 0 {
		t.Fatalf("owned clipboard was not cleared: %q", got)
	}
}

func TestCopyOwnsPrivateCloneAndRejectsInvalidTimeout(t *testing.T) {
	driver := &memoryDriver{}
	manager := New(driver)
	value := []byte("private")
	if err := manager.Copy(context.Background(), value, time.Now(), time.Second); err != nil {
		t.Fatal(err)
	}
	value[0] = 'X'
	if string(manager.owned) != "private" {
		t.Fatal("manager retained caller-owned mutable bytes")
	}
	if err := manager.Copy(context.Background(), value, time.Now(), 0); err == nil {
		t.Fatal("Copy accepted a non-positive timeout")
	}
}

func TestClearDropsOwnedStateEvenWhenDriverFails(t *testing.T) {
	driver := &memoryDriver{}
	manager := New(driver)
	if err := manager.Copy(context.Background(), []byte("owned"), time.Now(), time.Minute); err != nil {
		t.Fatal(err)
	}
	driver.readErr = errors.New("private driver diagnostic")
	if err := manager.Clear(context.Background()); err == nil {
		t.Fatal("Clear hid a driver failure")
	}
	if manager.hasOwned || len(manager.owned) != 0 || !manager.expires.IsZero() {
		t.Fatal("Clear failure retained owned secret state")
	}
	if got := string(driver.snapshot()); got != "owned" {
		t.Fatalf("failed Clear changed clipboard: %q", got)
	}
}

func TestUnavailableDriverReturnsSanitizedErrors(t *testing.T) {
	driver := unavailableDriver{}
	if err := driver.Write(context.Background(), []byte("do not leak")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Write error = %v", err)
	}
	if _, err := driver.Read(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Read error = %v", err)
	}
}

func TestManagerMethodsAreConcurrentSafe(t *testing.T) {
	driver := &memoryDriver{}
	manager := New(driver)
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			value := []byte{byte('a' + i)}
			_ = manager.Copy(context.Background(), value, now.Add(time.Duration(i)*time.Millisecond), time.Second)
			_, _ = manager.Expire(context.Background(), now.Add(time.Duration(i)*time.Second))
			_ = manager.Clear(context.Background())
		}(i)
	}
	wg.Wait()
	if err := manager.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledCopyDoesNotReachDriverAndCanceledClearDropsOwnership(t *testing.T) {
	driver := &memoryDriver{}
	manager := New(driver)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Copy(ctx, []byte("never copied"), time.Now(), time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Copy error = %v", err)
	}
	if driver.writes != 0 || manager.hasOwned {
		t.Fatal("canceled Copy reached the driver or stored ownership")
	}
	if err := manager.Copy(context.Background(), []byte("owned"), time.Now(), time.Second); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := manager.Clear(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Clear error = %v", err)
	}
	if manager.hasOwned || len(manager.owned) != 0 {
		t.Fatal("canceled Clear retained owned bytes")
	}
	if got := string(driver.snapshot()); got != "owned" {
		t.Fatalf("canceled Clear touched clipboard: %q", got)
	}
}

func TestFailedCopyConditionallyCleansAttemptedValueWithoutClearingExternalValue(t *testing.T) {
	driver := &memoryDriver{writeErr: errors.New("write failed"), writeThenError: true}
	manager := New(driver)
	if err := manager.Copy(context.Background(), []byte("attempted secret"), time.Now(), time.Second); err == nil {
		t.Fatal("Copy hid a driver failure")
	}
	if got := driver.snapshot(); len(got) != 0 {
		t.Fatalf("manager did not conditionally clean attempted value: %q", got)
	}
	driver.writeErr = nil
	if err := manager.Clear(context.Background()); err != nil || manager.hasOwned {
		t.Fatalf("cleanup state after a failed cleanup write = %v, %v", manager.hasOwned, err)
	}

	driver = &memoryDriver{writeErr: errors.New("write failed"), onWriteError: []byte("external value")}
	manager = New(driver)
	if err := manager.Copy(context.Background(), []byte("attempted secret"), time.Now(), time.Second); err == nil {
		t.Fatal("Copy hid a driver failure")
	}
	if got := string(driver.snapshot()); got != "external value" {
		t.Fatalf("failed Copy cleared unrelated clipboard value: %q", got)
	}
	if driver.writes != 1 {
		t.Fatalf("failed Copy wrote to clipboard %d times; cleanup should not write after mismatch", driver.writes)
	}
}
