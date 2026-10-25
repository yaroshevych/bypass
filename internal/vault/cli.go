package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrInteractiveSessionLost = errors.New("interactive session lost; lock and unlock to retry")
	ErrUnavailable            = errors.New("vault CLI unavailable")
	ErrInvalidPath            = errors.New("invalid vault path")
	ErrUnlock                 = errors.New("unable to unlock vault")
	ErrCanceled               = errors.New("vault operation canceled")
	ErrMalformed              = errors.New("malformed vault export")
	ErrTooLarge               = errors.New("vault export exceeds size limit")
	ErrReadOnly               = errors.New("vault is read-only")
	ErrAmbiguous              = errors.New("vault selector is ambiguous")
	ErrUnsupported            = errors.New("vault change is unsupported")
	ErrExternalChanged        = errors.New("vault changed outside this application")
	ErrWrite                  = errors.New("vault write failed")
	ErrBackup                 = errors.New("vault backup failed")
)

const (
	maxExportBytes = 32 << 20
	defaultTimeout = 60 * time.Second
)

type commandCLI struct{ binary string }

func newCommandCLI(binary string) *commandCLI {
	if strings.TrimSpace(binary) == "" {
		binary = "keepassxc-cli"
	}
	return &commandCLI{binary: binary}
}

func (c *commandCLI) Probe(ctx context.Context) (string, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, "--version")
	cmd.WaitDelay = 250 * time.Millisecond
	var out limitedBuffer
	out.limit = 4096
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		var e *exec.Error
		if errors.As(err, &e) {
			return "", ErrUnavailable
		}
		return "", ErrUnavailable
	}
	v := strings.TrimSpace(out.String())
	if v == "" {
		return "", ErrUnavailable
	}
	return v, nil
}

func validateCredentials(creds Credentials) error {
	if creds.NoPassword && len(creds.Password) > 0 {
		return ErrUnlock
	}
	for _, b := range creds.Password {
		if b == '\n' || b == '\r' || b == 0 {
			return ErrUnlock
		}
	}
	for _, s := range []string{creds.KeyFile, creds.YubiKey} {
		if strings.ContainsAny(s, "\r\n\x00") {
			return ErrUnlock
		}
	}
	return nil
}

func boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remain := b.limit - b.buf.Len()
	if remain <= 0 {
		b.exceeded = true
		return 0, errLimit
	}
	if len(p) > remain {
		_, _ = b.buf.Write(p[:remain])
		b.exceeded = true
		return remain, errLimit
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }

var errLimit = errors.New("output limit reached")
