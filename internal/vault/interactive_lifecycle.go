package vault

import (
	"context"
	"errors"
	"strings"
)

func (c *InteractiveCLI) InfoDatabase(ctx context.Context, db *Database) (DatabaseInfo, error) {
	if db == nil || db.interactive == nil {
		return DatabaseInfo{}, ErrUnlock
	}
	s := db.interactive
	s.transactionMu.Lock()
	defer s.transactionMu.Unlock()
	if s.failed {
		return DatabaseInfo{}, ErrInteractiveSessionLost
	}
	data, err := s.command(ctx, "db-info", "-q")
	defer zero(data)
	if err != nil {
		return DatabaseInfo{}, err
	}
	return parseDatabaseInfo(data)
}

func (c *InteractiveCLI) ChangeCredentials(ctx context.Context, db *Database, _ Credentials, newCreds Credentials, backupDir string) (*Database, error) {
	if db == nil || db.interactive == nil {
		return nil, ErrUnlock
	}
	if err := validateNewCredentials(newCreds); err != nil {
		return nil, err
	}
	// The current UI changes the password only. Native db-edit preserves other
	// existing factors; do not infer factor removal from an empty credentials copy.
	if newCreds.NoPassword || newCreds.KeyFile != "" || newCreds.YubiKey != "" {
		return nil, ErrUnsupported
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
	if err := backupInteractiveVault(db.Path, backupDir); err != nil {
		return nil, ErrBackup
	}
	prompts := []string{"Enter password to encrypt database (optional): ", "Repeat password: "}
	output, err := s.commandPrompts(ctx, [][]byte{newCreds.Password, newCreds.Password}, prompts, "db-edit", "--set-password")
	defer zero(output)
	expected := prompts[0] + "\n" + prompts[1] + "\nSuccessfully edited the database.\n"
	if err != nil || string(output) != expected {
		s.failed = true
		s.close()
		return nil, errors.Join(ErrInteractiveSessionLost, ErrWrite, err)
	}
	updated, err := s.load(ctx, db.Path)
	if err != nil {
		s.failed = true
		s.close()
		return nil, errors.Join(ErrInteractiveSessionLost, err)
	}
	updated.interactive, updated.interactiveOwner = s, true
	return updated, nil
}

// Native rm tells us when it recycled an item. Retain the resulting bin location
// for navigation during this session; native CLI does not expose its group UUID.
func (s *interactiveSession) rememberRecycleBin(before, after *Database, change Change, selected selectedChange) {
	if change.Kind == DeleteEntry {
		for _, entry := range after.Entries {
			if entry.UUID == change.EntryUUID {
				s.recycleBinID = entry.GroupUUID
				return
			}
		}
	}
	if change.Kind == DeleteGroup {
		previous := map[string]bool{}
		for _, group := range before.Groups {
			previous[group.UUID] = true
		}
		candidate := ""
		for _, group := range after.Groups {
			if group.Name == selected.group.Name && !previous[group.UUID] {
				if candidate != "" {
					return
				}
				candidate = group.ParentUUID
			}
		}
		if candidate != "" {
			s.recycleBinID = candidate
		}
	}
}

func (s *interactiveSession) markRecycled(db *Database) {
	// The interactive CLI does not report the recycle-bin UUID. Recognize its
	// standard top-level name on open; native rm results identify it after edits.
	if s.recycleBinID == "" && db.RecycleBinEnabled {
		for _, group := range db.Groups {
			if group.ParentUUID == db.RootUUID && strings.EqualFold(group.Name, "Recycle Bin") {
				s.recycleBinID = group.UUID
				break
			}
		}
	}
	if s.recycleBinID == "" {
		return
	}
	db.RecycleBinUUID = s.recycleBinID
	groups := map[string]Group{}
	for _, group := range db.Groups {
		groups[group.UUID] = group
	}
	recycled := func(id string) bool {
		for id != "" {
			if id == s.recycleBinID {
				return true
			}
			group, ok := groups[id]
			if !ok {
				break
			}
			id = group.ParentUUID
		}
		return false
	}
	for index := range db.Groups {
		db.Groups[index].Recycled = recycled(db.Groups[index].UUID)
	}
	for index := range db.Entries {
		db.Entries[index].Recycled = recycled(db.Entries[index].GroupUUID)
	}
}
