package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxGeneratedPasswordLength = 1024

func (c *commandCLI) Generate(ctx context.Context, length int) ([]byte, error) {
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
	case AddEntry, EditEntry, MoveEntry, DeleteEntry, CreateGroup, DeleteGroup, AddAttachment, ReplaceAttachment, RemoveAttachment:
	default:
		return ErrUnsupported
	}
	isAttachmentChange := change.Kind == AddAttachment || change.Kind == ReplaceAttachment || change.Kind == RemoveAttachment
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
	if isAttachmentChange {
		if !validAttachmentName(change.AttachmentName) || len(change.AttachmentData) > maxAttachmentBytes {
			return ErrUnsupported
		}
		if change.Kind == RemoveAttachment && change.AttachmentData != nil {
			return ErrUnsupported
		}
	} else if change.AttachmentName != "" || change.AttachmentData != nil {
		return ErrUnsupported
	}
	if bytes.ContainsAny(change.Password, "\r\n\x00") {
		return ErrUnsupported
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
	case AddAttachment, ReplaceAttachment, RemoveAttachment:
		if !validAttachmentName(change.AttachmentName) {
			return selected, ErrUnsupported
		}
		path, entry, err := s.uniqueEntry(change.EntryUUID)
		if err != nil {
			return selected, err
		}
		exists := containsString(entry.AttachmentNames, change.AttachmentName)
		if (change.Kind == AddAttachment && exists) || (change.Kind != AddAttachment && !exists) {
			return selected, ErrUnsupported
		}
		selected.entryPath, selected.entry, selected.current = path, entry, entry
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
