package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const maxResolvedFieldBytes = 1 << 20

// ResolveField asks KeePassXC to resolve placeholders for one entry field.
// The decrypted value is returned only in memory and is never written to disk.
func (c *CLI) ResolveField(ctx context.Context, db *Database, creds Credentials, entryUUID, field string) ([]byte, error) {
	if db == nil || db.Path == "" || entryUUID == "" || field == "" || strings.ContainsAny(field, "\r\n\x00") {
		return nil, ErrUnsupported
	}
	if err := validateCredentials(creds); err != nil {
		return nil, err
	}
	unlockPath, unlock, err := lockCanonicalPath(db.Path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	defer unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		return nil, ErrWrite
	}
	entryPath, entry, err := selectors.uniqueEntry(entryUUID)
	if err != nil {
		return nil, err
	}
	fieldCount := 0
	for _, candidate := range entry.Fields {
		if candidate.Name == field {
			fieldCount++
		}
	}
	if fieldCount == 0 {
		return nil, ErrUnsupported
	}
	if fieldCount != 1 {
		return nil, ErrAmbiguous
	}
	before, err := Revision(unlockPath)
	if err != nil {
		return nil, err
	}
	if before != db.Revision {
		return nil, ErrExternalChanged
	}

	ctx, cancel := boundedContext(ctx)
	defer cancel()
	args := []string{"show", "-q", "--attributes=" + field}
	args = appendAuthArgs(args, creds)
	args = append(args, unlockPath, entryPath)
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	var stdout limitedBuffer
	stdout.limit = maxResolvedFieldBytes
	defer func() { zero(stdout.Bytes()) }()
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if !creds.NoPassword {
		input := append(append([]byte(nil), creds.Password...), '\n')
		cmd.Stdin = bytes.NewReader(input)
		defer zero(input)
	}
	runErr := cmd.Run()
	after, revisionErr := Revision(unlockPath)
	if revisionErr != nil || before != after {
		return nil, ErrExternalChanged
	}
	if stdout.exceeded {
		return nil, ErrTooLarge
	}
	if runErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		var e *exec.Error
		if errors.As(runErr, &e) {
			return nil, ErrUnavailable
		}
		return nil, ErrUnlock
	}
	resolved := stdout.Bytes()
	if runtime.GOOS == "windows" {
		resolved = bytes.TrimSuffix(resolved, []byte("\r\n"))
	} else {
		resolved = bytes.TrimSuffix(resolved, []byte("\n"))
	}
	return append([]byte(nil), resolved...), nil
}
