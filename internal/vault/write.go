package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxGeneratedPasswordLength = 1024

func (c *CLI) Generate(ctx context.Context, length int) ([]byte, error) {
	if length < 1 || length > maxGeneratedPasswordLength {
		return nil, ErrUnsupported
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, "generate", "--length", strconv.Itoa(length))
	cmd.WaitDelay = 250 * time.Millisecond
	var out limitedBuffer
	out.limit = maxGeneratedPasswordLength + 2
	defer func() { zero(out.Bytes()) }()
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if out.exceeded {
			return nil, ErrTooLarge
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		return nil, ErrWrite
	}
	if out.exceeded {
		return nil, ErrTooLarge
	}
	result := bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	result = bytes.TrimSuffix(result, []byte("\r"))
	if len(result) != length {
		return nil, ErrWrite
	}
	return append([]byte(nil), result...), nil
}

func (c *CLI) Apply(ctx context.Context, db *Database, creds Credentials, change Change, backupDir string) (*Database, error) {
	if db == nil || db.Path == "" || backupDir == "" {
		return nil, ErrWrite
	}
	if err := validateChange(change); err != nil {
		return nil, err
	}
	if err := validateCredentials(creds); err != nil {
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	path, unlock, err := lockCanonicalPath(db.Path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	defer unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	if info.Mode().Perm()&0222 == 0 {
		return nil, ErrReadOnly
	}
	initialRevision, err := Revision(path)
	if err != nil {
		return nil, err
	}
	if initialRevision != db.Revision {
		return nil, ErrExternalChanged
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		return nil, err
	}
	selection, err := selectors.selectChange(change)
	if err != nil {
		return nil, err
	}

	ctx, cancel := boundedContext(ctx)
	defer cancel()
	stage, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".keepassui-*.kdbx")
	if err != nil {
		return nil, ErrWrite
	}
	stagePath := stage.Name()
	stageClosed := false
	defer func() {
		if !stageClosed {
			_ = stage.Close()
		}
		_ = os.Remove(stagePath)
	}()
	if err = copyEncrypted(path, stage, 0600); err != nil {
		return nil, ErrWrite
	}
	if err = stage.Close(); err != nil {
		stageClosed = true
		return nil, ErrWrite
	}
	stageClosed = true
	stageRevision, err := Revision(stagePath)
	if err != nil || stageRevision != initialRevision {
		return nil, ErrExternalChanged
	}

	if err = c.runChange(ctx, stagePath, creds, change, selection); err != nil {
		return nil, err
	}
	stageCreds := creds.Clone()
	updated, err := c.Open(ctx, stagePath, stageCreds)
	stageCreds.Clear()
	if err != nil {
		return nil, ErrWrite
	}
	returnUpdated := false
	defer func() {
		if !returnUpdated {
			updated.Clear()
		}
	}()
	if err = verifyChange(db, updated, change, selection); err != nil {
		return nil, err
	}

	if err = createBackup(backupDir, path, initialRevision); err != nil {
		return nil, ErrBackup
	}
	currentRevision, err := Revision(path)
	if err != nil || currentRevision != initialRevision {
		return nil, ErrExternalChanged
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	newRevision, err := Revision(stagePath)
	if err != nil {
		return nil, ErrWrite
	}
	if err = os.Chmod(stagePath, info.Mode().Perm()); err != nil {
		return nil, ErrWrite
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err = os.Rename(stagePath, path); err != nil {
		return nil, ErrWrite
	}
	stagePath = ""
	stageClosed = true
	updated.Path = path
	updated.Revision = newRevision
	returnUpdated = true
	return updated, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCanceled, err)
	}
	return nil
}

func validateChange(change Change) error {
	switch change.Kind {
	case AddEntry, EditEntry, MoveEntry, DeleteEntry, CreateGroup, DeleteGroup:
	default:
		return ErrUnsupported
	}
	for key := range change.Values {
		if key != "Title" && key != "UserName" && key != "URL" && key != "Notes" {
			return ErrUnsupported
		}
	}
	if len(change.Values) > 0 && change.Kind != AddEntry && change.Kind != EditEntry {
		return ErrUnsupported
	}
	if (change.PasswordChanged || len(change.Password) > 0) && change.Kind != AddEntry && change.Kind != EditEntry {
		return ErrUnsupported
	}
	if len(change.Password) > 0 && !change.PasswordChanged {
		return ErrUnsupported
	}
	if change.NewName != "" && change.Kind != CreateGroup {
		return ErrUnsupported
	}
	if bytes.ContainsAny(change.Password, "\r\n\x00") {
		return ErrUnsupported
	}
	return nil
}

func (c *CLI) runChange(ctx context.Context, path string, creds Credentials, change Change, selected selectedChange) error {
	args := []string{}
	stdin := []byte(nil)
	appendMaster := !creds.NoPassword
	if appendMaster {
		stdin = append(append(stdin, creds.Password...), '\n')
	}
	args = append(args, "-q")
	args = appendAuthArgs(args, creds)
	switch change.Kind {
	case AddEntry:
		args = append([]string{"add", "-q"}, args[1:]...)
		args = appendValueArg(args, "--username", change.Values["UserName"])
		args = appendValueArg(args, "--url", change.Values["URL"])
		if notes, exists := change.Values["Notes"]; exists {
			if strings.Contains(notes, `\n`) {
				zero(stdin)
				return ErrUnsupported
			}
			args = appendValueArg(args, "--notes", notes)
		}
		if change.PasswordChanged {
			args = append(args, "--password-prompt")
			stdin = append(stdin, change.Password...)
			stdin = append(stdin, '\n')
		}
		args = append(args, path, selected.entryPath)
	case EditEntry:
		args = append([]string{"edit", "-q"}, args[1:]...)
		for _, field := range []struct{ key, option string }{{"Title", "--title"}, {"UserName", "--username"}, {"URL", "--url"}, {"Notes", "--notes"}} {
			if v, ok := change.Values[field.key]; ok && v != selected.current.Get(field.key) {
				if v == "" || (field.key == "Notes" && strings.Contains(v, `\n`)) {
					zero(stdin)
					return ErrUnsupported
				}
				args = append(args, field.option+"="+v)
			}
		}
		if change.PasswordChanged {
			args = append(args, "--password-prompt")
			stdin = append(stdin, change.Password...)
			stdin = append(stdin, '\n')
		}
		if len(args) == len(authArgsPrefix(creds))+2 && !change.PasswordChanged {
			zero(stdin)
			return ErrUnsupported
		}
		args = append(args, path, selected.entryPath)
	case MoveEntry:
		args = append([]string{"mv", "-q"}, args[1:]...)
		args = append(args, path, selected.entryPath, selected.groupPath)
	case DeleteEntry:
		args = append([]string{"rm", "-q"}, args[1:]...)
		args = append(args, path, selected.entryPath)
	case CreateGroup:
		args = append([]string{"mkdir", "-q"}, args[1:]...)
		args = append(args, path, selected.groupPath)
	case DeleteGroup:
		args = append([]string{"rmdir", "-q"}, args[1:]...)
		args = append(args, path, selected.groupPath)
	}
	defer zero(stdin)
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if len(stdin) != 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		return ErrWrite
	}
	return nil
}

func appendAuthArgs(args []string, creds Credentials) []string {
	if creds.KeyFile != "" {
		args = append(args, "-k", creds.KeyFile)
	}
	if creds.NoPassword {
		args = append(args, "--no-password")
	}
	if creds.YubiKey != "" {
		args = append(args, "-y", creds.YubiKey)
	}
	return args
}

func authArgsPrefix(creds Credentials) []string { return appendAuthArgs(nil, creds) }

func appendValueArg(args []string, key, value string) []string {
	if value != "" {
		args = append(args, key+"="+value)
	}
	return args
}

func copyEncrypted(src string, dst *os.File, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err = io.Copy(dst, in); err != nil {
		return err
	}
	if err = dst.Sync(); err != nil {
		return err
	}
	return dst.Chmod(mode)
}

func createBackup(dir, source string, revision [32]byte) error {
	dir = filepath.Clean(dir)
	if err := ensurePrivateBackupDir(dir); err != nil {
		return err
	}
	pathHash := sha256.Sum256([]byte(source))
	prefix := fmt.Sprintf("%x-", pathHash[:8])
	backup, err := os.CreateTemp(dir, prefix+"*.kdbx")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	committed := false
	defer func() {
		_ = backup.Close()
		if !committed {
			_ = os.Remove(backupPath)
		}
	}()
	if err := backup.Chmod(0600); err != nil {
		return err
	}
	if err := copyEncrypted(source, backup, 0600); err != nil {
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	got, err := Revision(backupPath)
	if err != nil || got != revision {
		return errors.New("backup revision mismatch")
	}
	committed = true
	matches, err := filepath.Glob(filepath.Join(dir, prefix+"*.kdbx"))
	if err != nil {
		return err
	}
	sort.Slice(matches, func(i, j int) bool {
		a, _ := os.Stat(matches[i])
		b, _ := os.Stat(matches[j])
		if a == nil || b == nil {
			return matches[i] < matches[j]
		}
		return a.ModTime().After(b.ModTime())
	})
	if len(matches) > 5 {
		for _, old := range matches[5:] {
			if old != backupPath {
				if err := os.Remove(old); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ensurePrivateBackupDir(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		info, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("backup path is not a real directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("backup directory is not private")
	}
	return nil
}

type selectorSet struct {
	db              *Database
	groupPaths      map[string]string
	entryPaths      map[string]string
	rawGroupPaths   map[string]string
	rawGroupPathIDs map[string][]string
	rawEntryPathIDs map[string][]string
	groupsByUUID    map[string]Group
	entriesByUUID   map[string]Entry
}

type selectedChange struct {
	entryPath string
	groupPath string
	group     Group
	entry     Entry
	current   Entry
}

func selectorsFor(db *Database) (*selectorSet, error) {
	s := &selectorSet{db: db, groupPaths: map[string]string{}, entryPaths: map[string]string{}, rawGroupPaths: map[string]string{}, rawGroupPathIDs: map[string][]string{}, rawEntryPathIDs: map[string][]string{}, groupsByUUID: map[string]Group{}, entriesByUUID: map[string]Entry{}}
	for _, g := range db.Groups {
		s.groupsByUUID[g.UUID] = g
	}
	for _, e := range db.Entries {
		s.entriesByUUID[e.UUID] = e
	}
	for _, g := range db.Groups {
		if path, ok := s.rawGroupPath(g.UUID, make(map[string]bool)); ok {
			s.rawGroupPaths[g.UUID] = path
		}
	}
	for _, g := range db.Groups {
		if rawPath, ok := s.rawGroupPaths[g.UUID]; ok {
			s.rawGroupPathIDs[rawPath] = append(s.rawGroupPathIDs[rawPath], g.UUID)
		}
		path, err := s.groupSelector(g.UUID)
		if err != nil {
			continue
		}
		s.groupPaths[g.UUID] = path
	}
	for _, e := range db.Entries {
		group, ok := s.groupsByUUID[e.GroupUUID]
		if !ok {
			continue
		}
		if rawGroupPath, ok := s.rawGroupPaths[group.UUID]; ok {
			rawEntryPath := rawGroupPath + e.Get("Title")
			s.rawEntryPathIDs[rawEntryPath] = append(s.rawEntryPathIDs[rawEntryPath], e.UUID)
		}
		groupPath, err := s.groupSelector(group.UUID)
		if err != nil {
			continue
		}
		title := e.Get("Title")
		path := groupPath
		if path != "/" {
			path = strings.TrimSuffix(path, "/") + "/"
		}
		path += title
		s.entryPaths[e.UUID] = path
	}
	return s, nil
}

func (s *selectorSet) groupSelector(uuid string) (string, error) {
	group, ok := s.groupsByUUID[uuid]
	if !ok {
		return "", ErrUnsupported
	}
	var segments []string
	seen := make(map[string]bool)
	for group.ParentUUID != "" {
		if seen[group.UUID] {
			return "", ErrUnsupported
		}
		seen[group.UUID] = true
		if strings.Contains(group.Name, "/") || group.Name == "" {
			return "", ErrUnsupported
		}
		segments = append(segments, group.Name)
		parent, exists := s.groupsByUUID[group.ParentUUID]
		if !exists {
			return "", ErrUnsupported
		}
		group = parent
	}
	if len(segments) == 0 {
		return "/", nil
	}
	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	return "/" + strings.Join(segments, "/") + "/", nil
}

func (s *selectorSet) rawGroupPath(uuid string, visiting map[string]bool) (string, bool) {
	if path, ok := s.rawGroupPaths[uuid]; ok {
		return path, true
	}
	group, ok := s.groupsByUUID[uuid]
	if !ok || visiting[uuid] {
		return "", false
	}
	if group.ParentUUID == "" {
		s.rawGroupPaths[uuid] = "/"
		return "/", true
	}
	visiting[uuid] = true
	parent, ok := s.rawGroupPath(group.ParentUUID, visiting)
	delete(visiting, uuid)
	if !ok {
		return "", false
	}
	// KeePassXC descends groups with basePath + name + "/" verbatim.
	path := parent + group.Name + "/"
	s.rawGroupPaths[uuid] = path
	return path, true
}

func (s *selectorSet) uniqueGroup(uuid string) (string, error) {
	path, ok := s.groupPaths[uuid]
	if !ok {
		return "", ErrUnsupported
	}
	if len(s.rawGroupPathIDs[path]) != 1 || s.rawGroupPathIDs[path][0] != uuid {
		return "", ErrAmbiguous
	}
	return path, nil
}

func (s *selectorSet) uniqueEntry(uuid string) (string, Entry, error) {
	path, ok := s.entryPaths[uuid]
	if !ok {
		return "", Entry{}, ErrUnsupported
	}
	if len(s.rawEntryPathIDs[path]) != 1 || s.rawEntryPathIDs[path][0] != uuid {
		return "", Entry{}, ErrAmbiguous
	}
	return path, s.entriesByUUID[uuid], nil
}

func (s *selectorSet) selectChange(change Change) (selectedChange, error) {
	var selected selectedChange
	switch change.Kind {
	case AddEntry:
		if change.GroupUUID == "" || change.Values["Title"] == "" || strings.Contains(change.Values["Title"], "/") {
			return selected, ErrUnsupported
		}
		group, ok := s.groupsByUUID[change.GroupUUID]
		if !ok || group.Recycled || group.UUID == s.db.RecycleBinUUID {
			return selected, ErrUnsupported
		}
		groupPath, err := s.uniqueGroup(group.UUID)
		if err != nil {
			return selected, err
		}
		if (groupPath == "/" && change.Values["Title"] == "") || (groupPath != "/" && strings.Contains(change.Values["Title"], "/")) {
			return selected, ErrUnsupported
		}
		entryPath := strings.TrimSuffix(groupPath, "/") + "/" + change.Values["Title"]
		if groupPath == "/" {
			entryPath = "/" + change.Values["Title"]
		}
		if len(s.rawEntryPathIDs[entryPath]) != 0 {
			return selected, ErrAmbiguous
		}
		selected.group, selected.groupPath = group, groupPath
		selected.entryPath = entryPath
	case EditEntry, MoveEntry, DeleteEntry:
		path, entry, err := s.uniqueEntry(change.EntryUUID)
		if err != nil {
			return selected, err
		}
		selected.entryPath, selected.entry, selected.current = path, entry, entry
		if change.Kind == EditEntry {
			if newTitle, exists := change.Values["Title"]; exists && newTitle != entry.Get("Title") {
				groupPath, ok := s.rawGroupPaths[entry.GroupUUID]
				if !ok {
					return selected, ErrUnsupported
				}
				candidatePath := groupPath + newTitle
				for _, uuid := range s.rawEntryPathIDs[candidatePath] {
					if uuid != entry.UUID {
						return selected, ErrAmbiguous
					}
				}
			}
		}
		if change.Kind == MoveEntry {
			group, ok := s.groupsByUUID[change.GroupUUID]
			if !ok {
				return selected, ErrUnsupported
			}
			groupPath, err := s.uniqueGroup(group.UUID)
			if err != nil {
				return selected, err
			}
			if group.UUID == entry.GroupUUID {
				return selected, ErrUnsupported
			}
			if rawGroupPath, ok := s.rawGroupPaths[group.UUID]; ok {
				candidatePath := rawGroupPath + entry.Get("Title")
				for _, uuid := range s.rawEntryPathIDs[candidatePath] {
					if uuid != entry.UUID {
						return selected, ErrAmbiguous
					}
				}
			} else {
				return selected, ErrUnsupported
			}
			selected.group, selected.groupPath = group, groupPath
		}
	case DeleteGroup:
		group, ok := s.groupsByUUID[change.GroupUUID]
		if !ok || group.ParentUUID == "" {
			return selected, ErrUnsupported
		}
		path, err := s.uniqueGroup(group.UUID)
		if err != nil {
			return selected, err
		}
		if group.UUID == s.db.RecycleBinUUID || containsGroup(s.db, group.UUID, s.db.RecycleBinUUID) {
			return selected, ErrUnsupported
		}
		selected.group, selected.groupPath = group, strings.TrimSuffix(path, "/")
	case CreateGroup:
		parent, ok := s.groupsByUUID[change.GroupUUID]
		if !ok || parent.Recycled || parent.UUID == s.db.RecycleBinUUID || change.NewName == "" || strings.Contains(change.NewName, "/") || strings.HasPrefix(change.NewName, "/") || strings.HasSuffix(change.NewName, "/") {
			return selected, ErrUnsupported
		}
		parentPath, err := s.uniqueGroup(parent.UUID)
		if err != nil {
			return selected, err
		}
		path := strings.TrimSuffix(parentPath, "/") + "/" + change.NewName
		if parentPath == "/" {
			path = "/" + change.NewName
		}
		candidatePath := path + "/"
		if len(s.rawGroupPathIDs[candidatePath]) != 0 {
			return selected, ErrAmbiguous
		}
		selected.group = parent
		selected.groupPath = path
	}
	return selected, nil
}

func containsGroup(db *Database, ancestorUUID, targetUUID string) bool {
	if targetUUID == "" {
		return false
	}
	groups := make(map[string]Group, len(db.Groups))
	for _, group := range db.Groups {
		groups[group.UUID] = group
	}
	for group := groups[targetUUID]; group.UUID != ""; group = groups[group.ParentUUID] {
		if group.UUID == ancestorUUID {
			return true
		}
	}
	return false
}

func verifyChange(before, after *Database, change Change, selected selectedChange) error {
	entryByUUID := make(map[string]Entry, len(after.Entries))
	for _, entry := range after.Entries {
		entryByUUID[entry.UUID] = entry
	}
	groupByUUID := make(map[string]Group, len(after.Groups))
	for _, group := range after.Groups {
		groupByUUID[group.UUID] = group
	}
	switch change.Kind {
	case AddEntry:
		var found *Entry
		for i := range after.Entries {
			e := &after.Entries[i]
			if e.GroupUUID == change.GroupUUID && e.Get("Title") == change.Values["Title"] {
				if found != nil {
					return ErrAmbiguous
				}
				found = e
			}
		}
		if found == nil || !verifyValues(*found, change.Values) || (change.PasswordChanged && found.Get("Password") != string(change.Password)) {
			return ErrWrite
		}
		if err := verifyEntrySelector(after, found.UUID); err != nil {
			return err
		}
	case EditEntry:
		entry, ok := entryByUUID[change.EntryUUID]
		if !ok || !verifyValues(entry, change.Values) || (change.PasswordChanged && entry.Get("Password") != string(change.Password)) {
			return ErrWrite
		}
		if err := verifyEntrySelector(after, change.EntryUUID); err != nil {
			return err
		}
	case MoveEntry:
		entry, ok := entryByUUID[change.EntryUUID]
		if !ok || entry.GroupUUID != change.GroupUUID {
			return ErrWrite
		}
		if err := verifyEntrySelector(after, change.EntryUUID); err != nil {
			return err
		}
	case DeleteEntry:
		beforeEntry, ok := selected.entry, selected.entry.UUID != ""
		if !ok {
			return ErrWrite
		}
		afterEntry, exists := entryByUUID[change.EntryUUID]
		shouldRecycle := before.RecycleBinEnabled && !beforeEntry.Recycled
		if shouldRecycle != exists || (shouldRecycle && !afterEntry.Recycled) {
			return ErrWrite
		}
	case CreateGroup:
		count := 0
		createdUUID := ""
		for _, group := range after.Groups {
			if group.ParentUUID == change.GroupUUID && group.Name == change.NewName {
				count++
				createdUUID = group.UUID
			}
		}
		if count != 1 {
			return ErrWrite
		}
		if err := verifyGroupSelector(after, createdUUID); err != nil {
			return err
		}
	case DeleteGroup:
		beforeGroup := selected.group
		afterGroup, exists := groupByUUID[change.GroupUUID]
		shouldRecycle := before.RecycleBinEnabled && !beforeGroup.Recycled
		if shouldRecycle != exists || (shouldRecycle && !afterGroup.Recycled) {
			return ErrWrite
		}
	}
	return nil
}

func verifyEntrySelector(db *Database, uuid string) error {
	selectors, err := selectorsFor(db)
	if err != nil {
		return ErrWrite
	}
	if _, _, err := selectors.uniqueEntry(uuid); err != nil {
		return err
	}
	return nil
}

func verifyGroupSelector(db *Database, uuid string) error {
	selectors, err := selectorsFor(db)
	if err != nil {
		return ErrWrite
	}
	if _, err := selectors.uniqueGroup(uuid); err != nil {
		return err
	}
	return nil
}

func verifyValues(entry Entry, values map[string]string) bool {
	for field, expected := range values {
		if entry.Get(field) != expected {
			return false
		}
	}
	return true
}
