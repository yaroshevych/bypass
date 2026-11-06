package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

func (s *interactiveSession) failGroupDeletion(err error) (*Database, error) {
	s.groupDeletePlan = nil
	s.failed = true
	s.close()
	return nil, errors.Join(ErrInteractiveSessionLost, ErrPartialWrite, ErrWrite, err)
}

func (s *interactiveSession) prepareRecycleBin(ctx context.Context, db *Database, backupDir string) (*Database, error) {
	s.groupDeletePlan = nil
	if err := backupInteractiveVault(db.Path, backupDir); err != nil {
		return nil, ErrBackup
	}
	path, err := s.discoverRecycleBin(ctx)
	if err != nil {
		return s.failGroupDeletion(err)
	}
	updated, err := s.load(ctx, db.Path)
	if err != nil {
		return s.failGroupDeletion(err)
	}
	selectors, err := selectorsFor(updated)
	if err != nil {
		return s.failGroupDeletion(err)
	}
	if found, err := selectors.uniqueGroup(updated.RecycleBinUUID); err != nil || found != path {
		return s.failGroupDeletion(ErrAmbiguous)
	}
	updated.interactive, updated.interactiveOwner = s, true
	return updated, nil
}

func (s *interactiveSession) prepareGroupDeletion(ctx context.Context, db *Database, selected selectedChange, backupDir string) (*Database, error) {
	s.groupDeletePlan = nil
	if err := backupInteractiveVault(db.Path, backupDir); err != nil {
		return nil, ErrBackup
	}
	binPath, err := s.discoverRecycleBin(ctx)
	if err != nil {
		return s.failGroupDeletion(err)
	}
	updated, err := s.load(ctx, db.Path)
	if err != nil {
		return s.failGroupDeletion(fmt.Errorf("reload after probe: %w", err))
	}
	selectors, err := selectorsFor(updated)
	if err != nil {
		return s.failGroupDeletion(err)
	}
	if path, err := selectors.uniqueGroup(updated.RecycleBinUUID); err != nil || path != binPath {
		return s.failGroupDeletion(ErrAmbiguous)
	}
	if _, err := selectors.selectChange(Change{Kind: DeleteGroup, GroupUUID: selected.group.UUID}); err != nil {
		return s.failGroupDeletion(err)
	}
	plan := &GroupDeletePlan{GroupUUID: selected.group.UUID, GroupName: selected.group.Name, BinPath: binPath}
	for _, group := range updated.Groups {
		if containsGroup(updated, selected.group.UUID, group.UUID) {
			plan.TargetGroups++
		}
		if group.ParentUUID == updated.RecycleBinUUID && group.Name == selected.group.Name {
			plan.MatchingGroups++
		}
	}
	for _, entry := range updated.Entries {
		if containsGroup(updated, selected.group.UUID, entry.GroupUUID) {
			plan.TargetEntries++
		}
	}
	if plan.MatchingGroups > 0 {
		// A lone modifier produces no search terms and matches all entries;
		// an empty positional argument is discarded by the interactive parser.
		out, err := s.command(ctx, "search", "--", "*")
		if err != nil {
			zero(out)
			return s.failGroupDeletion(fmt.Errorf("search matching entries: %w", err))
		}
		prefix := binPath + selected.group.Name + "/"
		for _, path := range strings.Split(string(trimOutputLine(out)), "\n") {
			if strings.HasPrefix(path, prefix) {
				plan.MatchingEntries++
			}
		}
		zero(out)
	}
	plan.Revision, err = Revision(db.Path)
	if err != nil {
		return s.failGroupDeletion(err)
	}
	s.groupDeletePlan = plan
	updated.GroupDeletePlan = plan
	updated.interactive, updated.interactiveOwner = s, true
	return updated, nil
}

func (s *interactiveSession) nativeGroupCommand(ctx context.Context, expected string, args ...string) error {
	out, err := s.command(ctx, args...)
	ok := bytes.Equal(out, []byte(expected+"\n"))
	zero(out)
	if err != nil || !ok {
		return errors.Join(ErrWrite, err)
	}
	return nil
}

func (s *interactiveSession) deletePreparedGroup(ctx context.Context, db *Database, selected selectedChange, backupDir string) (*Database, error) {
	plan := s.groupDeletePlan
	if plan == nil || db.GroupDeletePlan == nil || *plan != *db.GroupDeletePlan || plan.GroupUUID != selected.group.UUID || plan.BinPath != s.recycleBinPath {
		return nil, ErrUnsupported
	}
	revision, err := Revision(db.Path)
	if err != nil || revision != plan.Revision {
		s.groupDeletePlan = nil
		return nil, ErrExternalChanged
	}
	if err := backupInteractiveVault(db.Path, backupDir); err != nil {
		return nil, ErrBackup
	}
	s.groupDeletePlan = nil
	budget := 50000
	matchingPath := plan.BinPath + plan.GroupName + "/"
	for i := 0; i < plan.MatchingGroups; i++ {
		if err := s.emptyGroup(ctx, matchingPath, plan.BinPath, 0, true, &budget); err != nil {
			return s.failGroupDeletion(err)
		}
		path := strings.TrimSuffix(matchingPath, "/")
		if err := s.nativeGroupCommand(ctx, "Successfully deleted group "+path+".", "rmdir", "--", path); err != nil {
			return s.failGroupDeletion(err)
		}
	}
	target := selected.groupPath + "/"
	if err := s.emptyGroup(ctx, target, plan.BinPath, 0, false, &budget); err != nil {
		return s.failGroupDeletion(err)
	}
	if err := s.nativeGroupCommand(ctx, "Successfully recycled group "+selected.groupPath+".", "rmdir", "--", selected.groupPath); err != nil {
		return s.failGroupDeletion(err)
	}
	path := strings.TrimSuffix(matchingPath, "/")
	if err := s.nativeGroupCommand(ctx, "Successfully deleted group "+path+".", "rmdir", "--", path); err != nil {
		return s.failGroupDeletion(err)
	}
	updated, err := s.load(ctx, db.Path)
	if err != nil {
		return s.failGroupDeletion(err)
	}
	updated.interactive, updated.interactiveOwner = s, true
	return updated, nil
}

// emptyGroup moves all entries out before removing nested groups. Repeated
// names are resolved one at a time, matching KeePassXC's path lookup order.
func (s *interactiveSession) emptyGroup(ctx context.Context, path, bin string, depth int, inBin bool, budget *int) error {
	if depth > 64 {
		return ErrTooLarge
	}
	processedChildren := false
	for {
		*budget--
		if *budget < 0 {
			return ErrTooLarge
		}
		out, err := s.command(ctx, "ls", "-q", "-f", "--", path)
		if err != nil {
			zero(out)
			return err
		}
		listing := string(trimOutputLine(out))
		zero(out)
		if listing == "" {
			return nil
		}
		if listing == "[empty]" {
			out, err := s.command(ctx, "show", "-q", "-a", "Uuid", "--", path+listing)
			id := normalizeUUID(strings.Trim(string(trimOutputLine(out)), "{}"))
			zero(out)
			if err != nil {
				return err
			}
			if id == "" {
				return nil
			}
		}
		items := strings.Split(listing, "\n")
		if !inBin && !processedChildren {
			processedChildren = true
			children := make(map[string]bool)
			for _, child := range items {
				if strings.HasSuffix(child, "/") {
					if child == "/" || strings.Contains(strings.TrimSuffix(child, "/"), "/") || strings.IndexFunc(child, unicode.IsControl) >= 0 {
						return ErrAmbiguous
					}
					if children[child] {
						return ErrAmbiguous
					}
					children[child] = true
				}
			}
			for _, child := range items {
				if strings.HasSuffix(child, "/") {
					if err := s.emptyGroup(ctx, path+child, bin, depth+1, false, budget); err != nil {
						return err
					}
				}
			}
		}
		item := ""
		for _, candidate := range items {
			if inBin || !strings.HasSuffix(candidate, "/") {
				item = candidate
				break
			}
		}
		if item == "" {
			return nil
		}
		if item == "" || strings.IndexFunc(item, unicode.IsControl) >= 0 {
			return ErrAmbiguous
		}
		if strings.HasSuffix(item, "/") {
			if strings.Contains(strings.TrimSuffix(item, "/"), "/") || item == "/" {
				return ErrAmbiguous
			}
			child := path + item
			if err := s.emptyGroup(ctx, child, bin, depth+1, true, budget); err != nil {
				return err
			}
			childArg := strings.TrimSuffix(child, "/")
			if err := s.nativeGroupCommand(ctx, "Successfully deleted group "+childArg+".", "rmdir", "--", childArg); err != nil {
				return err
			}
			continue
		}
		if strings.Contains(item, "/") {
			return ErrAmbiguous
		}
		binOut, err := s.command(ctx, "ls", "-q", "-f", "--", bin)
		if err != nil {
			zero(binOut)
			return err
		}
		binItems := strings.Split(string(trimOutputLine(binOut)), "\n")
		zero(binOut)
		if len(binItems) == 1 && binItems[0] == "[empty]" {
			out, err := s.command(ctx, "show", "-q", "-a", "Uuid", "--", bin+"[empty]")
			id := normalizeUUID(strings.Trim(string(trimOutputLine(out)), "{}"))
			zero(out)
			if err != nil {
				return err
			}
			if id == "" {
				binItems = nil
			}
		}
		occupied := map[string]bool{}
		for _, name := range binItems {
			if !strings.HasSuffix(name, "/") {
				occupied[name] = true
			}
		}
		finalTitle := ""
		if occupied[item] {
			used := map[string]bool{}
			for _, name := range items {
				used[name] = true
			}
			newTitle := ""
			for i := 1; i <= 10000; i++ {
				candidate := fmt.Sprintf("%s %d", item, i)
				if !occupied[candidate] {
					newTitle = candidate
					break
				}
			}
			if newTitle == "" {
				return ErrTooLarge
			}
			if used[newTitle] {
				finalTitle = newTitle
				newTitle = ""
				for i := 1; i <= 10000; i++ {
					candidate := fmt.Sprintf("%s %d", item, i)
					if !occupied[candidate] && !used[candidate] {
						newTitle = candidate
						break
					}
				}
				if newTitle == "" {
					return ErrTooLarge
				}
			}
			if err := s.nativeGroupCommand(ctx, "Successfully edited entry "+newTitle+".", "edit", "--title", newTitle, "--", path+item); err != nil {
				return err
			}
			item = newTitle
		}
		if err := s.nativeGroupCommand(ctx, "Successfully moved entry "+item+" to group "+bin+".", "mv", "--", path+item, bin); err != nil {
			return err
		}
		if finalTitle != "" {
			if err := s.nativeGroupCommand(ctx, "Successfully edited entry "+finalTitle+".", "edit", "--title", finalTitle, "--", bin+item); err != nil {
				return err
			}
		}
	}
}
