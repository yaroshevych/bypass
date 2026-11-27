package ui

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 200] /Resources << >> /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 5 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), start)
	return b.Bytes()
}

func TestPDFPreviewPages(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo unavailable")
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm unavailable")
	}
	backend := &extrasTestBackend{exportData: testPDF()}
	m, _, _ := previewTestModel(t, []string{"test.pdf"}, backend)
	cmd := m.toggleReveal()
	m.Update(cmd())
	if m.extras.err != "" || m.extras.previewPage != 1 || m.extras.previewPages != 2 || m.extras.previewHeight <= m.extras.previewWidth {
		t.Fatalf("first page: err=%q page=%d/%d size=%dx%d", m.extras.err, m.extras.previewPage, m.extras.previewPages, m.extras.previewWidth, m.extras.previewHeight)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd == nil {
		t.Fatal("right did not request next page")
	}
	m.Update(cmd())
	if m.extras.err != "" || m.extras.previewPage != 2 || m.extras.previewWidth <= m.extras.previewHeight {
		t.Fatal("second PDF page did not render")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if cmd == nil {
		t.Fatal("left did not request previous page")
	}
	m.Update(cmd())
	if m.extras.previewPage != 1 {
		t.Fatal("previous page did not render")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.extras != nil {
		t.Fatal("preview did not close")
	}
}

func TestPDFPreviewInvalidOrCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	png, _, _, _, err := makePDFPreview(ctx, testPDF(), 1)
	if err == nil || png != nil {
		t.Fatal("canceled PDF render succeeded")
	}
	if !isPDF([]byte("%PDF-1.7")) || isPDF([]byte("not PDF")) {
		t.Fatal("PDF detection failed")
	}
	t.Setenv("PATH", t.TempDir())
	_, _, _, _, err = makePDFPreview(context.Background(), testPDF(), 1)
	if err == nil || !strings.Contains(err.Error(), "Poppler") {
		t.Fatal("missing renderer was not reported")
	}
}
