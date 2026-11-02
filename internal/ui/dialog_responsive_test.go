package ui

import (
	"context"
	"strings"
	"testing"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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
			m.lifecycle = &lifecycleState{kind: "create", inputs: make([]input, 5)}
			for _, action := range []struct {
				focus int
				label string
			}{{3, "Save"}, {4, "Cancel"}} {
				m.lifecycle.focus = action.focus
				if !strings.Contains(strings.Join(m.lifecycleLines(), "\n"), action.label) {
					t.Errorf("%s %v vault form hides focused %q", appearance, size, action.label)
				}
			}
			m.lifecycle = nil
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

func TestUnlockMouseUsesVisibleDialogBounds(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {80, 24}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height, m.screen = size[0], size[1], unlock
			m.path = "/tmp/example.kdbx"
			row, column := -1, -1
			for y, line := range m.unlockLines() {
				plain := ansi.Strip(line)
				if x := strings.Index(plain, "Cancel"); x >= 0 {
					row, column = y, len([]rune(plain[:x]))
				}
			}
			if row < 0 {
				t.Fatalf("%s %v: Cancel not visible", appearance, size)
			}
			m.pointer(tea.MouseMsg{X: 0, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.screen != unlock {
				t.Fatalf("%s %v: click outside dialog activated Cancel", appearance, size)
			}
			m.pointer(tea.MouseMsg{X: column, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.screen != browse {
				t.Fatalf("%s %v: click on Cancel did not close dialog", appearance, size)
			}
		}
	}
}

func TestVaultFormMouseUsesVisibleTargets(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {80, 24}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			m.lifecycle = &lifecycleState{kind: "create", inputs: make([]input, 5)}
			row, column := -1, -1
			for y, line := range m.lifecycleLines() {
				plain := ansi.Strip(line)
				if x := strings.Index(plain, "New .kdbx path"); x >= 0 {
					row, column = y, len([]rune(plain[:x]))
				}
			}
			if row < 0 {
				t.Fatalf("%s %v: path field not visible", appearance, size)
			}
			m.lifecyclePointer(tea.MouseMsg{X: 0, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.lifecycle.focus != 0 {
				t.Fatal("outside click changed focus")
			}
			m.lifecyclePointer(tea.MouseMsg{X: column, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.lifecycle.focus != 0 {
				t.Fatalf("%s %v: path click did not focus field", appearance, size)
			}
			if size[1] > 14 {
				for y, line := range m.lifecycleLines() {
					plain := ansi.Strip(line)
					if x := strings.Index(plain, "New password"); x >= 0 {
						m.lifecyclePointer(tea.MouseMsg{X: len([]rune(plain[:x])), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
						break
					}
				}
				if m.lifecycle.focus != 1 {
					t.Fatalf("%s %v: password click did not focus field", appearance, size)
				}
			}
		}
	}
}

func TestDestinationMouseUsesVisibleRows(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {80, 24}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			m.dialog = &actionDialog{kind: "destination", title: "Move entry", groups: []vault.Group{{Path: "Alpha"}, {Path: "Beta"}}}
			row, column := -1, -1
			for y, line := range m.dialogLines() {
				plain := ansi.Strip(line)
				if x := strings.Index(plain, "Alpha"); x >= 0 {
					row, column = y, len([]rune(plain[:x]))
				}
			}
			if row < 0 {
				t.Fatalf("%s %v: destination hidden", appearance, size)
			}
			m.destinationPointer(tea.MouseMsg{X: 0, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.dialog.selected != 0 {
				t.Fatal("outside click changed selection")
			}
			m.destinationPointer(tea.MouseMsg{X: column, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.dialog.selected != 0 {
				t.Fatalf("%s %v: destination click missed", appearance, size)
			}
			if size[1] > 14 {
				for y, line := range m.dialogLines() {
					plain := ansi.Strip(line)
					if x := strings.Index(plain, "Beta"); x >= 0 {
						m.destinationPointer(tea.MouseMsg{X: len([]rune(plain[:x])), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
						break
					}
				}
				if m.dialog.selected != 1 {
					t.Fatalf("%s %v: second destination click missed", appearance, size)
				}
			}
		}
	}
}

func TestWelcomeMouseDistinguishesRecentVaultRows(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.width, m.height = 80, 24
		m.recent = []string{"/first/shared.kdbx", "/second/shared.kdbx"}
		row, column := -1, -1
		for y, line := range m.welcomeLines() {
			plain := ansi.Strip(line)
			if x := strings.Index(plain, "/second"); x >= 0 {
				row, column = y, len([]rune(plain[:x]))
			}
		}
		if row < 0 {
			t.Fatalf("%s: second recent vault not visible", appearance)
		}
		m.welcomePointer(tea.MouseMsg{X: 0, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if m.landingFocus != 0 {
			t.Fatal("outside click changed welcome selection")
		}
		m.welcomePointer(tea.MouseMsg{X: column, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if m.landingFocus != 1 {
			t.Fatalf("%s: click selected wrong recent vault", appearance)
		}
	}
}

func TestMoreDialogsAndEditorAtSmallSizes(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {60, 20}, {80, 24}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			m.dialog = &actionDialog{kind: "delete", title: "Delete", options: []string{"Cancel", "Delete"}}
			for _, label := range []string{"Cancel", "Delete"} {
				if !strings.Contains(strings.Join(m.dialogLines(), "\n"), label) {
					t.Errorf("%s %v confirmation hides %q", appearance, size, label)
				}
			}
			m.dialog = nil
			for _, label := range []string{"Unlock", "Full Lock", "Quit"} {
				if !strings.Contains(strings.Join(m.pinLines(), "\n"), label) {
					t.Errorf("%s %v PIN hides %q", appearance, size, label)
				}
			}
			if !strings.Contains(strings.Join(m.pickerLines(), "\n"), "VAULT") {
				t.Errorf("%s %v picker title hidden", appearance, size)
			}
			e := newEntryEditor(nil, "root")
			e.compact = appearance == "compact"
			for _, action := range []struct {
				focus int
				label string
			}{{editSave, "Save"}, {editCancel, "Cancel"}} {
				e.focus = action.focus
				if !strings.Contains(strings.Join(e.render(28, max(4, size[1]-6)), "\n"), action.label) {
					t.Errorf("%s %v editor hides focused %q", appearance, size, action.label)
				}
			}
		}
	}
}
