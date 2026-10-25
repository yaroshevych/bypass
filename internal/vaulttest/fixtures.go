package vaulttest

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"bypass/internal/vault"
)

// Fixture writes use the application backend. Reads use a separate native XML
// export so that tests check saved data independently of the session cache.
func (c *CLI) Create(ctx context.Context, path string, creds vault.Credentials) (*vault.Database, error) {
	return c.CreateWithOptions(ctx, path, creds, vault.CreateOptions{})
}

func (c *CLI) CreateWithOptions(ctx context.Context, path string, creds vault.Credentials, options vault.CreateOptions) (*vault.Database, error) {
	db, err := vault.NewInteractiveCLI(c.binary).CreateWithOptions(ctx, path, creds, options)
	if err != nil {
		return nil, err
	}
	db.Clear()
	return c.Open(ctx, path, creds)
}

func (c *CLI) Apply(ctx context.Context, snapshot *vault.Database, creds vault.Credentials, change vault.Change, backup string) (*vault.Database, error) {
	if change.Kind == vault.AddAttachment {
		// Seed unsupported UI features through the native CLI for read-only tests.
		var entryPath string
		for _, entry := range snapshot.Entries {
			if entry.UUID == change.EntryUUID {
				_, group, _ := strings.Cut(entry.GroupPath, "/")
				entryPath = "/" + strings.Trim(group+"/"+entry.Get("Title"), "/")
			}
		}
		if entryPath == "" {
			return nil, vault.ErrUnsupported
		}
		directory, err := os.MkdirTemp("", "bypass-readback-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(directory)
		path := filepath.Join(directory, "attachment")
		if err := os.WriteFile(path, change.AttachmentData, 0600); err != nil {
			return nil, err
		}
		args := []string{"attachment-import", "-q"}
		if creds.NoPassword {
			args = append(args, "--no-password")
		}
		if creds.KeyFile != "" {
			args = append(args, "-k", creds.KeyFile)
		}
		args = append(args, "--", snapshot.Path, entryPath, change.AttachmentName, path)
		cmd := exec.CommandContext(ctx, c.binary, args...)
		cmd.Stdin = strings.NewReader(string(creds.Password) + "\n")
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Run(); err != nil {
			return nil, vault.ErrWrite
		}
		return c.Open(ctx, snapshot.Path, creds)
	}
	backend := vault.NewInteractiveCLI(c.binary)
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

func (c *CLI) Info(ctx context.Context, path string, creds vault.Credentials) (vault.DatabaseInfo, error) {
	backend := vault.NewInteractiveCLI(c.binary)
	db, err := backend.Open(ctx, path, creds)
	if err != nil {
		return vault.DatabaseInfo{}, err
	}
	defer db.Clear()
	return backend.InfoDatabase(ctx, db)
}

// ExportAttachment is used only by older terminal readback checks.
func (c *CLI) ExportAttachment(ctx context.Context, db *vault.Database, creds vault.Credentials, id, name string) ([]byte, error) {
	var path string
	for _, entry := range db.Entries {
		if entry.UUID == id {
			_, group, _ := strings.Cut(entry.GroupPath, "/")
			path = "/" + strings.Trim(group+"/"+entry.Get("Title"), "/")
		}
	}
	if path == "" {
		return nil, vault.ErrUnsupported
	}
	directory, err := os.MkdirTemp("", "bypass-readback-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	destination := filepath.Join(directory, "attachment")
	args := []string{"attachment-export", "-q"}
	if creds.NoPassword {
		args = append(args, "--no-password")
	}
	if creds.KeyFile != "" {
		args = append(args, "-k", creds.KeyFile)
	}
	args = append(args, db.Path, path, name, destination)
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.Stdin = strings.NewReader(string(creds.Password) + "\n")
	cmd.Stderr = io.Discard
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, vault.ErrWrite
	}
	return os.ReadFile(destination)
}
