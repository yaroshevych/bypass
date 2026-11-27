package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestKittyTerminalPreviewAndClear(t *testing.T) {
	var out bytes.Buffer
	terminal := NewKittyTerminal(&out, true)
	png := []byte("image bytes")
	marker := terminal.SetPreview(png, 4, 2, 30, 10)
	frame := marker + strings.Repeat(" ", 80)
	// Bubble Tea uses ansi.Truncate before writing each row.
	frame = ansi.Truncate(frame, 80, "")
	if _, err := terminal.Write([]byte(frame)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "\x1b[3;5H") || !strings.Contains(got, base64.StdEncoding.EncodeToString(png)) || strings.Contains(got, kittyMarkerPrefix) {
		t.Fatal("preview transmission was not emitted at the requested position")
	}
	out.Reset()
	terminal.Clear()
	if !strings.Contains(out.String(), fmt.Sprintf("a=d,d=I,i=%d", terminal.imageID)) {
		t.Fatal("preview was not deleted")
	}
	out.Reset()
	_, _ = terminal.Write([]byte(marker))
	if out.Len() != 0 {
		t.Fatal("stale frame displayed a deleted preview")
	}
}

func TestKittyTerminalChunksLargeImages(t *testing.T) {
	var out bytes.Buffer
	terminal := NewKittyTerminal(&out, true)
	png := bytes.Repeat([]byte{42}, 7000)
	marker := terminal.SetPreview(png, 0, 0, 2, 2)
	_, err := terminal.Write([]byte(marker))
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "m=1;") || !strings.Contains(text, "m=0;") {
		t.Fatal("image was not sent in chunks")
	}
}

func TestKittyTerminalUnsupported(t *testing.T) {
	var out bytes.Buffer
	terminal := NewKittyTerminal(&out, false)
	if terminal.SetPreview([]byte("data"), 0, 0, 10, 10) != "" {
		t.Fatal("unsupported terminal accepted an image")
	}
	terminal.Clear()
	if out.Len() != 0 {
		t.Fatal("unsupported terminal received graphics commands")
	}
}

type kittyTestModel struct{ marker string }

func (m kittyTestModel) Init() tea.Cmd                       { return nil }
func (m kittyTestModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m kittyTestModel) View() string                        { return "preview\n" + m.marker + " " }

type kittyCapture struct {
	bytes.Buffer
	transmitted chan struct{}
}

func (w *kittyCapture) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("\x1b_Ga=T")) {
		select {
		case w.transmitted <- struct{}{}:
		default:
		}
	}
	return w.Buffer.Write(p)
}

func TestKittyTerminalBubbleTeaRenderer(t *testing.T) {
	out := &kittyCapture{transmitted: make(chan struct{}, 1)}
	terminal := NewKittyTerminal(out, true)
	marker := terminal.SetPreview([]byte("example"), 1, 1, 4, 4)
	program := tea.NewProgram(kittyTestModel{marker}, tea.WithInput(nil), tea.WithOutput(terminal), tea.WithoutSignalHandler(), tea.WithAltScreen())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	select {
	case <-out.transmitted:
	case err := <-done:
		t.Fatalf("program exited before image transmission: %v", err)
	case <-time.After(5 * time.Second):
		program.Quit()
		t.Fatal("renderer did not transmit image")
	}
	program.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program did not exit")
	}
	terminal.Clear()
	if !strings.Contains(out.String(), "a=d,d=I") {
		t.Fatal("image deletion not emitted")
	}
}

func TestKittyTerminalSingleDimensionPreservesAspectRatio(t *testing.T) {
	var out bytes.Buffer
	terminal := NewKittyTerminal(&out, true)
	marker := terminal.SetPreview([]byte("image"), 0, 0, 32, 0)
	if _, err := terminal.Write([]byte(marker)); err != nil {
		t.Fatal(err)
	}
	command := out.String()
	if !strings.Contains(command, ",c=32,") || strings.Contains(command, ",r=") {
		t.Fatal("preview must specify only its limiting dimension")
	}
}
