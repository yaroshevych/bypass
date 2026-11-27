package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os/exec"
	"strconv"
	"time"
)

func isPDF(data []byte) bool { return bytes.HasPrefix(data, []byte("%PDF-")) }

// PDF data goes to Poppler over stdin and the rendered page returns over stdout.
// No plaintext attachment or page image is written to disk.
func makePDFPreview(ctx context.Context, data []byte, page int) ([]byte, int, int, int, error) {
	if len(data) > maxPreviewBytes {
		return nil, 0, 0, 0, errors.New("PDF is too large to preview.")
	}
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		return nil, 0, 0, 0, errors.New("PDF preview requires Poppler (pdfinfo and pdftoppm).")
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, 0, 0, 0, errors.New("PDF preview requires Poppler (pdfinfo and pdftoppm).")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := pdfCommand(ctx, data, 1<<20, "pdfinfo", "-")
	if err != nil {
		return nil, 0, 0, 0, errors.New("Could not read PDF; it may be invalid or password protected.")
	}
	defer clear(info)
	pages := 0
	for _, line := range bytes.Split(info, []byte("\n")) {
		key, value, ok := bytes.Cut(line, []byte(":"))
		if ok && bytes.Equal(key, []byte("Pages")) {
			pages, _ = strconv.Atoi(string(bytes.TrimSpace(value)))
			break
		}
	}
	if pages < 1 || page < 1 || page > pages {
		return nil, 0, 0, 0, errors.New("Could not read PDF pages.")
	}
	n := strconv.Itoa(page)
	pngData, err := pdfCommand(ctx, data, maxPreviewBytes, "pdftoppm", "-f", n, "-l", n, "-scale-to", "1600", "-png", "-singlefile", "-")
	if err != nil {
		clear(pngData)
		return nil, 0, 0, 0, errors.New("Could not render PDF page.")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(pngData))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxPreviewPixels/cfg.Height {
		clear(pngData)
		return nil, 0, 0, 0, errors.New("Could not render PDF page.")
	}
	return pngData, cfg.Width, cfg.Height, pages, nil
}

type boundedPreviewBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedPreviewBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("preview output too large")
	}
	return b.buffer.Write(p)
}
func pdfCommand(ctx context.Context, data []byte, limit int, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(data)
	var output boundedPreviewBuffer
	output.limit = limit
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		clear(output.buffer.Bytes())
		return nil, fmt.Errorf("PDF renderer: %w", err)
	}
	return output.buffer.Bytes(), nil
}
