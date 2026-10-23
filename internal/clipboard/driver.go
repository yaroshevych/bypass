package clipboard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"time"
)

const operationTimeout = time.Second
const maxClipboardBytes = 1 << 20

var (
	ErrUnavailable = errors.New("system clipboard is unavailable")
	ErrOperation   = errors.New("system clipboard operation failed")
)

type Driver interface {
	Write(context.Context, []byte) error
	Read(context.Context) ([]byte, error)
}

type systemDriver struct {
	writeCommand string
	writeArgs    []string
	readCommand  string
	readArgs     []string
}

type unavailableDriver struct{}

func NewSystem() Driver {
	switch runtime.GOOS {
	case "darwin":
		return systemDriver{writeCommand: "pbcopy", readCommand: "pbpaste"}
	case "linux":
		_, wlCopyErr := exec.LookPath("wl-copy")
		_, wlPasteErr := exec.LookPath("wl-paste")
		if wlCopyErr == nil && wlPasteErr == nil {
			return systemDriver{writeCommand: "wl-copy", readCommand: "wl-paste", readArgs: []string{"--no-newline"}}
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			return systemDriver{
				writeCommand: "xclip", writeArgs: []string{"-selection", "clipboard", "-in"},
				readCommand: "xclip", readArgs: []string{"-selection", "clipboard", "-out"},
			}
		}
	}
	return unavailableDriver{}
}

func (unavailableDriver) Write(context.Context, []byte) error  { return ErrUnavailable }
func (unavailableDriver) Read(context.Context) ([]byte, error) { return nil, ErrUnavailable }

func (d systemDriver) Write(ctx context.Context, value []byte) error {
	if d.writeCommand == "" {
		return ErrUnavailable
	}
	bounded, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(bounded, d.writeCommand, d.writeArgs...)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdin = bytes.NewReader(value)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ErrOperation
	}
	return nil
}

func (d systemDriver) Read(ctx context.Context) ([]byte, error) {
	if d.readCommand == "" {
		return nil, ErrUnavailable
	}
	bounded, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(bounded, d.readCommand, d.readArgs...)
	cmd.WaitDelay = 250 * time.Millisecond
	var output limitedBuffer
	output.limit = maxClipboardBytes
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || output.exceeded {
		clear(output.Bytes())
		return nil, ErrOperation
	}
	result := bytes.Clone(output.Bytes())
	clear(output.Bytes())
	return result, nil
}

func boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, operationTimeout)
}

type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.exceeded = true
		return 0, errors.New("clipboard output limit reached")
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.exceeded = true
		return remaining, errors.New("clipboard output limit reached")
	}
	return b.buf.Write(p)
}
