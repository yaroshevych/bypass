package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
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
	if err := os.Chmod(absolute, 0600); err != nil {
		return nil, fmt.Errorf("%w: could not restrict new vault permissions", ErrWrite)
	}
	return c.Open(ctx, absolute, creds)
}

// Apply sends an edit to the already authenticated KeePassXC process. KeePassXC
// saves the original vault itself. Bypass copies the encrypted file beforehand
// for recovery, then refreshes its view after KeePassXC reports success.
func (c *InteractiveCLI) Apply(ctx context.Context, db *Database, _ Credentials, change Change, backupDir string) (*Database, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Apply runs later with a UI worker context, separate from Open's context.
	// Reattach the backend logger so mutation diagnostics reach --log-file.
	if c.logger != nil && ctx.Value(diagnosticKey{}) == nil {
		ctx = context.WithValue(ctx, diagnosticKey{}, c.logger.With("operation", openAttempt.Add(1)))
	}
	if db == nil || db.interactive == nil {
		return nil, ErrWrite
	}
	diagnostic(ctx, "mutation_started", "kind", change.Kind, "vault_groups", len(db.Groups), "vault_entries", len(db.Entries))
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
	selection := change
	if change.Kind == PrepareRecycleBin {
		selection.Kind = DeleteEntry
		if change.GroupUUID != "" {
			selection.Kind = DeleteGroup
		}
	}
	selected, err := selectors.selectChangeWithMoveCollision(selection, change.Kind == MoveEntry)
	if err != nil {
		return nil, err
	}
	if change.Kind == PrepareRecycleBin {
		if !db.RecycleBinEnabled {
			return nil, ErrUnsupported
		}
		return s.prepareRecycleBin(ctx, db, backupDir)
	}
	if db.RecycleBinEnabled && !db.RecycleBinVerified && (change.Kind == DeleteEntry || change.Kind == DeleteGroup && selected.group.Recycled) {
		return nil, ErrUnsupported
	}
	if change.Kind == PrepareDeleteGroup {
		if !db.RecycleBinEnabled || selected.group.Recycled {
			return nil, ErrUnsupported
		}
		return s.prepareGroupDeletion(ctx, db, selected, backupDir)
	}
	if change.Kind == DeleteGroup && db.RecycleBinEnabled && !selected.group.Recycled {
		return s.deletePreparedGroup(ctx, db, selected, backupDir)
	}
	s.groupDeletePlan = nil
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
	finalMoveTitle := ""
	if change.Kind == MoveEntry {
		title := selected.entry.Get("Title")
		destination := selectors.rawGroupPaths[selected.group.UUID]
		source := selectors.rawGroupPaths[selected.entry.GroupUUID]
		if len(selectors.rawEntryPathIDs[destination+title]) != 0 {
			newTitle := ""
			for i := 1; i <= 10000; i++ {
				candidate := fmt.Sprintf("%s %d", title, i)
				if len(selectors.rawEntryPathIDs[destination+candidate]) == 0 {
					newTitle = candidate
					break
				}
			}
			if newTitle == "" {
				return nil, ErrTooLarge
			}
			if len(selectors.rawEntryPathIDs[source+newTitle]) != 0 {
				finalMoveTitle = newTitle
				newTitle = ""
				for i := 1; i <= 10000; i++ {
					candidate := fmt.Sprintf("%s %d", title, i)
					if len(selectors.rawEntryPathIDs[source+candidate]) == 0 && len(selectors.rawEntryPathIDs[destination+candidate]) == 0 {
						newTitle = candidate
						break
					}
				}
				if newTitle == "" {
					return nil, ErrTooLarge
				}
			}
			out, renameErr := s.command(ctx, "edit", "--title", newTitle, "--", selected.entryPath)
			ok := bytes.Equal(out, []byte("Successfully edited entry "+newTitle+".\n"))
			zero(out)
			if renameErr != nil || !ok {
				s.failed = true
				s.close()
				return nil, errors.Join(ErrInteractiveSessionLost, ErrPartialWrite, ErrWrite, renameErr)
			}
			selected.entry = cloneEntry(selected.entry, true)
			for i := range selected.entry.Fields {
				if selected.entry.Fields[i].Name == "Title" {
					selected.entry.Fields[i].Value = newTitle
				}
			}
			selected.entryPath = source + newTitle
			args, expected, prompt, err = interactiveChangeArgs(change, selected)
			if err != nil {
				s.failed = true
				s.close()
				return nil, errors.Join(ErrInteractiveSessionLost, ErrPartialWrite, err)
			}
		}
	}
	output, err := s.commandInput(ctx, change.Password, prompt, args...)
	defer zero(output)
	if err != nil {
		diagnostic(ctx, "mutation_command_failed", "kind", change.Kind, "error", diagnosticError(err))
		s.failed = true
		return nil, errors.Join(ErrInteractiveSessionLost, ErrPartialWrite, err)
	}
	acknowledgement := output
	if prompt != "" {
		acknowledgement = bytes.TrimPrefix(acknowledgement, []byte(prompt+"\n"))
	}
	succeeded := bytes.Equal(acknowledgement, []byte(expected+"\n"))
	if change.Kind == DeleteEntry || change.Kind == DeleteGroup {
		succeeded = succeeded || bytes.Equal(acknowledgement, []byte(strings.Replace(expected, "recycled", "deleted", 1)+"\n"))
	}
	if change.Kind == DeleteGroup && selected.group.Recycled {
		succeeded = bytes.Equal(acknowledgement, []byte(strings.Replace(expected, "recycled", "deleted", 1)+"\n"))
	}
	if change.Kind == DeleteGroup {
		// Diagnostic only: report response shape and classification, never the
		// group path or raw CLI output (which may contain user data).
		diagnostic(ctx, "group_delete_acknowledgement", "matched", succeeded, "response_bytes", len(acknowledgement), "recycled", bytes.HasPrefix(acknowledgement, []byte("Successfully recycled ")), "deleted", bytes.HasPrefix(acknowledgement, []byte("Successfully deleted ")))
	}
	if !succeeded {
		// Native commands can update their in-memory model before a failed save.
		// Reauthenticate before using that model again; don't attempt a rollback.
		s.failed = true
		s.close()
		return nil, errors.Join(ErrInteractiveSessionLost, ErrPartialWrite, ErrWrite)
	}
	if finalMoveTitle != "" {
		path := selectors.rawGroupPaths[selected.group.UUID] + selected.entry.Get("Title")
		out, renameErr := s.command(ctx, "edit", "--title", finalMoveTitle, "--", path)
		ok := bytes.Equal(out, []byte("Successfully edited entry "+finalMoveTitle+".\n"))
		zero(out)
		if renameErr != nil || !ok {
			s.failed = true
			s.close()
			return nil, errors.Join(ErrInteractiveSessionLost, ErrPartialWrite, ErrWrite, renameErr)
		}
	}
	updated, err := s.load(ctx, db.Path)
	if err != nil {
		diagnostic(ctx, "mutation_reload_failed", "kind", change.Kind, "error", diagnosticError(err))
		s.failed = true
		s.close()
		return nil, errors.Join(ErrInteractiveSessionLost, ErrPartialWrite, err)
	}
	if change.Kind == DeleteGroup {
		diagnostic(ctx, "group_delete_reloaded", "groups", len(updated.Groups), "entries", len(updated.Entries))
	}
	if (change.Kind == DeleteEntry || change.Kind == DeleteGroup) && bytes.HasPrefix(acknowledgement, []byte("Successfully recycled ")) {
		s.rememberRecycleBin(db, updated, change, selected)
	}
	s.markRecycled(updated)
	updated.interactive, updated.interactiveOwner = s, true
	return updated, nil
}

var backupMu sync.Mutex
var backupName = regexp.MustCompile(`^.+-[0-9]+\.kdbx$`)

// backupInteractiveVault is a plain copy of the encrypted file. KeePassXC alone
// owns the save; this copy is not used for staging, validation, or publication.
func backupInteractiveVault(path, directory string) error {
	// Different unlocked vaults can edit concurrently and share this directory.
	backupMu.Lock()
	defer backupMu.Unlock()
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
	return pruneBackups(directory, backup.Name())
}

// Keep five completed Bypass copies across all vaults in this directory.
// Unrelated files, symlinks, and directories are left alone.
func pruneBackups(directory, newest string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var backups []os.FileInfo
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !backupName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		backups = append(backups, info)
	}
	newest = filepath.Base(newest)
	sort.Slice(backups, func(i, j int) bool {
		left, right := backups[i], backups[j]
		if left.Name() == newest || right.Name() == newest {
			return left.Name() == newest
		}
		if left.ModTime().Equal(right.ModTime()) {
			return left.Name() > right.Name()
		}
		return left.ModTime().After(right.ModTime())
	})
	for _, backup := range backups[min(5, len(backups)):] {
		if err := os.Remove(filepath.Join(directory, backup.Name())); err != nil {
			return err
		}
	}
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
