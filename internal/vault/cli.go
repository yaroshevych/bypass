package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrUnavailable = errors.New("vault CLI unavailable")
	ErrInvalidPath = errors.New("invalid vault path")
	ErrUnlock      = errors.New("unable to unlock vault")
	ErrCanceled    = errors.New("vault operation canceled")
	ErrMalformed   = errors.New("malformed vault export")
	ErrTooLarge    = errors.New("vault export exceeds size limit")
)

const (
	maxExportBytes = 32 << 20
	defaultTimeout = 60 * time.Second
)

type CLI struct{ binary string }

func NewCLI(binary string) *CLI {
	if strings.TrimSpace(binary) == "" {
		binary = "keepassxc-cli"
	}
	return &CLI{binary: binary}
}

func (c *CLI) Probe(ctx context.Context) (string, error) {
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

func (c *CLI) Open(ctx context.Context, path string, creds Credentials) (*Database, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	if creds.NoPassword && len(creds.Password) > 0 {
		return nil, ErrUnlock
	}
	for _, b := range creds.Password {
		if b == '\n' || b == '\r' || b == 0 {
			return nil, ErrUnlock
		}
	}
	for _, s := range []string{creds.KeyFile, creds.YubiKey} {
		if strings.ContainsAny(s, "\r\n\x00") {
			return nil, ErrUnlock
		}
	}

	ctx, cancel := boundedContext(ctx)
	defer cancel()
	args := []string{"export", "-q", "-f", "xml"}
	if creds.KeyFile != "" {
		args = append(args, "-k", creds.KeyFile)
	}
	if creds.NoPassword {
		args = append(args, "--no-password")
	}
	if creds.YubiKey != "" {
		args = append(args, "-y", creds.YubiKey)
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	var stdout limitedBuffer
	stdout.limit = maxExportBytes
	defer func() { zero(stdout.Bytes()) }()
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard // CLI errors can echo user-controlled paths or credentials.
	if !creds.NoPassword {
		input := append(append([]byte(nil), creds.Password...), '\n')
		cmd.Stdin = bytes.NewReader(input)
		defer zero(input)
	}
	if err := cmd.Run(); err != nil {
		if stdout.exceeded {
			return nil, ErrTooLarge
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		var e *exec.Error
		if errors.As(err, &e) {
			return nil, ErrUnavailable
		}
		// Keep unlock failures opaque: CLI stderr includes uncontrolled text.
		return nil, ErrUnlock
	}
	if stdout.exceeded {
		return nil, ErrTooLarge
	}
	db, err := parseXML(stdout.Bytes(), path)
	if err != nil {
		return nil, err
	}
	return db, nil
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
