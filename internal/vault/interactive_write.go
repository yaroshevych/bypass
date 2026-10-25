package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var _ Writer = (*InteractiveCLI)(nil)

func (c *InteractiveCLI) Generate(ctx context.Context, length int) ([]byte, error) {
	return c.cli.Generate(ctx, length)
}

func (c *InteractiveCLI) CreateWithOptions(ctx context.Context, path string, creds Credentials, options CreateOptions) (*Database, error) {
	if creds.YubiKey != "" {
		return nil, ErrUnsupported
	}
	if err := validateNewCredentials(creds); err != nil {
		return nil, err
	}
	if options.DecryptionTimeMS != 0 && (options.DecryptionTimeMS < minKDFMillis || options.DecryptionTimeMS > maxKDFMillis) {
		return nil, ErrUnsupported
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	if _, err := os.Lstat(absolute); err == nil {
		return nil, ErrAlreadyExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrInvalidPath
	}
	if err := c.cli.runCreate(ctx, absolute, creds, options); err != nil {
		return nil, err
	}
	return c.Open(ctx, absolute, creds)
}

// Apply sends an edit to the already authenticated KeePassXC process. KeePassXC
// saves the original vault itself. Bypass copies the encrypted file beforehand
// for recovery, then refreshes its view after KeePassXC reports success.
func (c *InteractiveCLI) Apply(ctx context.Context, db *Database, _ Credentials, change Change, backupDir string) (*Database, error) {
	if db == nil || db.interactive == nil {
		return nil, ErrWrite
	}
	if err := validateChange(change); err != nil {
		return nil, err
	}
	s := db.interactive
	s.transactionMu.Lock()
	defer s.transactionMu.Unlock()
	if s.failed {
		return nil, ErrInteractiveSessionLost
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		return nil, err
	}
	selected, err := selectors.selectChange(change)
	if err != nil {
		return nil, err
	}
	args, expected, prompt, err := interactiveChangeArgs(change, selected)
	if err != nil {
		return nil, err
	}
	if _, err := interactiveLine(args...); err != nil {
		return nil, err
	}
	if err := backupInteractiveVault(db.Path, backupDir); err != nil {
		return nil, ErrBackup
	}
	output, err := s.commandInput(ctx, change.Password, prompt, args...)
	defer zero(output)
	if err != nil {
		s.failed = true
		return nil, errors.Join(ErrInteractiveSessionLost, err)
	}
	acknowledgement := output
	if prompt != "" {
		acknowledgement = bytes.TrimPrefix(acknowledgement, []byte(prompt+"\n"))
	}
	ack := string(acknowledgement)
	succeeded := ack == expected+"\n"
	if change.Kind == DeleteEntry || change.Kind == DeleteGroup {
		succeeded = succeeded || ack == strings.Replace(expected, "recycled", "deleted", 1)+"\n"
	}
	if !succeeded {
		// Native commands can update their in-memory model before a failed save.
		// Reauthenticate before using that model again; don't attempt a rollback.
		s.failed = true
		s.close()
		return nil, errors.Join(ErrInteractiveSessionLost, ErrWrite)
	}
	updated, err := s.load(ctx, db.Path)
	if err != nil {
		s.failed = true
		s.close()
		return nil, errors.Join(ErrInteractiveSessionLost, err)
	}
	if (change.Kind == DeleteEntry || change.Kind == DeleteGroup) && strings.HasPrefix(ack, "Successfully recycled ") {
		s.rememberRecycleBin(db, updated, change, selected)
	}
	s.markRecycled(updated)
	updated.interactive, updated.interactiveOwner = s, true
	return updated, nil
}

// backupInteractiveVault is a plain copy of the encrypted file. KeePassXC alone
// owns the save; this copy is not used for staging, validation, or publication.
func backupInteractiveVault(path, directory string) error {
	if directory == "" {
		return ErrBackup
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	backup, err := os.CreateTemp(directory, filepath.Base(path)+"-*.kdbx")
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		backup.Close()
		if !complete {
			os.Remove(backup.Name())
		}
	}()
	if _, err := io.Copy(backup, source); err != nil {
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func interactiveChangeArgs(change Change, selected selectedChange) (args []string, expected, prompt string, err error) {
	switch change.Kind {
	case AddEntry, EditEntry:
		name := "add"
		title := change.Values["Title"]
		if change.Kind == EditEntry {
			name = "edit"
			title = selected.entry.Get("Title")
			if value, ok := change.Values["Title"]; ok {
				title = value
			}
		}
		args = []string{name}
		for _, field := range []struct{ key, option string }{{"Title", "--title"}, {"UserName", "--username"}, {"URL", "--url"}, {"Notes", "--notes"}} {
			value, exists := change.Values[field.key]
			if !exists || (change.Kind == AddEntry && field.key == "Title") || (change.Kind == EditEntry && value == selected.current.Get(field.key)) {
				continue
			}
			if value == "" {
				if change.Kind == EditEntry {
					return nil, "", "", ErrUnsupported
				}
				continue
			}
			if field.key == "Notes" {
				if strings.Contains(value, `\n`) || strings.Contains(value, "\r") {
					return nil, "", "", ErrUnsupported
				}
				value = strings.ReplaceAll(value, "\n", `\n`)
			}
			args = append(args, field.option+"="+value)
		}
		if change.PasswordChanged {
			args = append(args, "--password-prompt")
			if change.Kind == AddEntry {
				prompt = "Enter password for new entry: "
			} else {
				prompt = "Enter new password for entry: "
			}
		}
		if change.Kind == EditEntry && len(args) == 1 {
			return nil, "", "", ErrUnsupported
		}
		args = append(args, "--", selected.entryPath)
		verb := "added"
		if change.Kind == EditEntry {
			verb = "edited"
		}
		expected = fmt.Sprintf("Successfully %s entry %s.", verb, title)
	case DeleteEntry:
		args = []string{"rm", "--", selected.entryPath}
		expected = "Successfully recycled entry " + selected.entry.Get("Title") + "."
	case DeleteGroup:
		args = []string{"rmdir", "--", selected.groupPath}
		expected = "Successfully recycled group " + selected.groupPath + "."
	case CreateGroup:
		args = []string{"mkdir", "--", selected.groupPath}
		expected = "Successfully added group " + change.NewName + "."
	case MoveEntry:
		args = []string{"mv", "--", selected.entryPath, selected.groupPath}
		expected = fmt.Sprintf("Successfully moved entry %s to group %s.", selected.entry.Get("Title"), selected.groupPath)
	default:
		return nil, "", "", ErrUnsupported
	}
	return args, expected, prompt, nil
}
