package ui

import "testing"

func executePINLockFromPalette(t *testing.T, m *Model) {
	t.Helper()
	m.openPalette("actions")
	for i, choice := range m.paletteChoices() {
		if choice.id == "protect" {
			if choice.reason != "" {
				t.Fatalf("lock with PIN disabled: %s", choice.reason)
			}
			m.palette.selected = i
			m.executePalette()
			return
		}
	}
	t.Fatal("lock with PIN missing from palette")
}

func TestPalettePINLockPromptsForSetupAndLocks(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/fixture.kdbx"))
	m.normalize()
	executePINLockFromPalette(t, m)
	if m.dialog == nil || m.dialog.kind != "pin-setup" {
		t.Fatal("missing PIN setup dialog")
	}
	m.key(key("1234"))
	m.key(key("enter"))
	if m.dialog == nil || m.dialog.kind != "pin-confirm" {
		t.Fatal("missing PIN confirmation")
	}
	m.key(key("1234"))
	m.key(key("enter"))
	if m.dialog != nil || !m.gate.Configured() || !m.gate.Protected() {
		t.Fatal("PIN setup did not lock the UI")
	}
}

func TestPalettePINLockAlwaysAvailable(t *testing.T) {
	m := New(Options{})
	executePINLockFromPalette(t, m)
	if m.dialog != nil || m.notice == "" {
		t.Fatal("expected notice when no vault is unlocked")
	}
	m.ws.Open(fixture("/tmp/fixture.kdbx"))
	m.normalize()
	if err := m.gate.Configure([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	executePINLockFromPalette(t, m)
	if !m.gate.Protected() || m.dialog != nil {
		t.Fatal("configured PIN did not lock the UI directly")
	}
}
