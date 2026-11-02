package ui

import (
	"context"
	"strings"
	"testing"

	runpkg "bypass/internal/run"
)

func TestDialogsKeepActionsVisibleInSmallWindows(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {60, 20}, {80, 24}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			m.path = "/tmp/example.kdbx"
			for _, label := range []string{"Password", "Open", "Cancel"} {
				if !strings.Contains(strings.Join(m.unlockLines(), "\n"), label) {
					t.Errorf("%s %v unlock hides %q", appearance, size, label)
				}
			}
			m.openPalette("actions", false)
			if !strings.Contains(strings.Join(m.paletteLines(), "\n"), "COMMANDS") {
				t.Errorf("%s %v hides command palette", appearance, size)
			}
			m.palette = nil
			p := &runpkg.Pending{Context: context.Background()}
			m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}}
			view := strings.Join(m.runDialogView(), "\n")
			for _, label := range []string{"Deny all", "Approve all", "Reset saved permissions", "Close"} {
				if !strings.Contains(view, label) {
					t.Errorf("%s %v requests hides %q", appearance, size, label)
				}
			}
		}
	}
}
