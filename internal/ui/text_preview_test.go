package ui

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func textPreviewModel(t *testing.T, content []byte) *Model {
	t.Helper()
	backend := &extrasTestBackend{exportData: content}
	m := newExtrasTestModel(t, extrasEntryDB(filepath.Join(t.TempDir(), "vault.kdbx")), backend)
	t.Cleanup(m.Close)
	m.previewTerminal = nil
	v := m.ws.ActiveView()
	v.Focus, v.FieldIndex = 2, len(m.fieldNames())
	return m
}

func TestTextPreviewWithoutKittyAndScrolling(t *testing.T) {
	content := []byte("First line\r\n" + strings.Repeat("middle line\n", 60) + "Last line")
	m := textPreviewModel(t, content)
	cmd := m.toggleReveal()
	if cmd == nil {
		t.Fatal("text preview did not start")
	}
	m.Update(cmd())
	if m.extras == nil || !m.extras.previewIsText || m.extras.err != "" {
		t.Fatal("text did not load without Kitty")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "First line") || strings.Contains(view, "Last line") {
		t.Fatal("unexpected initial text viewport")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.extras.valueOffset == 0 {
		t.Fatal("page down did not scroll")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(ansi.Strip(m.View()), "Last line") {
		t.Fatal("end did not reach last line")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.extras.valueOffset != 0 {
		t.Fatal("home did not return to the start")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.extras.valueOffset != 1 {
		t.Fatal("mouse wheel did not scroll text")
	}
	raw := m.extras.previewText
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.extras != nil {
		t.Fatal("preview did not close")
	}
	if !bytes.Equal(raw, make([]byte, len(raw))) {
		t.Fatal("closing did not clear text bytes")
	}
}

func TestTextPreviewSanitizesControlsAndHandlesEmpty(t *testing.T) {
	m := textPreviewModel(t, []byte("hello\x1b[2Jworld\tend"))
	cmd := m.toggleReveal()
	m.Update(cmd())
	if m.extras == nil || !m.extras.previewIsText {
		t.Fatal("text did not load")
	}
	view := m.View()
	if strings.Contains(view, "\x1b[2J") || !strings.Contains(ansi.Strip(view), "helloworld    end") {
		t.Fatal("terminal escape was not sanitized")
	}
	m.clearExtras()
	empty := textPreviewModel(t, nil)
	cmd = empty.toggleReveal()
	empty.Update(cmd())
	if empty.extras == nil || !empty.extras.previewIsText || empty.extras.err != "" {
		t.Fatal("empty file did not preview")
	}
}

func TestTextPreviewLockRejectsLateResult(t *testing.T) {
	m := textPreviewModel(t, []byte("private synthetic text"))
	cmd := m.toggleReveal()
	msg := cmd().(imagePreviewMsg)
	raw := msg.png
	m.lockCurrent(true)
	m.Update(msg)
	if m.extras != nil || strings.Contains(m.View(), "private synthetic text") {
		t.Fatal("late text exposed after lock")
	}
	if !bytes.Equal(raw, make([]byte, len(raw))) {
		t.Fatal("late text was not cleared")
	}
}

func TestTextPreviewRejectsBinaryAndOversized(t *testing.T) {
	if isPlaintextAttachment([]byte{0, 1, 2}) || isPlaintextAttachment([]byte{0xff, 0xfe}) {
		t.Fatal("binary was classified as text")
	}
	m := textPreviewModel(t, bytes.Repeat([]byte("a"), maxTextPreviewBytes+1))
	cmd := m.toggleReveal()
	m.Update(cmd())
	if m.extras == nil || !strings.Contains(m.extras.err, "too large") {
		t.Fatal("oversized text was not rejected")
	}
}

func TestTextPreviewFitsSmallTerminals(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := textPreviewModel(t, []byte("visible first line\nsecond line"))
		m.settings.Appearance = appearance
		m.width, m.height = 48, 16
		cmd := m.toggleReveal()
		m.Update(cmd())
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "visible first line") || !strings.Contains(view, "esc close") {
			t.Errorf("%s: text or close hint missing", appearance)
		}
	}
}

func TestImagePreviewStillRequiresKitty(t *testing.T) {
	m := textPreviewModel(t, previewTestImage(t))
	cmd := m.toggleReveal()
	m.Update(cmd())
	if m.extras == nil || !strings.Contains(m.extras.err, "Kitty graphics") {
		t.Fatal("unsupported graphics terminal not reported")
	}
}

func TestTextPreviewWrapsLongLinesAndPreservesBlankLines(t *testing.T) {
	m := textPreviewModel(t, []byte(strings.Repeat("x", 120)+"\n\nend"))
	m.width, m.height = 48, 16
	cmd := m.toggleReveal()
	m.Update(cmd())
	lines := m.textPreviewLines(m.extras)
	if len(lines) < 5 || lines[len(lines)-2] != "" || lines[len(lines)-1] != "end" {
		t.Fatal("text wrapping or blank lines were not preserved")
	}
}
