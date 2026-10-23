package clipboard

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"
)

// Manager tracks only clipboard contents written by this application. It never
// clears a value that differs from its last successful copy.
type Manager struct {
	mu       sync.Mutex
	driver   Driver
	owned    []byte
	hasOwned bool
	expires  time.Time
}

func New(driver Driver) *Manager { return &Manager{driver: driver} }

func (m *Manager) Copy(ctx context.Context, value []byte, now time.Time, timeout time.Duration) error {
	if m == nil {
		return ErrUnavailable
	}
	if timeout <= 0 {
		return errors.New("clipboard timeout must be positive")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if m.driver == nil {
		m.dropOwned()
		return ErrUnavailable
	}
	owned := bytes.Clone(value)
	toWrite := bytes.Clone(owned)
	err := m.driver.Write(ctx, toWrite)
	if err != nil {
		matched, cleanupErr := m.clearIfMatchesAfterWriteError(owned)
		clear(toWrite)
		if matched {
			if cleanupErr == nil {
				m.dropOwned()
			} else {
				// The failed write left our attempted value on the clipboard and
				// the conditional clear also failed. Retain it for cleanup.
				m.dropOwned()
				m.owned = bytes.Clone(owned)
				m.hasOwned = true
				m.expires = now.Add(timeout)
			}
		}
		clear(owned)
		return err
	}
	clear(toWrite)
	m.dropOwned()
	m.owned = owned
	m.hasOwned = true
	m.expires = now.Add(timeout)
	return nil
}

// Clear clears clipboard contents only if they still match this manager's last
// copy. Ownership state is dropped even if reading or writing the clipboard
// fails, so a later cleanup cannot unexpectedly clear unrelated data.
func (m *Manager) Clear(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clearLocked(ctx)
}

// Expire conditionally clears an owned clipboard value once its timeout has
// elapsed. The returned bool reports whether an owned value had expired.
func (m *Manager) Expire(ctx context.Context, now time.Time) (bool, error) {
	if m == nil {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasOwned || now.Before(m.expires) {
		return false, nil
	}
	return true, m.clearLocked(ctx)
}

func (m *Manager) clearLocked(ctx context.Context) error {
	defer m.dropOwned()
	if !m.hasOwned {
		return nil
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if m.driver == nil {
		return ErrUnavailable
	}
	current, err := m.driver.Read(ctx)
	if err != nil {
		return err
	}
	defer clear(current)
	if !bytes.Equal(current, m.owned) {
		return nil
	}
	return m.driver.Write(ctx, nil)
}

func (m *Manager) clearIfMatchesAfterWriteError(value []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	current, err := m.driver.Read(ctx)
	if err != nil {
		return false, err
	}
	defer clear(current)
	if !bytes.Equal(current, value) {
		return false, nil
	}
	return true, m.driver.Write(ctx, nil)
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (m *Manager) dropOwned() {
	clear(m.owned)
	m.owned = nil
	m.hasOwned = false
	m.expires = time.Time{}
}
