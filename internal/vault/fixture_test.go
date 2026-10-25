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

// CLI is a test fixture driver, not an application backend.
type CLI = commandCLI

func NewCLI(binary string) *CLI { return newCommandCLI(binary) }

func (c *CLI) Open(ctx context.Context, path string, creds Credentials) (*Database, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	path = canonical
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	initialRevision, err := Revision(path)
	if err != nil {
		return nil, err
	}
	if err := validateCredentials(creds); err != nil {
		return nil, err
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
	runErr := cmd.Run()
	finalRevision, revisionErr := Revision(path)
	if revisionErr != nil {
		return nil, ErrExternalChanged
	}
	if initialRevision != finalRevision {
		return nil, ErrExternalChanged
	}
	if err := runErr; err != nil {
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
	db.Revision = finalRevision
	return db, nil
}

// Fixture writes use the application backend. Reads use a separate native XML
// export so that tests check saved data independently of the session cache.
func (c *CLI) Create(ctx context.Context, path string, creds Credentials) (*Database, error) {
	return c.CreateWithOptions(ctx, path, creds, CreateOptions{})
}

func (c *CLI) CreateWithOptions(ctx context.Context, path string, creds Credentials, options CreateOptions) (*Database, error) {
	db, err := NewInteractiveCLI(c.binary).CreateWithOptions(ctx, path, creds, options)
	if err != nil {
		return nil, err
	}
	db.Clear()
	return c.Open(ctx, path, creds)
}

func (c *CLI) Apply(ctx context.Context, snapshot *Database, creds Credentials, change Change, backup string) (*Database, error) {
	backend := NewInteractiveCLI(c.binary)
	db, err := backend.Open(ctx, snapshot.Path, creds)
	if err != nil {
		return nil, err
	}
	defer db.Clear()
	// CLI group IDs belong to the current session; XML uses the stored UUIDs.
	groupID := func(id string) string {
		for _, old := range snapshot.Groups {
			if old.UUID != id {
				continue
			}
			_, path, _ := strings.Cut(old.Path, "/")
			for _, current := range db.Groups {
				_, candidate, _ := strings.Cut(current.Path, "/")
				if candidate == path {
					return current.UUID
				}
			}
		}
		return id
	}
	change.GroupUUID = groupID(change.GroupUUID)
	updated, err := backend.Apply(ctx, db, creds, change, backup)
	if err != nil {
		return nil, err
	}
	updated.Clear()
	return c.Open(ctx, snapshot.Path, creds)
}

func (c *CLI) Info(ctx context.Context, path string, creds Credentials) (DatabaseInfo, error) {
	backend := NewInteractiveCLI(c.binary)
	db, err := backend.Open(ctx, path, creds)
	if err != nil {
		return DatabaseInfo{}, err
	}
	defer db.Clear()
	return backend.InfoDatabase(ctx, db)
}
