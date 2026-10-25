package ui

import (
	"context"
	"testing"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/vault"
)

type nativeCopyBackend struct {
	*operationBackend
	entries []string
}

func (b *nativeCopyBackend) CopyPassword(_ context.Context, _ *vault.Database, entry string) error {
	b.entries = append(b.entries, entry)
	return nil
}

type observedClipboard struct{ reads, writes int }

func (c *observedClipboard) Read(context.Context) ([]byte, error) { c.reads++; return nil, nil }
func (c *observedClipboard) Write(context.Context, []byte) error  { c.writes++; return nil }

func TestNativePasswordCopiesFollowSelectionWithoutReadingOrClearingClipboard(t *testing.T) {
	m, backend := operationModel(t)
	native := &nativeCopyBackend{operationBackend: backend}
	m.backend = native
	driver := &observedClipboard{}
	m.clipboard = clipboard.New(driver)
	for _, entry := range []string{"a", "b", "a"} {
		m.ws.ActiveView().SelectedUUID = entry
		execute(t, m, m.copyField("Password"))
		if !m.clipboardExpires.IsZero() {
			t.Fatal("native password copy scheduled clearing")
		}
	}
	if len(native.entries) != 3 || native.entries[0] != "a" || native.entries[1] != "b" || native.entries[2] != "a" {
		t.Fatal("native copy did not follow rapid selection changes")
	}
	m.onTick(m.now().Add(time.Hour))
	m.clearClipboard()()
	m.Close()
	if driver.reads != 0 || driver.writes != 0 {
		t.Fatal("Bypass accessed the native clipboard")
	}
}

func TestPasswordCopyDoesNotFallBackToPlaintextResolution(t *testing.T) {
	m, backend := operationModel(t)
	backend.failure = vault.ErrWrite
	if cmd := m.copyField("Password"); cmd != nil || m.notice != "native password copy unavailable" {
		t.Fatal("password copy should require the native copier")
	}
}
