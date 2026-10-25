package vault

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"unicode"
)

type EntryRename struct {
	Path  string
	Title string
}

// DuplicateEntries keeps the authenticated session alive for an explicit repair
// decision. It contains entry names, but no master password or entry values.
type DuplicateEntries struct {
	Path    string
	Renames []EntryRename
	session *interactiveSession
	state   atomic.Int32 // 0 pending, 1 closed, 2 transferred to the opened database
}

func (p *DuplicateEntries) Error() string { return "duplicate entry names require confirmation" }
func (p *DuplicateEntries) Unwrap() error { return ErrAmbiguous }
func (p *DuplicateEntries) Close() {
	if p != nil && p.state.CompareAndSwap(0, 1) {
		p.session.close()
	}
}

func (s *interactiveSession) duplicateRenames(ctx context.Context) ([]EntryRename, error) {
	var renames []EntryRename
	entities := 0
	owners := map[string]string{}
	occupied := map[string]bool{}
	type duplicates struct {
		path, name string
		count      int
	}
	var pending []duplicates
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if depth > 64 || entities >= 10000 {
			return ErrTooLarge
		}
		output, err := s.command(ctx, "ls", "-q", "-f", "--", path)
		if err != nil {
			return err
		}
		listing := string(trimOutputLine(output))
		zero(output)
		if listing == "" || listing == "[empty]" {
			return nil
		}
		counts, used := map[string]int{}, map[string]bool{}
		var names, groups []string
		for row, name := range strings.Split(listing, "\n") {
			entities++
			if entities > 10000 {
				return ErrTooLarge
			}
			reason := ""
			switch {
			case name == "":
				reason = "empty_listing_name"
			case strings.IndexFunc(name, unicode.IsControl) >= 0:
				reason = "control_character_in_listing_name"
			case strings.HasSuffix(name, "/") && strings.Contains(strings.TrimSuffix(name, "/"), "/"):
				reason = "slash_in_listing_name"
			case name == "/":
				reason = "empty_group_name"
			}
			if reason != "" {
				diagnostic(ctx, "duplicate_scan_rejected", "reason", reason, "depth", depth, "row", row)
				return ErrAmbiguous
			}
			if strings.HasSuffix(name, "/") {
				if used[name] {
					diagnostic(ctx, "duplicate_scan_rejected", "reason", "duplicate_group_name", "depth", depth, "row", row)
					return ErrAmbiguous
				} // Native CLI cannot rename duplicate groups.
				used[name] = true
				groups = append(groups, name)
				continue
			}
			fullPath := path + name
			if owner, exists := owners[fullPath]; exists && owner != path {
				diagnostic(ctx, "duplicate_scan_rejected", "reason", "ambiguous_entry_path", "depth", depth, "row", row)
				return ErrAmbiguous
			}
			owners[fullPath], occupied[fullPath] = path, true
			if counts[name] == 0 {
				names = append(names, name)
			}
			counts[name]++
			used[name] = true
		}
		for _, name := range names {
			if counts[name] < 2 {
				continue
			}
			diagnostic(ctx, "duplicate_entries_found", "depth", depth, "count", counts[name])
			pending = append(pending, duplicates{path, name, counts[name]})
		}
		for _, group := range groups {
			if err := walk(path+group, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("/", 0); err != nil {
		return nil, err
	}
	for _, duplicate := range pending {
		for number, remaining := 1, duplicate.count; remaining > 0; number++ {
			title := fmt.Sprintf("%s %d", duplicate.name, number)
			if occupied[duplicate.path+title] {
				continue
			}
			occupied[duplicate.path+title] = true
			renames = append(renames, EntryRename{Path: duplicate.path + duplicate.name, Title: title})
			remaining--
		}
	}
	return renames, nil
}

// Rename uses native edits on the original vault. Completed edits remain saved
// if a later command fails; the encrypted backup is retained for recovery.
func (p *DuplicateEntries) Rename(ctx context.Context, backupDir string) (_ *Database, err error) {
	s := p.session
	s.transactionMu.Lock()
	defer s.transactionMu.Unlock()
	if p.state.Load() != 0 {
		return nil, ErrInteractiveSessionLost
	}
	defer func() {
		if err != nil {
			p.Close()
		}
	}()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	info, err := os.Stat(p.Path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	if info.Mode().Perm()&0222 == 0 {
		return nil, ErrReadOnly
	}
	if err := backupInteractiveVault(p.Path, backupDir); err != nil {
		return nil, ErrBackup
	}
	for _, rename := range p.Renames {
		output, err := s.command(ctx, "edit", "--title", rename.Title, "--", rename.Path)
		ok := string(output) == "Successfully edited entry "+rename.Title+".\n"
		zero(output)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrWrite
		}
	}
	db, err := s.load(ctx, p.Path)
	if err != nil {
		return nil, err
	}
	db.interactive, db.interactiveOwner = s, true
	if !p.state.CompareAndSwap(0, 2) {
		db.Clear()
		return nil, ErrCanceled
	}
	return db, nil
}
