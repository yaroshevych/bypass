package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestDialogsKeepActionsVisibleInSmallWindows(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {60, 20}, {80, 24}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			m.path = "/tmp/example.kdbx"
			for _, label := range []string{"Password", "Open", "Cancel"} {
				if !strings.Contains(strings.Join(m.unlockLines(), "\n"), label) {
					t.Errorf("%s %v unlock hides %q", appearance, size, label)
				}
			}
			m.openPalette("actions")
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
			for _, label := range []string{"Deny all", "Approve all", "Review session permissions", "Close"} {
				if !strings.Contains(view, label) {
					t.Errorf("%s %v requests hides %q", appearance, size, label)
				}
			}
		}
	}
}

func TestUnlockMouseUsesVisibleDialogBounds(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
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
			if !m.smallDialogScreen() {
				m.pointer(tea.MouseMsg{X: 0, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				if m.screen != unlock {
					t.Fatalf("%s %v: click outside dialog activated Cancel", appearance, size)
				}
			}
			m.pointer(tea.MouseMsg{X: column, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.screen != browse {
				t.Fatalf("%s %v: click on Cancel did not close dialog", appearance, size)
			}
		}
	}
}

func TestUnlockErrorWrapsWithoutHidingActions(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {60, 20}, {80, 24}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height, m.path = size[0], size[1], "/tmp/example.kdbx"
			m.err = "Could not unlock. Check credentials, file access and backend support."
			view := ansi.Strip(strings.Join(m.unlockLines(), "\n"))
			for _, text := range []string{"Could not unlock.", "backend support.", "Password", "Open", "Cancel"} {
				if !strings.Contains(view, text) {
					t.Errorf("%s %v unlock hides %q", appearance, size, text)
				}
			}
		}
	}
}

func TestErrorStatesWrapInSmallWindows(t *testing.T) {
	const message = "Could not unlock. Check credentials, file access and backend support."
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
			for _, screen := range []string{"unlock", "picker", "palette", "group", "generator", "delete", "external", "unsaved", "pin-setup", "vault", "pin", "request", "reset", "editor", "editor-validation", "totp", "attachments"} {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", appearance, size[0], size[1], screen), func(t *testing.T) {
					m := New(Options{})
					m.settings.Appearance = appearance
					m.width, m.height, m.path = size[0], size[1], "/tmp/example.kdbx"
					var lines []string
					switch screen {
					case "unlock":
						m.err = message
						lines = m.unlockLines()
					case "picker":
						m.err = message
						lines = m.pickerLines()
					case "palette":
						m.openPalette("actions")
						m.palette.err = message
						lines = m.paletteLines()
					case "group", "generator", "delete", "external", "unsaved", "pin-setup":
						m.dialog = &actionDialog{kind: screen, title: screen, err: message, options: []string{"Cancel", "Continue"}}
						lines = m.dialogLines()
					case "vault":
						m.lifecycle = &lifecycleState{kind: "create", inputs: make([]input, 5), err: message}
						lines = m.lifecycleLines()
					case "pin":
						m.pinError = message
						lines = m.pinLines()
					case "request", "reset":
						p := &runpkg.Pending{Context: context.Background()}
						m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, err: message}
						if screen == "reset" {
							m.runs.review = false
							m.openRunPermissions()
							m.dialog.err = message
						}
						if screen == "reset" {
							lines = m.dialogLines()
						} else {
							lines = m.runDialogView()
						}
					case "editor", "editor-validation":
						e := newEntryEditor(nil, "root")
						e.compact = appearance == "compact"
						e.focus = editSave
						if screen == "editor-validation" {
							e.focus, e.validationFocus, e.validation = editPassword, editPassword, message
						} else {
							e.err = message
						}
						lines = e.render(30, max(8, size[1]-6))
					case "totp":
						lines = m.totpLines(&extraState{err: message})
					case "attachments":
						m.ws.Open(fixture("/tmp/errors.kdbx"))
						session := m.ws.ActiveSession()
						lines = m.attachmentLines(&extraState{sessionID: session.ID, entryUUID: "a", groupUUID: "root", err: message})
					}
					view := ansi.Strip(strings.Join(lines, "\n"))
					if !strings.Contains(strings.ToLower(view), "could not unlock.") || !strings.Contains(strings.ToLower(view), "support") {
						t.Errorf("error text is not fully visible")
					}
				})
			}
		}
	}
}

func TestLongErrorNoticeRemainsVisible(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.width, m.height = 48, 16
		m.ws.Open(fixture("/tmp/example.kdbx"))
		m.notice = "reload failed · check the vault file access and credentials before retrying"
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "reload failed") || !strings.Contains(view, "before retrying") {
			t.Errorf("%s: notice was clipped", appearance)
		}
		if len(strings.Split(view, "\n")) != m.height {
			t.Errorf("%s: notice changed the terminal height", appearance)
		}
	}
}

func TestVaultFormMouseUsesVisibleTargets(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
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
			if size[1] > 16 {
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
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
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
			if size[1] > 16 {
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
		for _, size := range [][2]int{{48, 16}, {60, 20}, {80, 24}} {
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

func TestVaultAndDestinationCancelMouseActions(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.width, m.height = 80, 24
		m.lifecycle = &lifecycleState{kind: "create", inputs: make([]input, 5), focus: 4}
		click := func(lines []string, label string) tea.MouseMsg {
			for y, line := range lines {
				plain := ansi.Strip(line)
				if x := strings.Index(plain, label); x >= 0 {
					return tea.MouseMsg{X: len([]rune(plain[:x])), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
				}
			}
			t.Fatalf("%s: %s button not visible", appearance, label)
			return tea.MouseMsg{}
		}
		m.lifecyclePointer(click(m.lifecycleLines(), "Cancel"))
		if m.lifecycle != nil {
			t.Fatalf("%s: Cancel did not close vault form", appearance)
		}
		m.dialog = &actionDialog{kind: "destination", title: "Move entry", groups: []vault.Group{{Path: "Alpha"}}}
		m.destinationPointer(click(m.dialogLines(), "Cancel"))
		if m.dialog != nil {
			t.Fatalf("%s: Cancel did not close destination", appearance)
		}
	}
}

func TestDialogClicksFollowRowsWhenErrorIsVisible(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, kind := range []string{"unlock", "vault", "destination"} {
			t.Run(appearance+"/"+kind, func(t *testing.T) {
				m := New(Options{})
				m.settings.Appearance, m.width, m.height = appearance, 48, 16
				var lines []string
				switch kind {
				case "unlock":
					m.screen, m.err = unlock, "An error that wraps on a narrow terminal and remains visible."
					lines = m.unlockLines()
				case "vault":
					m.lifecycle = &lifecycleState{kind: "create", inputs: make([]input, 5), focus: 4, err: "An error that wraps on a narrow terminal and remains visible."}
					lines = m.lifecycleLines()
				case "destination":
					m.dialog = &actionDialog{kind: "destination", title: "Move entry", groups: []vault.Group{{Path: "Alpha"}}, err: "An error that wraps on a narrow terminal and remains visible."}
					lines = m.dialogLines()
				}
				row, column := -1, -1
				for y, line := range lines {
					plain := ansi.Strip(line)
					if x := strings.Index(plain, "Cancel"); x >= 0 {
						row, column = y, len([]rune(plain[:x]))
					}
				}
				if row < 0 {
					t.Fatal("Cancel not visible")
				}
				click := tea.MouseMsg{X: column, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
				switch kind {
				case "unlock":
					m.pointer(click)
					if m.screen != browse {
						t.Fatal("Cancel missed")
					}
				case "vault":
					m.lifecyclePointer(click)
					if m.lifecycle != nil {
						t.Fatal("Cancel missed")
					}
				case "destination":
					m.destinationPointer(click)
					if m.dialog != nil {
						t.Fatal("Cancel missed")
					}
				}
			})
		}
	}
}

func TestPaletteErrorIsNotAnActionTarget(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance, m.width, m.height = appearance, 48, 16
		m.openPalette("actions")
		m.palette.err = "An error that wraps on a narrow terminal."
		found := false
		for y, line := range m.paletteLines() {
			plain := ansi.Strip(line)
			if x := strings.Index(plain, "An error"); x >= 0 {
				m.palettePointer(tea.MouseMsg{X: len([]rune(plain[:x])), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				if m.palette == nil || m.palette.scope != "actions" {
					t.Fatalf("%s: error click activated an action", appearance)
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s: error was not visible", appearance)
		}
	}
}

func TestHistoryMouseUsesOverlayGeometry(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.width, m.height = 80, 24
		m.extras = &extraState{mode: extraHistory, history: []vault.Entry{
			{Fields: []vault.Field{{Name: "Title", Value: "First snapshot"}}},
			{Fields: []vault.Field{{Name: "Title", Value: "Second snapshot"}}},
		}}
		row, column := -1, -1
		for y, line := range m.historyLines(m.extras) {
			plain := ansi.Strip(line)
			if x := strings.Index(plain, "Second snapshot"); x >= 0 {
				row, column = y, len([]rune(plain[:x]))
			}
		}
		if row < 0 {
			t.Fatalf("%s: second history entry hidden", appearance)
		}
		m.extrasPointer(tea.MouseMsg{X: column, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if m.extras.historyIndex != 1 {
			t.Fatalf("%s: history click selected %d", appearance, m.extras.historyIndex)
		}
	}
}

func TestOpenDialogsAndEditorSurviveResize(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.width, m.height = 80, 24
		m.openPalette("themes")
		m.palette.query.set("Nord")
		m.refreshPalette()
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			if m.palette == nil || m.palette.query.value() != "Nord" || !strings.Contains(strings.Join(m.paletteLines(), "\n"), "Nord") {
				t.Fatalf("%s %v: theme picker lost its query on resize", appearance, size)
			}
		}
		m.palette = nil
		m.dialog = &actionDialog{kind: "destination", title: "Move entry", groups: []vault.Group{{Path: "Alpha"}, {Path: "Beta"}}, selected: 1}
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			if m.dialog == nil || m.dialog.selected != 1 || !strings.Contains(strings.Join(m.dialogLines(), "\n"), "Beta") {
				t.Fatalf("%s %v: destination selection lost on resize", appearance, size)
			}
		}
		e := newEntryEditor(nil, "root")
		e.compact = appearance == "compact"
		e.inputs[editTitle].set("draft survives")
		for _, height := range []int{8, 18} {
			if !strings.Contains(strings.Join(e.render(28, height), "\n"), "draft survives") || e.inputs[editTitle].value() != "draft survives" {
				t.Fatalf("%s: editor draft lost on resize to height %d", appearance, height)
			}
		}
	}
}

func TestLongNoticeLeavesRoomForWorkspace(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.width, m.height, m.settings.Appearance = 48, 16, appearance
		m.notice = strings.Repeat("long notice ", 100)
		lines := m.noticeOverflowLines()
		if len(lines) > 3 || len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "…") {
			t.Fatalf("%s: notice overflow is not bounded and indicated", appearance)
		}
	}
}

func TestSmallPaneDialogsUseFullscreenAndRemainInteractive(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height, m.screen = 48, 16, unlock
			m.path = "/tmp/example.kdbx"
			view := ansi.Strip(m.View())
			for _, label := range []string{"UNLOCK VAULT", "Password", "Open", "Cancel"} {
				if !strings.Contains(view, label) {
					t.Errorf("unlock hides %q: %q", label, view)
				}
			}
			if strings.ContainsAny(view, "┌┐└┘╭╮╰╯") {
				t.Fatal("small dialog has a border")
			}
			m.screen = browse
			m.openPalette("actions")
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("theme")})
			view = ansi.Strip(m.View())
			if !strings.Contains(view, "theme") {
				t.Fatalf("palette filter not visible: %q", view)
			}
			m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if m.palette != nil {
				t.Fatal("Esc did not close palette")
			}
		})
	}
}

func TestTooSmallScreenShowsCenteredSize(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 14
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, text := range []string{"Terminal too small", "Need 38x16; current 48x14", "Resize to continue"} {
		row := (m.height-3)/2 + i
		if strings.TrimSpace(lines[row]) != text {
			t.Fatalf("row %d: got %q", row, lines[row])
		}
		if strings.Index(lines[row], text) != (m.width-len(text))/2 {
			t.Fatalf("row %d not centered: %q", row, lines[row])
		}
	}
}

func TestDialogButtonsUseMouseTargets(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
			for _, kind := range []string{"group", "generator", "pin-setup", "delete", "unsaved", "discardReload"} {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", appearance, size[0], size[1], kind), func(t *testing.T) {
					m := New(Options{})
					m.width, m.height, m.settings.Appearance = size[0], size[1], appearance
					m.dialog = &actionDialog{kind: kind, title: "Test", options: []string{"Cancel", "Delete"}, err: "Please correct this error"}
					if kind == "unsaved" {
						m.dialog.options = []string{"Save", "Discard", "Cancel"}
					}
					for y, line := range m.dialogLines() {
						plain := ansi.Strip(line)
						if x := strings.Index(plain, "Cancel"); x >= 0 {
							m.pointer(tea.MouseMsg{X: len([]rune(plain[:x])), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
							if m.dialog != nil {
								t.Fatal("Cancel click did not close dialog")
							}
							return
						}
					}
					t.Fatal("Cancel is not visible")
				})
			}
		}
	}
}

func TestPrivacyDialogMouseUsesSafeConfirmation(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.width, m.height, m.settings.Appearance = 48, 16, appearance
		if err := m.gate.Configure([]byte("1234")); err != nil {
			t.Fatal(err)
		}
		if !m.gate.Protect() {
			t.Fatal("could not protect screen")
		}
		click := func(label string) {
			t.Helper()
			for y, line := range m.pinLines() {
				plain := ansi.Strip(line)
				if x := strings.Index(plain, label); x >= 0 {
					m.pointer(tea.MouseMsg{X: len([]rune(plain[:x])), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
					return
				}
			}
			t.Fatalf("%s is not visible", label)
		}
		click("Full Lock")
		if !m.pinFullLock || !m.gate.Protected() {
			t.Fatal("Full Lock did not open protected confirmation")
		}
		click("Cancel")
		if m.pinFullLock || !m.gate.Protected() {
			t.Fatal("Cancel did not return to protected screen")
		}
	}
}

func TestSmallLayoutTemporarilyUsesCompactAppearance(t *testing.T) {
	m := New(Options{})
	m.settings.Appearance = "classic"
	m.width, m.height = 120, 35
	if m.compactAppearance() {
		t.Fatal("large layout ignored classic setting")
	}
	m.Update(tea.WindowSizeMsg{Width: 48, Height: 16})
	if !m.compactAppearance() || m.settings.Appearance != "classic" {
		t.Fatal("small layout did not temporarily use compact")
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	if m.compactAppearance() || m.settings.Appearance != "classic" {
		t.Fatal("large layout did not restore classic")
	}
}

func TestSmallDialogControlsUseFullWidth(t *testing.T) {
	m := New(Options{})
	m.width, m.height, m.screen = 48, 16, unlock
	m.path = "/tmp/example.kdbx"
	opts := m.unlockOptions()
	for _, action := range opts.Actions {
		if got := ansi.StringWidth(action.Text); got != m.width {
			t.Errorf("unlock action width = %d, want %d", got, m.width)
		}
	}
	if got := ansi.StringWidth(opts.Body[3]); got != m.width {
		t.Errorf("unlock input width = %d, want %d", got, m.width)
	}
	m.openPalette("actions")
	if got := m.paletteListWidth(m.paletteChoices()); got != m.width {
		t.Errorf("palette list width = %d, want %d", got, m.width)
	}
	m.palette = nil
	m.runs = &runState{visible: true}
	for _, action := range m.runLayerOptions(m.runRequestsLayout()).Actions {
		if got := ansi.StringWidth(action.Text); got != m.width {
			t.Errorf("request action width = %d, want %d", got, m.width)
		}
	}
}

func TestExtrasWheelNavigatesLists(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		t.Run(appearance, func(t *testing.T) {
			db := extrasEntryDB("/tmp/wheel-test.kdbx")
			db.Entries[0].AttachmentNames = []string{"a", "b"}
			m := newExtrasTestModel(t, db, &extrasTestBackend{})
			defer m.Close()
			m.settings.Appearance, m.width, m.height = appearance, 48, 16
			m.extras = &extraState{mode: extraHistory, history: []vault.Entry{{}, {}, {}}}
			wheel := func(button tea.MouseButton) {
				m.extrasPointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: button})
			}
			wheel(tea.MouseButtonWheelDown)
			if m.extras.historyIndex != 1 {
				t.Fatal("history wheel did not advance")
			}
			wheel(tea.MouseButtonWheelUp)
			if m.extras.historyIndex != 0 {
				t.Fatal("history wheel did not reverse")
			}
			m.openAttachments()
			if m.extras == nil || m.extras.mode != extraAttachments {
				t.Fatal("attachments did not open")
			}
			wheel(tea.MouseButtonWheelDown)
			if m.extras.selected != 1 {
				t.Fatal("attachment wheel did not advance")
			}
			m.extras.phase = "path"
			wheel(tea.MouseButtonWheelDown)
			if m.extras.selected != 1 {
				t.Fatal("wheel changed selection while editing path")
			}
			m.extras.phase = ""
			wheel(tea.MouseButtonWheelUp)
			if m.extras.selected != 0 {
				t.Fatal("attachment wheel did not reverse")
			}
		})
	}
}
