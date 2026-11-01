package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

// runGrantJob owns snapshots only. Model updates are accepted on the UI thread
// after checking that each original unlocked session is still current.
func (m *Model) runGrantJob(p *runpkg.Pending, targets []runTarget, mode string) tea.Cmd {
	snapshots := map[string]*vault.Database{}
	writable := true
	for _, target := range targets {
		if snapshots[target.session] == nil {
			snapshots[target.session] = target.database.Clone()
		}
		s := m.ws.Sessions[target.session]
		if s == nil || s.ReadOnly || s.Stale || m.readOnly {
			writable = false
		}
	}
	backend := m.backend
	backup := m.settings.BackupDir
	return func() tea.Msg {
		result := runResult{pending: p, targets: targets, values: map[string]string{}, updated: map[string]*vault.Database{}}
		defer func() {
			for _, db := range snapshots {
				db.Clear()
			}
		}()
		resolver, ok := backend.(interface {
			ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
		})
		if !ok {
			result.err = fmt.Errorf("field resolution unavailable")
			return result
		}
		writer, _ := backend.(vault.Writer)
		if mode != "a" && mode != "check" && (writer == nil || !writable) {
			result.err = fmt.Errorf("vault is not writable")
			return result
		}
		for _, target := range targets {
			db := snapshots[target.session]
			if mode == "check" {
				want := runpkg.Grant{Version: 1, Entry: target.entry, Field: target.field, Executable: p.Request.Command[0], Directory: p.Request.Directory}
				for _, entry := range db.Entries {
					if entry.UUID == target.entry {
						want.Name = entry.Get("Title")
						want.Group = entry.GroupPath
					}
				}
				allowed := false
				for _, entry := range db.Entries {
					if entry.Recycled || !strings.HasSuffix(strings.Trim(entry.GroupPath, "/"), runpkg.GrantGroup) {
						continue
					}
					value, err := resolver.ResolveField(p.Context, db, vault.Credentials{}, entry.UUID, "Notes")
					if err == nil {
						grant, ok := runpkg.DecodeGrant(string(value))
						if ok && grant.Allows(want) {
							allowed = true
						}
					}
					clear(value)
				}
				if !allowed {
					result.needsApproval = true
					return result
				}
			}
			if mode != "a" && mode != "check" {
				grant := runpkg.Grant{Version: 1, Entry: target.entry, Field: target.field, Executable: p.Request.Command[0]}
				for _, entry := range db.Entries {
					if entry.UUID == target.entry {
						grant.Name = entry.Get("Title")
						grant.Group = entry.GroupPath
					}
				}
				if mode == "p" {
					grant.Directory = p.Request.Directory
				}
				group := ""
				for _, g := range db.Groups {
					if g.Name == runpkg.GrantGroup && g.ParentUUID == db.RootUUID {
						group = g.UUID
					}
				}
				apply := func(change vault.Change) error {
					updated, err := writer.Apply(p.Context, db, vault.Credentials{}, change, backup)
					if err != nil {
						return err
					}
					updated.TransferInteractive(target.database)
					db.Clear()
					db = updated
					snapshots[target.session] = db
					return nil
				}
				if group == "" {
					if err := apply(vault.Change{Kind: vault.CreateGroup, GroupUUID: db.RootUUID, NewName: runpkg.GrantGroup}); err != nil {
						result.err = err
						return result
					}
					for _, g := range db.Groups {
						if g.Name == runpkg.GrantGroup && g.ParentUUID == db.RootUUID {
							group = g.UUID
						}
					}
				}
				if group == "" {
					result.err = fmt.Errorf("permission group unavailable")
					return result
				}
				exists := false
				for _, entry := range db.Entries {
					if entry.GroupUUID == group && entry.Get("Title") == grant.Title() {
						exists = true
					}
				}
				if !exists {
					if err := apply(vault.Change{Kind: vault.AddEntry, GroupUUID: group, Values: map[string]string{"Title": grant.Title(), "Notes": grant.Encode()}}); err != nil {
						result.err = err
						return result
					}
				}
				result.updated[target.session] = db.Clone()
			}
			if reader, ok := backend.(entryDetailsReader); ok {
				fields, err := reader.EntryDetails(p.Context, db, target.entry)
				if err != nil {
					result.err = err
					return result
				}
				for i := range db.Entries {
					if db.Entries[i].UUID == target.entry {
						for _, field := range fields {
							found := false
							for _, existing := range db.Entries[i].Fields {
								if existing.Name == field.Name {
									found = true
								}
							}
							if !found {
								db.Entries[i].Fields = append(db.Entries[i].Fields, field)
							}
						}
					}
				}
			}
			value, err := resolver.ResolveField(p.Context, db, vault.Credentials{}, target.entry, target.field)
			if err != nil {
				result.err = err
				return result
			}
			result.values[target.ref] = string(value)
			clear(value)
		}
		return result
	}
}

func grantEligible(command []string) bool {
	if len(command) == 0 {
		return false
	}
	name := strings.ToLower(filepath.Base(command[0]))
	// Interpreter invocations need a dedicated, verified script identity. Until
	// that can be established, they can only receive one-time approval.
	for _, prefix := range []string{"sh", "bash", "zsh", "dash", "fish", "python", "node", "ruby", "perl", "php", "env", "busybox", "deno", "bun", "lua", "pwsh"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}
