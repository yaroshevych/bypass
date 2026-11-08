package ui

import (
	"bytes"
	"context"
	"testing"

	"bypass/internal/clipboard"
	"bypass/internal/vault"
)

type observedClipboard struct {
	value  []byte
	writes int
	reads  int
}

func (c *observedClipboard) Read(context.Context) ([]byte, error) {
	c.reads++
	return bytes.Clone(c.value), nil
}
func (c *observedClipboard) Write(_ context.Context, value []byte) error {
	c.writes++
	c.value = bytes.Clone(value)
	return nil
}

func TestPasswordCopyResolvesThroughManagedClipboard(t *testing.T) {
	m, _ := operationModel(t)
	driver := &observedClipboard{}
	m.clipboard = clipboard.New(driver)

	cmd := m.copyField("Password")
	if cmd == nil {
		t.Fatal("password field was not resolved")
	}
	resolved := cmd().(resolvedCopyMsg)
	_, copyCmd := m.Update(resolved)
	if copyCmd == nil {
		t.Fatal("resolved password was not copied")
	}
	_, expiryCmd := m.Update(copyCmd())
	if expiryCmd == nil || m.clipboardExpires.IsZero() {
		t.Fatal("password copy did not schedule clipboard expiry")
	}
	if m.notice != "password copied" || driver.writes != 1 || string(driver.value) != "RESOLVED-SECRET" {
		t.Fatal("password was not copied through the managed clipboard")
	}

	_, clearCmd := m.Update(clipboardExpiryMsg{id: m.clipGeneration, now: m.clipboardExpires})
	if clearCmd == nil {
		t.Fatal("clipboard expiry did not clear the owned value")
	}
	m.Update(clearCmd())
	if driver.writes != 2 || len(driver.value) != 0 {
		t.Fatal("owned clipboard value was not cleared at expiry")
	}
}

func TestPasswordCopyDoesNotFallBackWhenResolutionFails(t *testing.T) {
	m, backend := operationModel(t)
	backend.failure = vault.ErrWrite
	driver := &observedClipboard{}
	m.clipboard = clipboard.New(driver)

	cmd := m.copyField("Password")
	if cmd == nil {
		t.Fatal("password resolution was not attempted")
	}
	m.Update(cmd())
	if m.notice != "field unavailable · reload if vault changed" || driver.writes != 0 {
		t.Fatal("failed password resolution wrote to the clipboard")
	}
}
