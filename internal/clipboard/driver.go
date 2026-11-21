package clipboard

import (
	wipebuf "bypass/internal/secret"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"time"
)

const operationTimeout = time.Second
const maxClipboardBytes = 1 << 20

var (
	ErrUnavailable          = errors.New("system clipboard is unavailable")
	ErrOperation            = errors.New("system clipboard operation failed")
	ErrSensitiveUnavailable = errors.New("secure clipboard copying is unavailable; use a clipboard driver that supports sensitive writes (wl-copy --sensitive on Wayland)")
)

type Driver interface {
	Write(context.Context, []byte) error
	Read(context.Context) ([]byte, error)
}

type systemDriver struct {
	sensitiveSupported bool
	sensitiveRequired  bool
	writeCommand       string
	writeArgs          []string
	readCommand        string
	readArgs           []string
}

type unavailableDriver struct{}

func NewSystem() Driver {
	return newSystemForOS(runtime.GOOS)
}

func newSystemForOS(goos string) Driver {
	switch goos {
	case "darwin":
		return systemDriver{writeCommand: "pbcopy", readCommand: "pbpaste"}
	case "linux":
		wlCopy, wlCopyErr := exec.LookPath("wl-copy")
		_, wlPasteErr := exec.LookPath("wl-paste")
		if wlCopyErr == nil && wlPasteErr == nil {
			return systemDriver{sensitiveSupported: supportsWlCopySensitive(wlCopy), sensitiveRequired: true, writeCommand: wlCopy, readCommand: "wl-paste", readArgs: []string{"--no-newline"}}
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

// SupportsSensitive reports whether the driver can mark a write as sensitive.
func SupportsSensitive(driver Driver) bool {
	d, ok := driver.(interface{ SupportsSensitive() bool })
	return ok && d.SupportsSensitive()
}

// RequiresSensitive reports whether secret writes must be marked sensitive.
func RequiresSensitive(driver Driver) bool {
	d, ok := driver.(interface{ RequiresSensitive() bool })
	return ok && d.RequiresSensitive()
}

func (d systemDriver) SupportsSensitive() bool { return d.sensitiveSupported }
func (d systemDriver) RequiresSensitive() bool { return d.sensitiveRequired }

func supportsWlCopySensitive(command string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "--help")
	cmd.WaitDelay = 250 * time.Millisecond
	const helpLimit = 64 << 10
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = helpLimit, helpLimit
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stdout.exceeded || stderr.exceeded {
		stdout.wipe()
		stderr.wipe()
		return false
	}
	supported := bytes.Contains(stdout.Bytes(), []byte("--sensitive")) || bytes.Contains(stderr.Bytes(), []byte("--sensitive"))
	stdout.wipe()
	stderr.wipe()
	return supported
}

func (unavailableDriver) Write(context.Context, []byte) error  { return ErrUnavailable }
func (unavailableDriver) Read(context.Context) ([]byte, error) { return nil, ErrUnavailable }

func (d systemDriver) Write(ctx context.Context, value []byte) error {
	args := d.writeArgs
	if d.sensitiveSupported {
		args = append(append([]string(nil), args...), "--sensitive")
	}
	return d.write(ctx, args, value)
}

func (d systemDriver) WriteSensitive(ctx context.Context, value []byte) error {
	if d.sensitiveRequired && !d.sensitiveSupported {
		return ErrSensitiveUnavailable
	}
	return d.Write(ctx, value)
}

func (d systemDriver) write(ctx context.Context, args []string, value []byte) error {
	if d.writeCommand == "" {
		return ErrUnavailable
	}
	bounded, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(bounded, d.writeCommand, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdin = bytes.NewReader(value)
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
	if err := cmd.Run(); err != nil || output.exceeded {
		output.wipe()
		return nil, ErrOperation
	}
	result := bytes.Clone(output.Bytes())
	output.wipe()
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

func (b *limitedBuffer) wipe() {
	wipebuf.Wipe(b.buf.Bytes())
	b.buf.Reset()
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
