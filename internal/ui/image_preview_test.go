package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func previewTestImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	img.Set(2, 3, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func previewTestModel(t *testing.T, names []string, backend *extrasTestBackend) (*Model, *KittyTerminal, *bytes.Buffer) {
	t.Helper()
	out := new(bytes.Buffer)
	terminal := NewKittyTerminal(out, true)
	m := New(Options{Backend: backend, KittyTerminal: terminal})
	t.Cleanup(m.Close)
	m.width, m.height = 100, 30
	db := fixture("/tmp/image-preview.kdbx")
	db.Entries[0].AttachmentNames = names
	m.ws.Open(db)
	m.ws.SetCredentials(m.ws.ActiveSession().ID, vault.Credentials{NoPassword: true})
	m.normalize()
	m.ws.ActiveView().Focus = 2
	m.ws.ActiveView().FieldIndex = len(m.fieldNames())
	return m, terminal, out
}

func TestImagePreviewOpenCloseAndGeometry(t *testing.T) {
	backend := &extrasTestBackend{exportData: previewTestImage(t)}
	m, terminal, out := previewTestModel(t, []string{"image.png"}, backend)
	if got := m.footerContext(); !strings.Contains(got, "v preview") || strings.Contains(got, "c copy") {
		t.Fatalf("footer: %s", got)
	}
	cmd := m.toggleReveal()
	if cmd == nil || m.extras == nil || !m.extras.loading {
		t.Fatal("preview did not start")
	}
	m.Update(cmd())
	if m.extras == nil || m.extras.err != "" || len(m.extras.previewPNG) == 0 {
		t.Fatalf("preview did not load: %#v", m.extras)
	}
	g, _, _, _, _ := m.imagePreviewGeometry()
	if g.Width != 90 || g.Height != 27 {
		t.Fatalf("dialog dimensions = %dx%d, want 90x27", g.Width, g.Height)
	}
	if terminal.cols != 86 || terminal.rows != 0 {
		t.Fatalf("fitted image dimensions = %dx%d", terminal.cols, terminal.rows)
	}
	view := m.View()
	if !strings.Contains(view, m.previewMarker) || strings.Contains(view, string(m.extras.previewPNG)) {
		t.Fatal("terminal marker missing or image leaked into view")
	}
	_, _ = terminal.Write([]byte(view))
	if !strings.Contains(out.String(), "\x1b_Ga=T") {
		t.Fatal("preview was not sent to terminal")
	}
	old := m.previewMarker
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.extras != nil || m.previewMarker != "" || len(terminal.png) != 0 {
		t.Fatal("closing preview did not clear image")
	}
	if !strings.Contains(out.String(), "\x1b_Ga=d") {
		t.Fatal("terminal image was not deleted")
	}
	out.Reset()
	_, _ = terminal.Write([]byte(old))
	if strings.Contains(out.String(), "\x1b_Ga=T") {
		t.Fatal("stale frame restored image")
	}
}

func TestImagePreviewRejectsDuplicatesAndUnsupportedData(t *testing.T) {
	backend := &extrasTestBackend{exportData: []byte("not an image")}
	m, _, _ := previewTestModel(t, []string{"same.png", "same.png"}, backend)
	if cmd := m.toggleReveal(); cmd != nil || !strings.Contains(m.extras.err, "duplicate") || backend.exportCalls != 0 {
		t.Fatal("duplicate name was not rejected")
	}
	m.clearExtras()
	m, _, _ = previewTestModel(t, []string{"data.txt"}, backend)
	cmd := m.toggleReveal()
	if cmd == nil {
		t.Fatal("missing read command")
	}
	m.Update(cmd())
	if m.extras == nil || !strings.Contains(m.extras.err, "not a supported image") {
		t.Fatal("non-image was not rejected")
	}
}

func TestImagePreviewCancelWipesLateResultAndPIN(t *testing.T) {
	backend := &extrasTestBackend{exportData: previewTestImage(t)}
	m, terminal, _ := previewTestModel(t, []string{"image.png"}, backend)
	cmd := m.toggleReveal()
	msg := cmd().(imagePreviewMsg)
	raw := msg.png
	m.concealForPIN()
	if m.extras != nil {
		t.Fatal("PIN did not clear preview")
	}
	m.Update(msg)
	if m.extras != nil || len(terminal.png) != 0 {
		t.Fatal("late result restored image")
	}
	for _, b := range raw {
		if b != 0 {
			t.Fatal("late image was not wiped")
			break
		}
	}
	// Cancellation before the command starts also clears its snapshot and returns safely.
	cmd = m.toggleReveal()
	m.clearExtras()
	m.Update(cmd())
	if m.extras != nil {
		t.Fatal("canceled preview reopened")
	}
}

func TestImagePreviewResizeAndLock(t *testing.T) {
	backend := &extrasTestBackend{exportData: previewTestImage(t)}
	m, terminal, out := previewTestModel(t, []string{"image.png"}, backend)
	cmd := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if cmd == nil {
		t.Fatal("v did not start preview")
	}
	m.Update(cmd())
	old := m.previewMarker
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.previewMarker == old || terminal.cols != 104 || terminal.rows != 0 {
		t.Fatalf("preview was not resized: %dx%d", terminal.cols, terminal.rows)
	}
	out.Reset()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	if m.extras != nil || len(terminal.png) != 0 || m.previewMarker != "" {
		t.Fatal("lock did not clear preview")
	}
	if !strings.Contains(out.String(), "\x1b_Ga=d") {
		t.Fatal("lock did not delete terminal image")
	}
}

func TestImagePreviewImageValidation(t *testing.T) {
	pngBytes, w, h, err := makePreviewPNG(context.Background(), previewTestImage(t))
	if err != nil || w != 20 || h != 10 || len(pngBytes) == 0 {
		t.Fatalf("PNG conversion: %dx%d %v", w, h, err)
	}
	clear(pngBytes)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if data, _, _, err := makePreviewPNG(ctx, previewTestImage(t)); err == nil || data != nil {
		t.Fatal("canceled image conversion succeeded")
	}
}

type blockedPreviewBackend struct {
	*extrasTestBackend
	started, release chan struct{}
	data             []byte
}

func (b *blockedPreviewBackend) ExportAttachment(_ context.Context, _ *vault.Database, _ vault.Credentials, _, _ string) ([]byte, error) {
	close(b.started)
	<-b.release
	return b.data, nil
}

func TestImagePreviewBlockedReadIsCleared(t *testing.T) {
	for _, action := range []string{"pin", "lock"} {
		t.Run(action, func(t *testing.T) {
			backend := &blockedPreviewBackend{extrasTestBackend: &extrasTestBackend{}, started: make(chan struct{}), release: make(chan struct{}), data: previewTestImage(t)}
			out := new(bytes.Buffer)
			terminal := NewKittyTerminal(out, true)
			m := New(Options{Backend: backend, KittyTerminal: terminal})
			defer m.Close()
			db := fixture("/tmp/image-preview-blocked.kdbx")
			db.Entries[0].AttachmentNames = []string{"image.png"}
			m.ws.Open(db)
			m.ws.SetCredentials(m.ws.ActiveSession().ID, vault.Credentials{NoPassword: true})
			m.width, m.height = 100, 30
			m.normalize()
			m.ws.ActiveView().Focus = 2
			m.ws.ActiveView().FieldIndex = len(m.fieldNames())
			cmd := m.toggleReveal()
			if cmd == nil {
				t.Fatal("preview did not start")
			}
			result := make(chan tea.Msg, 1)
			go func() { result <- cmd() }()
			select {
			case <-backend.started:
			case <-time.After(3 * time.Second):
				t.Fatal("read did not start")
			}
			if action == "pin" {
				m.concealForPIN()
			} else {
				m.lockCurrent(true)
			}
			close(backend.release)
			select {
			case msg := <-result:
				m.Update(msg)
			case <-time.After(3 * time.Second):
				t.Fatal("canceled read did not finish")
			}
			if m.extras != nil || len(terminal.png) != 0 {
				t.Fatal("late read restored preview")
			}
			for _, b := range backend.data {
				if b != 0 {
					t.Fatal("late attachment data was not wiped")
					break
				}
			}
		})
	}
}

func TestFitPreviewImagePreservesAspectRatio(t *testing.T) {
	x, y, cols, rows := fitPreviewImage(0, 0, 80, 20, 800, 400, 10, 20)
	if x != 0 || y != 0 || cols != 80 || rows != 0 {
		t.Fatalf("wide image: %d %d %d %d", x, y, cols, rows)
	}
	x, y, cols, rows = fitPreviewImage(0, 0, 80, 20, 400, 800, 10, 20)
	if x != 30 || y != 0 || cols != 0 || rows != 20 {
		t.Fatalf("tall image: %d %d %d %d", x, y, cols, rows)
	}
}
